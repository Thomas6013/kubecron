package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/kubecron/kubecron/internal/metrics"
	"github.com/kubecron/kubecron/internal/schedule"
	"github.com/kubecron/kubecron/internal/storage"
)

// CronJobHandler receives CronJob informer events and persists them to the store.
type CronJobHandler struct {
	clusterID string
	store     storage.Store
}

// NewCronJobHandler creates a handler for the given cluster.
func NewCronJobHandler(clusterID string, store storage.Store) *CronJobHandler {
	return &CronJobHandler{clusterID: clusterID, store: store}
}

// OnAdd handles a newly observed CronJob (initial list or new resource).
func (h *CronJobHandler) OnAdd(obj interface{}, isInInitialList bool) {
	cj, ok := obj.(*batchv1.CronJob)
	if !ok {
		return
	}
	h.upsert(cj)
}

// OnUpdate handles a changed CronJob.
func (h *CronJobHandler) OnUpdate(oldObj, newObj interface{}) {
	cj, ok := newObj.(*batchv1.CronJob)
	if !ok {
		return
	}
	h.upsert(cj)
}

// OnDelete soft-deletes the CronJob: the row is hidden from listings and its
// Prometheus series are dropped, but its run history is preserved (BUG-20).
func (h *CronJobHandler) OnDelete(obj interface{}) {
	cj, ok := obj.(*batchv1.CronJob)
	if !ok {
		// The informer delivers a tombstone when the final state was missed
		// (watch gap, relist); it wraps the last known object.
		tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !isTombstone {
			return
		}
		if cj, ok = tombstone.Obj.(*batchv1.CronJob); !ok {
			return
		}
	}
	h.markDeleted(context.Background(), cj.Namespace, cj.Name)
}

// Reconcile soft-deletes any CronJob that the store still lists as live but
// that is absent from the cluster. The informer only reports deletions it
// observed, so CronJobs removed while KubeCron was down would otherwise linger
// forever. Called once per cluster after the CronJob cache has synced.
func (h *CronJobHandler) Reconcile(ctx context.Context, live []*batchv1.CronJob) {
	present := make(map[string]struct{}, len(live))
	for _, cj := range live {
		present[h.id(cj.Namespace, cj.Name)] = struct{}{}
	}

	stored, err := h.store.ListCronJobs(ctx, h.clusterID)
	if err != nil {
		slog.Error("cronjob reconcile: failed to list stored cronjobs", "cluster", h.clusterID, "err", err)
		return
	}

	var removed int
	for _, cj := range stored {
		if _, ok := present[cj.ID]; ok {
			continue
		}
		h.markDeleted(ctx, cj.Namespace, cj.Name)
		removed++
	}
	if removed > 0 {
		slog.Info("cronjob reconcile: marked cronjobs deleted", "cluster", h.clusterID, "count", removed)
	}
}

// markDeleted flags the CronJob as gone and drops its Prometheus series so the
// gauges stop reporting a schedule and status for an object that no longer
// exists.
func (h *CronJobHandler) markDeleted(ctx context.Context, namespace, name string) {
	if err := h.store.MarkCronJobDeleted(ctx, h.id(namespace, name)); err != nil {
		slog.Error("failed to mark cronjob deleted", "cluster", h.clusterID, "namespace", namespace, "name", name, "err", err)
		return
	}
	metrics.DeleteCronJobSeries(h.clusterID, namespace, name)
}

// id builds the stable, deterministic CronJob key: <clusterID>/<namespace>/<name>.
func (h *CronJobHandler) id(namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", h.clusterID, namespace, name)
}

// upsert converts a CronJob object to a storage.CronJob and persists it.
func (h *CronJobHandler) upsert(cj *batchv1.CronJob) {
	namespace := cj.Namespace
	name := cj.Name

	record := storage.CronJob{
		ID:        h.id(namespace, name),
		ClusterID: h.clusterID,
		Namespace: namespace,
		Name:      name,
		Schedule:  cj.Spec.Schedule,
		TimeZone:  cj.Spec.TimeZone,
		Suspended: cj.Spec.Suspend != nil && *cj.Spec.Suspend,
		UpdatedAt: time.Now(),
	}

	// Extract resource requests/limits from the first container, if present.
	containers := cj.Spec.JobTemplate.Spec.Template.Spec.Containers
	if len(containers) > 0 {
		res := containers[0].Resources
		record.CPURequest = quantityPtr(res.Requests, corev1.ResourceCPU)
		record.CPULimit = quantityPtr(res.Limits, corev1.ResourceCPU)
		record.MemoryRequest = quantityPtr(res.Requests, corev1.ResourceMemory)
		record.MemoryLimit = quantityPtr(res.Limits, corev1.ResourceMemory)
	}

	// Last successful time from status.
	if cj.Status.LastSuccessfulTime != nil {
		t := cj.Status.LastSuccessfulTime.Time
		record.LastSuccessfulTime = &t
	}

	ctx := context.Background()
	if err := h.store.UpsertCronJob(ctx, record); err != nil {
		slog.Error("failed to upsert cronjob", "cluster", h.clusterID, "namespace", namespace, "name", name, "err", err)
	}

	// Update Prometheus gauges on every CronJob add/update.
	suspendedVal := 0.0
	if record.Suspended {
		suspendedVal = 1.0
	}
	metrics.CronJobSuspended.WithLabelValues(h.clusterID, namespace, name).Set(suspendedVal)

	// The schedule is resolved in the CronJob's own zone; publishing a next-run
	// timestamp computed in the server's zone would make the gauge disagree with
	// when Kubernetes actually fires the job (DOM-1).
	nextRun, err := schedule.NextRun(record.Schedule, record.TZ(), time.Now())
	if err != nil {
		// Leave the gauge untouched rather than publishing a wrong timestamp.
		slog.Warn("cannot compute next run", "cluster", h.clusterID, "namespace", namespace,
			"name", name, "schedule", record.Schedule, "time_zone", record.TZ(), "err", err)
		return
	}
	metrics.NextRunTimestamp.WithLabelValues(h.clusterID, namespace, name).Set(float64(nextRun.Unix()))
}

// quantityPtr returns a pointer to the string representation of a resource
// quantity from the given resource list, or nil if the resource is absent.
func quantityPtr(list corev1.ResourceList, name corev1.ResourceName) *string {
	if list == nil {
		return nil
	}
	q, ok := list[name]
	if !ok {
		return nil
	}
	s := q.String()
	return &s
}
