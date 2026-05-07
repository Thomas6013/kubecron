package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

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

// OnDelete is a no-op — we keep historical CronJob records.
func (h *CronJobHandler) OnDelete(obj interface{}) {}

// upsert converts a CronJob object to a storage.CronJob and persists it.
func (h *CronJobHandler) upsert(cj *batchv1.CronJob) {
	namespace := cj.Namespace
	name := cj.Name

	// Stable, deterministic ID: <clusterID>/<namespace>/<name>
	id := fmt.Sprintf("%s/%s/%s", h.clusterID, namespace, name)

	record := storage.CronJob{
		ID:        id,
		ClusterID: h.clusterID,
		Namespace: namespace,
		Name:      name,
		Schedule:  cj.Spec.Schedule,
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
