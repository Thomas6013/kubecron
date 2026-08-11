package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// addRunWithResources inserts a finished run carrying peak CPU and memory
// samples, so the resource rankings have something to order by.
func addRunWithResources(
	t *testing.T,
	store storage.Store,
	runID, cronJobID, status string,
	ago time.Duration,
	durationMs, cpuMillicores, memoryBytes int64,
) {
	t.Helper()
	ctx := context.Background()
	addFinishedRun(t, store, runID, cronJobID, status, ago, durationMs)

	// FinalizeResourceUsage derives the run's avg/max columns from the samples,
	// which is the same path the sampler drives in production.
	if err := store.InsertResourceSample(ctx, runID, cpuMillicores, memoryBytes); err != nil {
		t.Fatalf("InsertResourceSample(%s): %v", runID, err)
	}
	if err := store.FinalizeResourceUsage(ctx, runID); err != nil {
		t.Fatalf("FinalizeResourceUsage(%s): %v", runID, err)
	}
}

func TestGetFleetStats_CountsOutcomesAcrossClusters(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seed(t, store, "c1", "c1/default/test-cron")
	seedCronJob(t, store, "c1", "c1/default/other-cron", "other-cron")
	seed(t, store, "c2", "c2/default/test-cron")

	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", 2*time.Hour, 1000)
	addFinishedRun(t, store, "r2", "c1/default/test-cron", "failed", 1*time.Hour, 1000)
	addFinishedRun(t, store, "r3", "c1/default/other-cron", "failed", 1*time.Hour, 1000)
	addFinishedRun(t, store, "r4", "c2/default/test-cron", "succeeded", 1*time.Hour, 1000)

	got, err := store.GetFleetStats(ctx, "", 7)
	if err != nil {
		t.Fatalf("GetFleetStats: %v", err)
	}

	if got.Clusters != 2 {
		t.Errorf("Clusters = %d, want 2", got.Clusters)
	}
	if got.CronJobs != 3 {
		t.Errorf("CronJobs = %d, want 3", got.CronJobs)
	}
	if got.Runs != 4 {
		t.Errorf("Runs = %d, want 4", got.Runs)
	}
	if got.Succeeded != 2 {
		t.Errorf("Succeeded = %d, want 2", got.Succeeded)
	}
	if got.Failed != 2 {
		t.Errorf("Failed = %d, want 2", got.Failed)
	}
	// Two failures spread over two different CronJobs.
	if got.FailingCronJob != 2 {
		t.Errorf("FailingCronJob = %d, want 2", got.FailingCronJob)
	}
	if want := 50.0; got.SuccessRate() != want {
		t.Errorf("SuccessRate() = %v, want %v", got.SuccessRate(), want)
	}
}

// A run still in flight has no outcome, so it must not be scored as a failure —
// otherwise every busy window would read as a partial outage.
func TestGetFleetStats_RunningRunsExcludedFromSuccessRate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)
	if err := store.UpsertJobRun(ctx, storage.JobRun{
		ID: "r2", CronJobID: "c1/default/test-cron", PodName: "pod-r2",
		Trigger: "scheduled", Status: "running", StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertJobRun: %v", err)
	}

	got, err := store.GetFleetStats(ctx, "", 7)
	if err != nil {
		t.Fatalf("GetFleetStats: %v", err)
	}
	if got.Running != 1 {
		t.Errorf("Running = %d, want 1", got.Running)
	}
	if got.Runs != 2 {
		t.Errorf("Runs = %d, want 2", got.Runs)
	}
	if got.SuccessRate() != 100 {
		t.Errorf("SuccessRate() = %v, want 100 (running run must not count as failed)", got.SuccessRate())
	}
}

// Deleting a CronJob keeps its run history on purpose, so the fleet totals must
// join through `cronjobs` and drop those runs rather than counting a job that
// no longer exists.
func TestGetFleetStats_ExcludesDeletedCronJobs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	seedCronJob(t, store, "c1", "c1/default/gone-cron", "gone-cron")

	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)
	addFinishedRun(t, store, "r2", "c1/default/gone-cron", "failed", time.Hour, 1000)

	if err := store.MarkCronJobDeleted(ctx, "c1/default/gone-cron"); err != nil {
		t.Fatalf("MarkCronJobDeleted: %v", err)
	}

	got, err := store.GetFleetStats(ctx, "", 7)
	if err != nil {
		t.Fatalf("GetFleetStats: %v", err)
	}
	if got.CronJobs != 1 {
		t.Errorf("CronJobs = %d, want 1", got.CronJobs)
	}
	if got.Runs != 1 {
		t.Errorf("Runs = %d, want 1 (deleted CronJob's run must be excluded)", got.Runs)
	}
	if got.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (deleted CronJob's failure must be excluded)", got.Failed)
	}
}

