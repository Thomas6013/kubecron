package cluster

import (
	"sync/atomic"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ClusterClient holds all Kubernetes client components for a single cluster.
type ClusterClient struct {
	ID              string
	RestConfig      *rest.Config
	Clientset       kubernetes.Interface
	InformerFactory informers.SharedInformerFactory
	MetricsClient   metricsv.Interface

	// metricsEnabled is set asynchronously by the metrics probe and read by the
	// pod watcher to decide whether to start resource sampling. It is an
	// atomic.Bool because the probe goroutine writes it while informer event
	// handlers read it concurrently.
	metricsEnabled atomic.Bool
}

// MetricsEnabled reports whether the Metrics API was reachable at the last probe.
func (c *ClusterClient) MetricsEnabled() bool { return c.metricsEnabled.Load() }

// SetMetricsEnabled records the result of a metrics-API probe.
func (c *ClusterClient) SetMetricsEnabled(v bool) { c.metricsEnabled.Store(v) }
