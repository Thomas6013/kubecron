package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubecron/kubecron/internal/schedule"
	"github.com/kubecron/kubecron/internal/storage"
)

// resourcesResponse holds the resource request/limit strings for a CronJob.
type resourcesResponse struct {
	CPURequest    string `json:"cpu_request"`
	CPULimit      string `json:"cpu_limit"`
	MemoryRequest string `json:"memory_request"`
	MemoryLimit   string `json:"memory_limit"`
}

// cronJobResponse is the enriched JSON shape for GET /api/clusters/{clusterID}/cronjobs.
type cronJobResponse struct {
	ID        string             `json:"id"`
	Namespace string             `json:"namespace"`
	Name      string             `json:"name"`
	Schedule  string             `json:"schedule"`
	Suspended bool               `json:"suspended"`
	NextRunAt *time.Time         `json:"next_run_at,omitempty"`
	Resources resourcesResponse  `json:"resources"`
	LastRun   interface{}        `json:"last_run,omitempty"`
	Stats7d   interface{}        `json:"stats_7d,omitempty"`
}

// ListCronJobs handles GET /api/clusters/{clusterID}/cronjobs.
func (h *Handler) ListCronJobs(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterID")
	ctx := r.Context()

	cronjobs, err := h.store.ListCronJobs(ctx, clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cronjobs")
		return
	}

	resp := make([]cronJobResponse, 0, len(cronjobs))
	for _, cj := range cronjobs {
		item := cronJobResponse{
			ID:        cj.ID,
			Namespace: cj.Namespace,
			Name:      cj.Name,
			Schedule:  cj.Schedule,
			Suspended: cj.Suspended,
			Resources: resourcesResponse{
				CPURequest:    derefStr(cj.CPURequest),
				CPULimit:      derefStr(cj.CPULimit),
				MemoryRequest: derefStr(cj.MemoryRequest),
				MemoryLimit:   derefStr(cj.MemoryLimit),
			},
		}

		// Compute next run time from the cron expression.
		if cj.Schedule != "" {
			if next, err := schedule.NextRun(cj.Schedule, time.Now()); err == nil {
				item.NextRunAt = &next
			}
		}

		// Fetch the last job run for this CronJob.
		if lastRun, err := h.store.GetLastJobRun(ctx, cj.ID); err == nil {
			item.LastRun = lastRun
		}

		// Fetch 7-day stats for this CronJob.
		if stats, err := h.store.GetRunStats7d(ctx, cj.ID); err == nil {
			item.Stats7d = stats
		}

		resp = append(resp, item)
	}

	writeJSON(w, http.StatusOK, resp)
}

// Suspend handles POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/suspend.
// It patches the CronJob in Kubernetes to set spec.suspend=true.
func (h *Handler) Suspend(w http.ResponseWriter, r *http.Request) {
	if err := patchSuspend(r.Context(), h, r, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Resume handles POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/resume.
// It patches the CronJob in Kubernetes to set spec.suspend=false.
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) {
	if err := patchSuspend(r.Context(), h, r, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// patchSuspend performs the Kubernetes MergePatch to flip spec.suspend.
func patchSuspend(ctx context.Context, h *Handler, r *http.Request, suspend bool) error {
	clusterID := r.PathValue("clusterID")
	ns := r.PathValue("ns")
	name := r.PathValue("name")

	cl, ok := h.registry.Get(clusterID)
	if !ok {
		return fmt.Errorf("cluster %q not found", clusterID)
	}

	suspendValue := "true"
	if !suspend {
		suspendValue = "false"
	}
	patch := []byte(`{"spec":{"suspend":` + suspendValue + `}}`)

	_, err := cl.Clientset.BatchV1().CronJobs(ns).Patch(
		ctx, name, types.MergePatchType, patch, metav1.PatchOptions{},
	)
	return err
}

// triggerResponse is the JSON body returned by the trigger endpoint.
type triggerResponse struct {
	RunID   string `json:"run_id"`
	PodName string `json:"pod_name"`
}

// Trigger handles POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/trigger.
// It creates a one-off Job derived from the CronJob's JobTemplate.
func (h *Handler) Trigger(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterID")
	ns := r.PathValue("ns")
	name := r.PathValue("name")
	ctx := r.Context()

	cl, ok := h.registry.Get(clusterID)
	if !ok {
		writeError(w, http.StatusNotFound, "cluster not found")
		return
	}

	cronJob, err := cl.Clientset.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get cronjob")
		return
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name + "-manual-",
			Namespace:    ns,
			Labels: map[string]string{
				"kubecron/trigger": "manual",
			},
		},
		Spec: cronJob.Spec.JobTemplate.Spec,
	}

	created, err := cl.Clientset.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	runID := string(created.UID)
	cronJobID := fmt.Sprintf("%s/%s/%s", clusterID, ns, name)
	run := storage.JobRun{
		ID:        runID,
		CronJobID: cronJobID,
		PodName:   created.Name,
		Trigger:   "manual",
		Status:    "running",
		StartedAt: time.Now(),
	}
	if err := h.store.UpsertJobRun(ctx, run); err != nil {
		slog.Error("failed to pre-insert triggered run", "run_id", runID, "err", err)
	}

	writeJSON(w, http.StatusCreated, triggerResponse{
		RunID:   runID,
		PodName: created.Name,
	})
}