// Runs outside the requested window must not be counted, or the range switch
// on the overview would show the same numbers for 24h and 30d.
func TestGetFleetStats_RespectsWindow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	addFinishedRun(t, store, "recent", "c1/default/test-cron", "succeeded", 2*time.Hour, 1000)
	addFinishedRun(t, store, "old", "c1/default/test-cron", "failed", 10*24*time.Hour, 1000)

	within1, err := store.GetFleetStats(ctx, "", 1)
	if err != nil {
		t.Fatalf("GetFleetStats(1): %v", err)
	}
	if within1.Runs != 1 || within1.Failed != 0 {
		t.Errorf("1-day window: Runs=%d Failed=%d, want 1 and 0", within1.Runs, within1.Failed)
	}

	within30, err := store.GetFleetStats(ctx, "", 30)
	if err != nil {
		t.Fatalf("GetFleetStats(30): %v", err)
	}
	if within30.Runs != 2 || within30.Failed != 1 {
		t.Errorf("30-day window: Runs=%d Failed=%d, want 2 and 1", within30.Runs, within30.Failed)
	}
}

func TestGetTopCronJobs_RanksByEachMetric(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	seedCronJob(t, store, "c1", "c1/default/hungry", "hungry")
	seedCronJob(t, store, "c1", "c1/default/slow", "slow")

	// hungry: modest duration, heavy resources.
	addRunWithResources(t, store, "r1", "c1/default/hungry", "succeeded", time.Hour, 1_000, 900, 800<<20)
	// slow: long duration, light resources.
	addRunWithResources(t, store, "r2", "c1/default/slow", "succeeded", time.Hour, 600_000, 50, 10<<20)
	// test-cron: two failures, nothing remarkable otherwise.
	addRunWithResources(t, store, "r3", "c1/default/test-cron", "failed", time.Hour, 2_000, 100, 20<<20)
	addRunWithResources(t, store, "r4", "c1/default/test-cron", "failed", time.Hour, 2_000, 100, 20<<20)

	cases := []struct {
		metric    storage.RankMetric
		wantFirst string
		wantValue int64
	}{
		{storage.RankByCPU, "hungry", 900},
		{storage.RankByMemory, "hungry", 800 << 20},
		{storage.RankByDuration, "slow", 600_000},
		{storage.RankByFailures, "test-cron", 2},
	}
	for _, tc := range cases {
		t.Run(string(tc.metric), func(t *testing.T) {
			ranks, err := store.GetTopCronJobs(ctx, "", tc.metric, 7, 5)
			if err != nil {
				t.Fatalf("GetTopCronJobs(%s): %v", tc.metric, err)
			}
			if len(ranks) == 0 {
				t.Fatalf("GetTopCronJobs(%s) returned no rows", tc.metric)
			}
			if ranks[0].Name != tc.wantFirst {
				t.Errorf("top by %s = %q, want %q", tc.metric, ranks[0].Name, tc.wantFirst)
			}
			if ranks[0].Value != tc.wantValue {
				t.Errorf("top by %s value = %d, want %d", tc.metric, ranks[0].Value, tc.wantValue)
			}
			// Rankings must be ordered, since the bar widths are scaled against
			// the leader and would misrepresent an unsorted list.
			for i := 1; i < len(ranks); i++ {
				if ranks[i].Value > ranks[i-1].Value {
					t.Errorf("ranks not descending at %d: %d > %d", i, ranks[i].Value, ranks[i-1].Value)
				}
			}
		})
	}
}

