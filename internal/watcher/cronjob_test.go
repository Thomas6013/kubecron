package watcher

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/kubecron/kubecron/internal/storage"
)

func makeCronJob(namespace, name, schedule string, timeZone *string) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: batchv1.CronJobSpec{
			Schedule: schedule,
			TimeZone: timeZone,
		},
	}
}

// liveCronJobs returns the CronJobs the store still considers present.
func liveCronJobs(t *testing.T, store storage.Store, clusterID string) []storage.CronJob {
	t.Helper()
	cjs, err := store.ListCronJobs(context.Background(), clusterID)
	if err != nil {
		t.Fatalf("ListCronJobs: %v", err)
	}
	return cjs
}

// TestCronJobHandler_OnAdd_PersistsTimeZone verifies that spec.timeZone reaches
// the store, which is what makes next-run and missed detection agree with the
// Kubernetes controller (DOM-1).
func TestCronJobHandler_OnAdd_PersistsTimeZone(t *testing.T) {
	store := newTestStore(t)
	h := NewCronJobHandler("c1", store)

	tz := "America/New_York"
	h.OnAdd(makeCronJob("default", "tz-cron", "0 4 * * *", &tz), false)

	cj, err := store.GetCronJobByName(context.Background(), "c1", "default", "tz-cron")
	if err != nil {
		t.Fatalf("GetCronJobByName: %v", err)
	}
	if cj == nil {
		t.Fatal("expected cronjob to be persisted")
	}
	if cj.TZ() != tz {
		t.Errorf("TimeZone = %q, want %q", cj.TZ(), tz)
	}
}

// TestCronJobHandler_OnAdd_NoTimeZone verifies a CronJob without spec.timeZone
// stores no zone rather than an empty string, so TZ() reports "unpinned".
func TestCronJobHandler_OnAdd_NoTimeZone(t *testing.T) {
	store := newTestStore(t)
	h := NewCronJobHandler("c1", store)

	h.OnAdd(makeCronJob("default", "plain-cron", "0 4 * * *", nil), false)

	cj, _ := store.GetCronJobByName(context.Background(), "c1", "default", "plain-cron")
	if cj == nil {
		t.Fatal("expected cronjob to be persisted")
	}
	if cj.TimeZone != nil {
		t.Errorf("TimeZone = %v, want nil", *cj.TimeZone)
	}
}

// TestCronJobHandler_OnDelete_HidesFromListings verifies a deleted CronJob stops
// appearing in listings but keeps its run history reachable (BUG-20).
func TestCronJobHandler_OnDelete_HidesFromListings(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	h := NewCronJobHandler("c1", store)

	h.OnAdd(makeCronJob("default", "doomed", "* * * * *", nil), false)
	if err := store.UpsertJobRun(ctx, storage.JobRun{
		ID:        "run-1",
		CronJobID: "c1/default/doomed",
		PodName:   "doomed-abc",
		Trigger:   "scheduled",
		Status:    "succeeded",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertJobRun: %v", err)
	}

	h.OnDelete(makeCronJob("default", "doomed", "* * * * *", nil))

	for _, cj := range liveCronJobs(t, store, "c1") {
		if cj.Name == "doomed" {
			t.Error("deleted cronjob must not appear in listings")
		}
	}

	// History survives: the row is still readable by direct lookup, and its runs
	// were not cascaded away.
	cj, _ := store.GetCronJobByName(ctx, "c1", "default", "doomed")
	if cj == nil {
		t.Fatal("deleted cronjob row must be preserved for history")
	}
	if cj.DeletedAt == nil {
		t.Error("expected DeletedAt to be set")
	}
	if run, _ := store.GetJobRun(ctx, "run-1"); run == nil {
		t.Error("run history must survive CronJob deletion")
	}
}

// TestCronJobHandler_OnDelete_Tombstone verifies the informer's
// DeletedFinalStateUnknown wrapper is unwrapped rather than dropped, which is
// how deletions observed across a watch gap arrive.
func TestCronJobHandler_OnDelete_Tombstone(t *testing.T) {
	store := newTestStore(t)
	h := NewCronJobHandler("c1", store)

	h.OnAdd(makeCronJob("default", "ghost", "* * * * *", nil), false)
	h.OnDelete(cache.DeletedFinalStateUnknown{
		Key: "default/ghost",
		Obj: makeCronJob("default", "ghost", "* * * * *", nil),
	})

	for _, cj := range liveCronJobs(t, store, "c1") {
		if cj.Name == "ghost" {
			t.Error("tombstoned cronjob must be marked deleted")
		}
	}
}

// TestCronJobHandler_Recreated_IsRevived verifies that a CronJob deleted and
// recreated under the same name comes back, rather than staying hidden forever.
func TestCronJobHandler_Recreated_IsRevived(t *testing.T) {
	store := newTestStore(t)
	h := NewCronJobHandler("c1", store)

	cj := makeCronJob("default", "phoenix", "* * * * *", nil)
	h.OnAdd(cj, false)
	h.OnDelete(cj)
	h.OnAdd(cj, false)

	var found bool
	for _, c := range liveCronJobs(t, store, "c1") {
		if c.Name == "phoenix" {
			found = true
		}
	}
	if !found {
		t.Error("recreated cronjob must reappear in listings")
	}
}

// TestCronJobHandler_Reconcile verifies that CronJobs removed while KubeCron was
// not running are caught at startup — the informer never reports those, so
// without reconciliation they would linger as ghost rows forever (BUG-20).
func TestCronJobHandler_Reconcile(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	h := NewCronJobHandler("c1", store)

	kept := makeCronJob("default", "kept", "* * * * *", nil)
	h.OnAdd(kept, false)
	h.OnAdd(makeCronJob("default", "vanished", "* * * * *", nil), false)

	// The informer cache holds only what still exists in the cluster.
	h.Reconcile(ctx, []*batchv1.CronJob{kept})

	live := liveCronJobs(t, store, "c1")
	names := map[string]bool{}
	for _, cj := range live {
		names[cj.Name] = true
	}
	if !names["kept"] {
		t.Error("cronjob still present in the cluster must stay live")
	}
	if names["vanished"] {
		t.Error("cronjob absent from the cluster must be marked deleted")
	}
	// newTestStore seeds "my-cron", which is also absent from the cache.
	if names["my-cron"] {
		t.Error("seeded cronjob absent from the cache must be marked deleted too")
	}
}

// TestCronJobHandler_Reconcile_EmptyCluster verifies reconciliation of a cluster
// that genuinely has no CronJobs left marks everything deleted (as opposed to
// silently skipping, which would preserve ghosts).
func TestCronJobHandler_Reconcile_EmptyCluster(t *testing.T) {
	store := newTestStore(t)
	h := NewCronJobHandler("c1", store)

	h.Reconcile(context.Background(), nil)

	if live := liveCronJobs(t, store, "c1"); len(live) != 0 {
		t.Errorf("expected no live cronjobs, got %d", len(live))
	}
}
