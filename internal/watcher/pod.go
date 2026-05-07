package watcher

import (
	"context"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubecron/kubecron/internal/sampler"
	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/internal/streamer"
)

// PodHandler receives Pod informer events, triggers log streaming, metrics
// sampling, and status updates.
type PodHandler struct {
	clusterID      string
	store          storage.Store
	streamer       *streamer.Streamer
	sampler        *sampler.Sampler
	clientset      kubernetes.Interface
	metricsEnabled bool
}

// NewPodHandler creates a PodHandler.
func NewPodHandler(
	clusterID string,
	store storage.Store,
	str *streamer.Streamer,
	smp *sampler.Sampler,
	clientset kubernetes.Interface,
	metricsEnabled bool,
) *PodHandler {
	return &PodHandler{
		clusterID:      clusterID,
		store:          store,
		streamer:       str,
		sampler:        smp,
		clientset:      clientset,
		metricsEnabled: metricsEnabled,
	}
}

// OnAdd handles pods already in a terminal phase when the informer starts or
// when a fast-completing pod is seen for the first time in a terminal state.
func (h *PodHandler) OnAdd(obj interface{}, isInInitialList bool) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	phase := pod.Status.Phase
	if phase != corev1.PodSucceeded && phase != corev1.PodFailed {
		return
	}
	// Delegate to OnUpdate so the full status/log/sampler logic runs.
	h.OnUpdate(nil, obj)
}

// OnUpdate handles pod phase transitions.
func (h *PodHandler) OnUpdate(oldObj, newObj interface{}) {
	pod, ok := newObj.(*corev1.Pod)
	if !ok {
		return
	}

	// Only care about pods owned by a Job.
	jobName, found := jobOwner(pod)
	if !found {
		return
	}

	namespace := pod.Namespace
	podName := pod.Name
	ctx := context.Background()

	// Resolve the runID by scanning running runs and matching pod_name = jobName.
	// The job handler stores the job name as the placeholder pod_name when creating
	// the run record, so this lookup is stable.
	runID := h.findRunID(ctx, jobName)
	if runID == "" {
		// Run not tracked yet — ignore.
		return
	}

	phase := pod.Status.Phase

	switch phase {
	case corev1.PodRunning:
		nodeName := pod.Spec.NodeName

		// Determine the container image from the first container status, if available.
		containerImage := ""
		if len(pod.Status.ContainerStatuses) > 0 {
			containerImage = pod.Status.ContainerStatuses[0].Image
		}

		// UpdateJobRunNode accepts plain strings (empty string is fine for unknown image).
		if err := h.store.UpdateJobRunNode(ctx, runID, nodeName, containerImage); err != nil {
			slog.Warn("failed to update job run node", "runID", runID, "err", err)
		}

		// Start log streaming.
		h.streamer.Stream(ctx, h.clientset, namespace, podName, runID)

		// Start resource sampling if metrics are enabled for this cluster.
		if h.metricsEnabled && h.sampler != nil {
			h.sampler.Start(ctx, h.clusterID, namespace, podName, runID)
		}

	case corev1.PodSucceeded, corev1.PodFailed:
		status := "succeeded"
		if phase == corev1.PodFailed {
			status = "failed"
		}

		finishedAt := time.Now()
		exitCode := 0
		retryCount := 0

		// Extract exit code and restart count from the first container status.
		if len(pod.Status.ContainerStatuses) > 0 {
			cs := pod.Status.ContainerStatuses[0]
			retryCount = int(cs.RestartCount)
			if cs.State.Terminated != nil {
				exitCode = int(cs.State.Terminated.ExitCode)
				if !cs.State.Terminated.FinishedAt.IsZero() {
					finishedAt = cs.State.Terminated.FinishedAt.Time
				}
			}
		}

		if err := h.store.UpdateJobRunStatus(ctx, runID, status, &finishedAt, exitCode, retryCount); err != nil {
			slog.Warn("failed to update job run status", "runID", runID, "status", status, "err", err)
		}

		// Stream logs for fast-completing pods whose PodRunning event was missed.
		// Only fetch if no logs have been captured yet to avoid duplicates.
		if run, err := h.store.GetJobRun(ctx, runID); err == nil && run != nil && run.LogSizeBytes == 0 {
			h.streamer.Stream(ctx, h.clientset, namespace, podName, runID)
		}

		// Stop resource sampling; finalization is handled inside the sampler.
		if h.sampler != nil {
			h.sampler.Stop(runID)
		}
	}
}

// findRunID looks through currently running runs for one whose pod_name matches
// jobName. Returns the run ID or an empty string if not found.
func (h *PodHandler) findRunID(ctx context.Context, jobName string) string {
	runs, err := h.store.GetRunningRuns(ctx)
	if err != nil {
		slog.Warn("failed to fetch running runs", "err", err)
		return ""
	}
	for _, r := range runs {
		if r.PodName == jobName {
			return r.ID
		}
	}
	return ""
}

// OnDelete is a no-op.
func (h *PodHandler) OnDelete(obj interface{}) {}

// jobOwner returns the name of the Job that owns the given pod, if any.
func jobOwner(pod *corev1.Pod) (string, bool) {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "Job" {
			return ref.Name, true
		}
	}
	return "", false
}