// Only CronJobs that actually failed belong in the failures ranking; padding it
// with zero-failure rows would make a healthy fleet look ranked.
func TestGetTopCronJobs_OmitsZeroValues(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)

	ranks, err := store.GetTopCronJobs(ctx, "", storage.RankByFailures, 7, 5)
	if err != nil {
		t.Fatalf("GetTopCronJobs: %v", err)
	}
	if len(ranks) != 0 {
		t.Errorf("got %d rows, want 0 (no failures recorded)", len(ranks))
	}
}

func TestGetTopCronJobs_RejectsUnknownMetric(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetTopCronJobs(context.Background(), "", storage.RankMetric("; DROP TABLE job_runs"), 7, 5); err == nil {
		t.Fatal("GetTopCronJobs accepted an unknown rank metric, want error")
	}
}

func TestGetTopCronJobs_RespectsLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	for i, name := range []string{"a", "b", "c", "d"} {
		id := "c1/default/" + name
		seedCronJob(t, store, "c1", id, name)
		addFinishedRun(t, store, "r"+name, id, "failed", time.Hour, int64(i+1)*1000)
	}

	ranks, err := store.GetTopCronJobs(ctx, "", storage.RankByDuration, 7, 2)
	if err != nil {
		t.Fatalf("GetTopCronJobs: %v", err)
	}
	if len(ranks) != 2 {
		t.Errorf("got %d rows, want 2", len(ranks))
	}
}

// Scoping is what lets the overview and the cluster view share one query, so
// a non-empty clusterID must confine every counter to that cluster.
func TestGetFleetStats_ScopesToCluster(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seed(t, store, "c1", "c1/default/test-cron")
	seedCronJob(t, store, "c1", "c1/default/other-cron", "other-cron")
	seed(t, store, "c2", "c2/default/test-cron")

	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)
	addFinishedRun(t, store, "r2", "c1/default/other-cron", "failed", time.Hour, 1000)
	addFinishedRun(t, store, "r3", "c2/default/test-cron", "failed", time.Hour, 1000)

	got, err := store.GetFleetStats(ctx, "c1", 7)
	if err != nil {
		t.Fatalf("GetFleetStats(c1): %v", err)
	}
	if got.Clusters != 1 {
		t.Errorf("Clusters = %d, want 1", got.Clusters)
	}
	if got.CronJobs != 2 {
		t.Errorf("CronJobs = %d, want 2 (c2's CronJob must be excluded)", got.CronJobs)
	}
	if got.Runs != 2 {
		t.Errorf("Runs = %d, want 2 (c2's run must be excluded)", got.Runs)
	}
	if got.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (c2's failure must be excluded)", got.Failed)
	}

	// The unscoped call over the same data still spans both clusters.
	all, err := store.GetFleetStats(ctx, "", 7)
	if err != nil {
		t.Fatalf("GetFleetStats(all): %v", err)
	}
	if all.Runs != 3 || all.Clusters != 2 {
		t.Errorf("unscoped: Runs=%d Clusters=%d, want 3 and 2", all.Runs, all.Clusters)
	}
}

func TestGetTopCronJobs_ScopesToCluster(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seed(t, store, "c1", "c1/default/test-cron")
	seed(t, store, "c2", "c2/default/test-cron")

	// c2's run is the fleet-wide leader by duration, so a scoped query that
	// leaked would surface it under c1.
	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1_000)
	addFinishedRun(t, store, "r2", "c2/default/test-cron", "succeeded", time.Hour, 900_000)

	scoped, err := store.GetTopCronJobs(ctx, "c1", storage.RankByDuration, 7, 5)
	if err != nil {
		t.Fatalf("GetTopCronJobs(c1): %v", err)
	}
	if len(scoped) != 1 {
		t.Fatalf("got %d rows, want 1", len(scoped))
	}
	if scoped[0].ClusterID != "c1" || scoped[0].Value != 1_000 {
		t.Errorf("scoped top = cluster %q value %d, want c1 and 1000",
			scoped[0].ClusterID, scoped[0].Value)
	}

	all, err := store.GetTopCronJobs(ctx, "", storage.RankByDuration, 7, 5)
	if err != nil {
		t.Fatalf("GetTopCronJobs(all): %v", err)
	}
	if len(all) != 2 || all[0].ClusterID != "c2" {
		t.Errorf("unscoped: got %d rows led by %q, want 2 led by c2", len(all), all[0].ClusterID)
	}
}
