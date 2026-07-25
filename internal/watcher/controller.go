package watcher

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"github.com/kubecron/kubecron/internal/cluster"
	"github.com/kubecron/kubecron/internal/sampler"
	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/internal/streamer"
)

// Controller starts all informers (CronJob, Job, Pod) for a single cluster and
// wires up the corresponding event handlers.
type Controller struct {
	clusterID    string
	client       *cluster.ClusterClient
	store        storage.Store
	streamerInst *streamer.Streamer
	samplerInst  *sampler.Sampler
	hasSynced    []cache.InformerSynced
}

// NewController creates a Controller for the given cluster.
func NewController(
	client *cluster.ClusterClient,
	store storage.Store,
	streamerInst *streamer.Streamer,
	samplerInst *sampler.Sampler,
) *Controller {
	return &Controller{
		clusterID:    client.ID,
		client:       client,
		store:        store,
		streamerInst: streamerInst,
		samplerInst:  samplerInst,
	}
}

// Start registers event handlers on the informers and starts the shared
// informer factory. It returns immediately — use CacheSynced() to check
// whether the initial list has been processed (for /readyz).
func (c *Controller) Start(ctx context.Context) error {
	factory := c.client.InformerFactory

	// ── CronJob informer ──────────────────────────────────────────────────────
	cronJobInformer := factory.Batch().V1().CronJobs().Informer()
	cronJobLister := factory.Batch().V1().CronJobs().Lister()
	cronJobHandler := NewCronJobHandler(c.clusterID, c.store)
	if _, err := cronJobInformer.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
		AddFunc:    cronJobHandler.OnAdd,
		UpdateFunc: cronJobHandler.OnUpdate,
		DeleteFunc: cronJobHandler.OnDelete,
	}); err != nil {
		return fmt.Errorf("add cronjob event handler: %w", err)
	}

	// ── Job informer ──────────────────────────────────────────────────────────
	runIndex := NewRunIndex()

	jobInformer := factory.Batch().V1().Jobs().Informer()
	jobHandler := NewJobHandler(c.clusterID, c.store, c.client.Clientset, c.streamerInst, runIndex)
	if _, err := jobInformer.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
		AddFunc:    jobHandler.OnAdd,
		UpdateFunc: jobHandler.OnUpdate,
		DeleteFunc: jobHandler.OnDelete,
	}); err != nil {
		return fmt.Errorf("add job event handler: %w", err)
	}

	// ── Pod informer ──────────────────────────────────────────────────────────
	podInformer := factory.Core().V1().Pods().Informer()
	podHandler := NewPodHandler(
		c.clusterID,
		c.store,
		c.streamerInst,
		c.samplerInst,
		c.client.Clientset,
		c.client.MetricsEnabled,
		runIndex,
	) // c.client.MetricsEnabled is a func() bool, read live by the handler
	if _, err := podInformer.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
		AddFunc:    podHandler.OnAdd,
		UpdateFunc: podHandler.OnUpdate,
		DeleteFunc: podHandler.OnDelete,
	}); err != nil {
		return fmt.Errorf("add pod event handler: %w", err)
	}

	// Start the factory goroutines; they run until ctx is cancelled.
	factory.Start(ctx.Done())

	// Capture the synced predicates for use by CacheSynced().
	c.hasSynced = []cache.InformerSynced{
		cronJobInformer.HasSynced,
		jobInformer.HasSynced,
		podInformer.HasSynced,
	}

	// Once the CronJob cache holds the full cluster state, reconcile it against
	// the store to catch CronJobs deleted while KubeCron was not running — the
	// informer never reports those (BUG-20).
	go func() {
		if !cache.WaitForCacheSync(ctx.Done(), cronJobInformer.HasSynced) {
			return // context cancelled during startup
		}
		live, err := cronJobLister.List(labels.Everything())
		if err != nil {
			slog.Error("cronjob reconcile: failed to list informer cache", "cluster", c.clusterID, "err", err)
			return
		}
		cronJobHandler.Reconcile(ctx, live)
	}()

	return nil
}

// CacheSynced returns true once the initial list from all three informers has
// been processed. Used by the /readyz endpoint.
func (c *Controller) CacheSynced() bool {
	for _, fn := range c.hasSynced {
		if !fn() {
			return false
		}
	}
	return len(c.hasSynced) > 0
}
