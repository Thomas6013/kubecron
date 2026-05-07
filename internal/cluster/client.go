package cluster

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ClusterClient holds all Kubernetes client components for a single cluster.
type ClusterClient struct {
	ID              string
	RestConfig      *rest.Config
	Clientset       *kubernetes.Clientset
	InformerFactory informers.SharedInformerFactory
	MetricsClient   metricsv.Interface
	MetricsEnabled  bool // set by probe
}
