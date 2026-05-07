package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/internal/streamer"
)

const labelTrigger = "kubecron/trigger"

// JobHandler receives Job informer events and creates job_run records for Jobs
// owned by a CronJob.
type JobHandler struct {
	clusterID string
	store     storage.Store
	clientset kubernetes.Interface
	streamer  *streamer.Streamer
}

// NewJobHandler creates a handler for the given cluster.
func NewJobHandler(clusterID string, store storage.Store, clientset kubernetes.Interface, str *streamer.Streamer) *JobHandler {
	return &JobHandler{
		clusterID: clusterID,
		store:     store,
		clientset: clientset,
		streamer:  str,
	}
}

// OnAdd handles a newly observed Job. If it is owned by a CronJob a new run
// record is created in the store.
func (h *JobHandler) OnAdd(obj interface{}, isInInitialList bool) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return
	}

	cronJobName, found := cronJobOwner(job)
	if !found {
		return
	}

	namespace := job.Namespace
	cronJobID := fmt.Sprintf("%s/%s/%s", h.clusterID, namespace, cronJobName)
	trigger := "scheduled"
	if job.Labels[labelTrigger] == "manual" {
		trigger = "manual"
	}
	runID := string(job.UID)
	ctx := context.Background()

	if isInInitialList && jobIsDone(job) {
		// Job already finished — backfill a run record if we have no record yet.
		h.backfillRun(ctx, job, namespace, cronJobID, trigger, runID)
		return
	}

	run := storage.JobRun{
		ID:        runID,
		CronJobID: cronJobID,
		PodName:   job.Name,
		Trigger:   trigger,
		Status:    "running",
		StartedAt: time.Now(),
	}

	if err := h.store.UpsertJobRun(ctx, run); err != nil {
		slog.Error("failed to upsert job run",
			"cluster", h.clusterID,
			"namespace", namespace,
			"job", job.Name,
			"err", err,
		)
	}
}

// backfillRun creates a historical run record for a Job that completed before
// (or during) kubecron startup, and attempts to recover logs from the pod.
func (h *JobHandler) backfillRun(ctx context.Context, job *batchv1.Job, namespace, cronJobID, trigger, runID string) {
	// If the run is already correctly marked as succeeded, leave it.
	// A 'failed' status here may have been set by the startup cleanup (which uses
	// exit_code=-1 as sentinel); in that case we overwrite with the true K8s status.
	if existing, err := h.store.GetJobRun(ctx, runID); err == nil && existing != nil && existing.Status == "succeeded" {
		return
	}

	status := "failed"
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			status = "succeeded"
			break
		}
	}

	startedAt := time.Now()
	if job.Status.StartTime != nil {
		startedAt = job.Status.StartTime.Time
	}

	run := storage.JobRun{
		ID:        runID,
		CronJobID: cronJobID,
		PodName:   job.Name,
		Trigger:   trigger,
		Status:    status,
		StartedAt: startedAt,
	}
	if err := h.store.UpsertJobRun(ctx, run); err != nil {
		slog.Warn("backfill: failed to upsert run", "job", job.Name, "err", err)
		return
	}

	// Persist the finish time.
	var finishedAt *time.Time
	if job.Status.CompletionTime != nil {
		t := job.Status.CompletionTime.Time
		finishedAt = &t
	}
	if finishedAt != nil {
		if err := h.store.UpdateJobRunStatus(ctx, runID, status, finishedAt, 0, 0); err != nil {
			slog.Warn("backfill: failed to set finished_at", "job", job.Name, "err", err)
		}
	}

	slog.Info("backfilled historical run", "job", job.Name, "status", status)

	// Best-effort: look up the pod to recover exit code, node, image, and logs.
	pods, err := h.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + job.Name,
	})
	if err != nil || len(pods.Items) == 0 {
		return
	}
	pod := &pods.Items[0]

	nodeName := pod.Spec.NodeName
	image := ""
	exitCode := 0
	if len(pod.Status.ContainerStatuses) > 0 {
		cs := pod.Status.ContainerStatuses[0]
		image = cs.Image
		if cs.State.Terminated != nil {
			exitCode = int(cs.State.Terminated.ExitCode)
			if finishedAt == nil && !cs.State.Terminated.FinishedAt.IsZero() {
				t := cs.State.Terminated.FinishedAt.Time
				finishedAt = &t
			}
		}
	}
	if err := h.store.UpdateJobRunNode(ctx, runID, nodeName, image); err != nil {
		slog.Warn("backfill: failed to update node", "job", job.Name, "err", err)
	}
	if err := h.store.UpdateJobRunStatus(ctx, runID, status, finishedAt, exitCode, 0); err != nil {
		slog.Warn("backfill: failed to update exit code", "job", job.Name, "err", err)
	}

	// Stream logs if the pod still has them.
	if h.streamer != nil {
		h.streamer.Stream(ctx, h.clientset, namespace, pod.Name, runID)
	}
}

// OnUpdate is a no-op — job status updates are handled by the pod watcher.
func (h *JobHandler) OnUpdate(oldObj, newObj interface{}) {}

// OnDelete marks the associated run as failed if it is still 'running' when
// the K8s Job is garbage-collected (e.g. successfulJobsHistoryLimit cleanup).
func (h *JobHandler) OnDelete(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return
	}
	runID := string(job.UID)
	ctx := context.Background()
	existing, err := h.store.GetJobRun(ctx, runID)
	if err != nil || existing == nil || existing.Status != "running" {
		return
	}
	if err := h.store.MarkRunFailed(ctx, runID); err != nil {
		slog.Warn("failed to mark deleted job run as failed", "job", job.Name, "err", err)
	}
}

// jobIsDone returns true if the Job has fully completed (succeeded or failed).
// CompletionTime alone is unreliable — older K8s versions only set it on success.
func jobIsDone(job *batchv1.Job) bool {
	if job.Status.CompletionTime != nil {
		return true
	}
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue &&
			(c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) {
			return true
		}
	}
	return false
}

// cronJobOwner returns the name of the CronJob that owns the given job, if any.
func cronJobOwner(job *batchv1.Job) (string, bool) {
	for _, ref := range job.OwnerReferences {
		if ref.Kind == "CronJob" {
			return ref.Name, true
		}
	}
	return "", false
}
