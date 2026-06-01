package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Manager loads cluster configs and registers a ClusterClient for each one in a
// Registry. Kubeconfig files take priority; if none are found the in-cluster
// service account config is used as a single "local" cluster.
type Manager struct {
	registry      *Registry
	store         storage.Store
	kubeconfigDir string
}

// NewManager creates a Manager that will read kubeconfigs from kubeconfigDir.
func NewManager(store storage.Store, kubeconfigDir string) *Manager {
	return &Manager{
		registry:      NewRegistry(),
		store:         store,
		kubeconfigDir: kubeconfigDir,
	}
}

// Load reads every file in the kubeconfig directory, builds a ClusterClient for
// each one, and registers it. If the directory is absent or empty, it falls
// back to the pod's in-cluster service account config ("local" cluster).
func (m *Manager) Load(ctx context.Context) error {
	entries, err := os.ReadDir(m.kubeconfigDir)
	if err != nil {
		slog.Info("kubeconfig directory not found, using in-cluster config", "dir", m.kubeconfigDir)
		return m.loadInCluster(ctx)
	}

	var files []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip dotfiles (.gitkeep, .DS_Store, etc.) and non-kubeconfig extensions.
		if strings.HasPrefix(name, ".") {
			continue
		}
		ext := filepath.Ext(name)
		if ext != "" && ext != ".yaml" && ext != ".yml" {
			continue
		}
		files = append(files, e)
	}

	if len(files) == 0 {
		slog.Info("kubeconfig directory is empty, using in-cluster config")
		return m.loadInCluster(ctx)
	}

	for _, entry := range files {
		filename := entry.Name()
		clusterID := strings.TrimSuffix(filename, filepath.Ext(filename))
		kubeconfigPath := filepath.Join(m.kubeconfigDir, filename)

		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			slog.Error("failed to build config from kubeconfig", "cluster", clusterID, "path", kubeconfigPath, "err", err)
			continue
		}
		if err := m.registerCluster(ctx, clusterID, cfg); err != nil {
			slog.Error("failed to load cluster", "cluster", clusterID, "path", kubeconfigPath, "err", err)
		}
	}

	return nil
}

// Registry returns the underlying registry populated by Load.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// loadInCluster registers the local cluster using the pod's service account.
func (m *Manager) loadInCluster(ctx context.Context) error {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("in-cluster config unavailable: %w", err)
	}
	return m.registerCluster(ctx, "local", cfg)
}

// registerCluster builds a ClusterClient from a rest.Config and registers it.
func (m *Manager) registerCluster(ctx context.Context, clusterID string, cfg *rest.Config) error {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)

	metricsClient, err := metricsv.NewForConfig(cfg)
	if err != nil {
		// Non-fatal: metrics may simply be unavailable.
		slog.Warn("failed to create metrics client", "cluster", clusterID, "err", err)
		metricsClient = nil
	}

	client := &ClusterClient{
		ID:              clusterID,
		RestConfig:      cfg,
		Clientset:       clientset,
		InformerFactory: factory,
		MetricsClient:   metricsClient,
	}

	m.registry.Register(client)

	if err := m.store.UpsertCluster(ctx, storage.Cluster{
		ID:        clusterID,
		Name:      clusterID,
		CreatedAt: time.Now(),
	}); err != nil {
		slog.Error("failed to upsert cluster in store", "cluster", clusterID, "err", err)
	}

	slog.Info("cluster loaded", "cluster", clusterID)
	return nil
}
