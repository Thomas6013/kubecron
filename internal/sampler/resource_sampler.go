package sampler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/kubecron/kubecron/internal/storage"
)

// Sampler periodically collects CPU/memory samples for active job runs using
// the Kubernetes Metrics API.
type Sampler struct {
	store         storage.Store
	metricsClient metricsv.Interface
	interval      time.Duration
	mu            sync.Mutex
	active        map[string]context.CancelFunc // runID → cancel
}

// NewSampler creates a Sampler with the given polling interval.
func NewSampler(store storage.Store, metricsClient metricsv.Interface, interval time.Duration) *Sampler {
	return &Sampler{
		store:         store,
		metricsClient: metricsClient,
		interval:      interval,
		active:        make(map[string]context.CancelFunc),
	}
}

// Start begins periodic sampling for runID. If sampling is already active for
// this run the call is a no-op.
func (s *Sampler) Start(ctx context.Context, clusterID, namespace, podName, runID string) {
	if s.metricsClient == nil {
		return
	}

	s.mu.Lock()
	if _, exists := s.active[runID]; exists {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.active[runID] = cancel
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.active, runID)
			s.mu.Unlock()
			s.finalize(context.Background(), runID)
		}()

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.sample(runCtx, namespace, podName, runID)
			case <-runCtx.Done():
				return
			}
		}
	}()
}

// Stop cancels the sampling goroutine for runID, which will trigger finalization.
func (s *Sampler) Stop(runID string) {
	s.mu.Lock()
	cancel, ok := s.active[runID]
	s.mu.Unlock()

	if ok {
		cancel()
	}
}

// sample collects one data point for the pod and persists it.
func (s *Sampler) sample(ctx context.Context, namespace, podName, runID string) {
	podMetrics, err := s.metricsClient.MetricsV1beta1().PodMetricses(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		slog.Warn("metrics sample failed", "pod", podName, "runID", runID, "err", err)
		return
	}

	var cpuMillicores, memoryBytes int64
	for _, c := range podMetrics.Containers {
		cpuMillicores += c.Usage.Cpu().MilliValue()
		memoryBytes += c.Usage.Memory().Value()
	}

	if err := s.store.InsertResourceSample(ctx, runID, cpuMillicores, memoryBytes); err != nil {
		slog.Warn("failed to insert resource sample", "runID", runID, "err", err)
	}
}

// finalize computes and persists aggregate resource statistics for the run.
func (s *Sampler) finalize(ctx context.Context, runID string) {
	if err := s.store.FinalizeResourceUsage(ctx, runID); err != nil {
		slog.Warn("failed to finalize resource usage", "runID", runID, "err", err)
	}
}
