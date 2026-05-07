package sampler

import (
	"context"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/kubecron/kubecron/internal/storage"
)

const probeInterval = 5 * time.Minute

// probe checks whether the Metrics API is reachable on the given cluster.
// On success it marks the cluster as metrics-enabled in the store.
func probe(ctx context.Context, clusterID string, metricsClient metricsv.Interface, store storage.Store) {
	_, err := metricsClient.MetricsV1beta1().PodMetricses("default").List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		slog.Info("metrics API not available", "cluster", clusterID, "err", err)
		return
	}
	if err := store.SetClusterMetricsEnabled(ctx, clusterID, true); err != nil {
		slog.Warn("failed to mark cluster metrics enabled", "cluster", clusterID, "err", err)
	}
}

// StartProbe runs an initial probe immediately and then re-probes every 5 minutes
// in the background until ctx is cancelled.
func StartProbe(ctx context.Context, clusterID string, metricsClient metricsv.Interface, store storage.Store) {
	if metricsClient == nil {
		slog.Info("metrics client unavailable, skipping probe", "cluster", clusterID)
		return
	}

	// Run an immediate probe.
	probe(ctx, clusterID, metricsClient, store)

	go func() {
		ticker := time.NewTicker(probeInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				probe(ctx, clusterID, metricsClient, store)
			case <-ctx.Done():
				return
			}
		}
	}()
}
