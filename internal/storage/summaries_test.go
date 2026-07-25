package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// seedCronJob adds one more CronJob to an existing cluster.
func seedCronJob(t *testing.T, store storage.Store, clusterID, id, name string) {
	t.Helper()
	if err := store.UpsertCronJob(context.Background(), storage.CronJob{
		ID:        id,
		ClusterID: clusterID,
		Namespace: "default",
		Name:      name,
		Schedule:  "* * * * *",
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCronJob(%s): %v", id, err)
	}
}

// addFinishedRun inserts a completed run that started `ago` before now and took
// durationMs milliseconds.
func addFinishedRun(t *testing.T, store storage.Store, runID, cronJobID, status string, ago time.Duration, durationMs int64) {
	t.Helper()
	ctx := context.Background()
	started := time.Now().Add(-ago)
	if err := store.UpsertJobRun(ctx, storage.JobRun{
		ID: runID, CronJobID: cronJobID,
		PodName: "pod-" + runID, Trigger: "scheduled", Status: "running", StartedAt: started,
	}); err != nil {
		t.Fatalf("UpsertJobRun(%s): %v", runID, err)
	}
	finished := started.Add(time.Duration(durationMs) * time.Millisecond)
	if err := store.UpdateJobRunStatus(ctx, runID, status, &finished, 0, 0); err != nil {
		t.Fatalf("UpdateJobRunStatus(%s): %v", runID, err)
	}
}

// TestGetCronJobSummaries_MatchesPerCronJobQueries is the parity test for
// PERF-2: the batched cluster-wide read must return exactly what the
// per-CronJob queries it replaces returned, or the refactor changed the UI.
func TestGetCronJobSummaries_MatchesPerCronJobQueries(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	seedCronJob(t, store, "c1", "c1/default/other-cron", "other-cron")

	// test-cron: three runs, mixed outcomes, newest last.
	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", 3*time.Hour, 1000)
	addFinishedRun(t, store, "r2", "c1/default/test-cron", "failed", 2*time.Hour, 2000)
	addFinishedRun(t, store, "r3", "c1/default/test-cron", "succeeded", 1*time.Hour, 3000)
	// other-cron: a single run.
	addFinishedRun(t, store, "r4", "c1/default/other-cron", "succeeded", 30*time.Minute, 500)

	summaries, err := store.GetCronJobSummaries(ctx, "c1", 20)
	if err != nil {
		t.Fatalf("GetCronJobSummaries: %v", err)
	}

	for _, id := range []string{"c1/default/test-cron", "c1/default/other-cron"} {
		sum := summaries[id]
		if sum == nil {
			t.Fatalf("%s: missing summary", id)
		}

		wantLast, err := store.GetLastJobRun(ctx, id)
		if err != nil {
			t.Fatalf("GetLastJobRun: %v", err)
		}
		if sum.LastRun == nil || wantLast == nil {
			t.Fatalf("%s: last run nil (batched=%v, per-cronjob=%v)", id, sum.LastRun, wantLast)
		}
		if sum.LastRun.ID != wantLast.ID {
			t.Errorf("%s: last run = %s, want %s", id, sum.LastRun.ID, wantLast.ID)
		}

		wantStats, err := store.GetRunStats7d(ctx, id)
		if err != nil {
			t.Fatalf("GetRunStats7d: %v", err)
		}
		if sum.Stats7d == nil {
			t.Fatalf("%s: stats nil", id)
		}
		if sum.Stats7d.Total != wantStats.Total ||
			sum.Stats7d.Succeeded != wantStats.Succeeded ||
			sum.Stats7d.Failed != wantStats.Failed {
			t.Errorf("%s: stats = %+v, want %+v", id, sum.Stats7d, wantStats)
		}

		wantDurs, err := store.GetRecentDurations(ctx, id, 20)
		if err != nil {
			t.Fatalf("GetRecentDurations: %v", err)
		}
		if len(sum.Durations) != len(wantDurs) {
			t.Fatalf("%s: %d durations, want %d", id, len(sum.Durations), len(wantDurs))
		}
		// Order matters: the sparkline reverses this slice, so newest-first must
		// be preserved.
		for i := range wantDurs {
			if sum.Durations[i] != wantDurs[i] {
				t.Errorf("%s: durations[%d] = %d, want %d", id, i, sum.Durations[i], wantDurs[i])
			}
		}
	}
}

// TestGetCronJobSummaries_NoRuns verifies a CronJob that has never run is absent
// from the map rather than present with zero values, which is the contract
// callers rely on to render "—".
func TestGetCronJobSummaries_NoRuns(t *testing.T) {
	store := newTestStore(t)
	seed(t, store, "c1", "c1/default/test-cron")

	summaries, err := store.GetCronJobSummaries(context.Background(), "c1", 20)
	if err != nil {
		t.Fatalf("GetCronJobSummaries: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected no summaries, got %d", len(summaries))
	}
}

// TestGetCronJobSummaries_ScopedToCluster verifies the aggregate does not leak
// runs from other clusters into a cluster's rows.
func TestGetCronJobSummaries_ScopedToCluster(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	seed(t, store, "c2", "c2/default/test-cron")

	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)
	addFinishedRun(t, store, "r2", "c2/default/test-cron", "failed", time.Hour, 2000)

	summaries, err := store.GetCronJobSummaries(ctx, "c1", 20)
	if err != nil {
		t.Fatalf("GetCronJobSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if _, ok := summaries["c1/default/test-cron"]; !ok {
		t.Error("expected only the c1 CronJob in the c1 summaries")
	}
}

// TestGetCronJobSummaries_DurationLimit verifies the per-CronJob cap is applied
// per partition, not globally.
func TestGetCronJobSummaries_DurationLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	seedCronJob(t, store, "c1", "c1/default/other-cron", "other-cron")

	for i := range 5 {
		addFinishedRun(t, store, "a"+string(rune('0'+i)), "c1/default/test-cron",
			"succeeded", time.Duration(5-i)*time.Hour, int64(1000*(i+1)))
		addFinishedRun(t, store, "b"+string(rune('0'+i)), "c1/default/other-cron",
			"succeeded", time.Duration(5-i)*time.Hour, int64(1000*(i+1)))
	}

	summaries, err := store.GetCronJobSummaries(ctx, "c1", 3)
	if err != nil {
		t.Fatalf("GetCronJobSummaries: %v", err)
	}
	for id, sum := range summaries {
		if len(sum.Durations) != 3 {
			t.Errorf("%s: %d durations, want 3 (limit applied per CronJob)", id, len(sum.Durations))
		}
	}
}

func TestCountRunningRuns(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	for _, id := range []string{"run-a", "run-b"} {
		if err := store.UpsertJobRun(ctx, storage.JobRun{
			ID: id, CronJobID: "c1/default/test-cron",
			PodName: id, Trigger: "scheduled", Status: "running", StartedAt: time.Now(),
		}); err != nil {
			t.Fatalf("UpsertJobRun: %v", err)
		}
	}
	addFinishedRun(t, store, "run-done", "c1/default/test-cron", "succeeded", time.Hour, 1000)

	counts, err := store.CountRunningRuns(ctx)
	if err != nil {
		t.Fatalf("CountRunningRuns: %v", err)
	}
	if counts["c1/default/test-cron"] != 2 {
		t.Errorf("running count = %d, want 2 (finished runs excluded)", counts["c1/default/test-cron"])
	}
}

// ── Soft delete (BUG-20) ─────────────────────────────────────────────────────

func TestMarkCronJobDeleted_HiddenFromListButKeepsHistory(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)

	if err := store.MarkCronJobDeleted(ctx, "c1/default/test-cron"); err != nil {
		t.Fatalf("MarkCronJobDeleted: %v", err)
	}

	live, err := store.ListCronJobs(ctx, "c1")
	if err != nil {
		t.Fatalf("ListCronJobs: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("expected deleted cronjob to be hidden, got %d rows", len(live))
	}

	// Direct lookup still resolves so existing run-history links keep working.
	cj, err := store.GetCronJobByName(ctx, "c1", "default", "test-cron")
	if err != nil {
		t.Fatalf("GetCronJobByName: %v", err)
	}
	if cj == nil || cj.DeletedAt == nil {
		t.Fatalf("expected soft-deleted row to remain readable, got %v", cj)
	}
	if run, _ := store.GetJobRun(ctx, "r1"); run == nil {
		t.Error("run history must survive")
	}
}

// TestMarkCronJobDeleted_Idempotent verifies re-marking does not slide the
// deletion timestamp, which would postpone the retention purge indefinitely.
func TestMarkCronJobDeleted_Idempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	if err := store.MarkCronJobDeleted(ctx, "c1/default/test-cron"); err != nil {
		t.Fatalf("MarkCronJobDeleted: %v", err)
	}
	first, _ := store.GetCronJobByName(ctx, "c1", "default", "test-cron")
	if first == nil || first.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set")
	}

	time.Sleep(10 * time.Millisecond)
	if err := store.MarkCronJobDeleted(ctx, "c1/default/test-cron"); err != nil {
		t.Fatalf("MarkCronJobDeleted (second): %v", err)
	}
	second, _ := store.GetCronJobByName(ctx, "c1", "default", "test-cron")
	if !second.DeletedAt.Equal(*first.DeletedAt) {
		t.Errorf("DeletedAt moved from %v to %v", first.DeletedAt, second.DeletedAt)
	}
}

func TestUpsertCronJob_RevivesDeletedRow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	if err := store.MarkCronJobDeleted(ctx, "c1/default/test-cron"); err != nil {
		t.Fatalf("MarkCronJobDeleted: %v", err)
	}
	seed(t, store, "c1", "c1/default/test-cron") // re-upsert, as the informer would

	live, _ := store.ListCronJobs(ctx, "c1")
	if len(live) != 1 {
		t.Fatalf("expected recreated cronjob to be live again, got %d rows", len(live))
	}
	if live[0].DeletedAt != nil {
		t.Error("expected DeletedAt to be cleared on re-upsert")
	}
}

func TestMarkClustersDeletedExcept(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"c1", "c2", "c3"} {
		if err := store.UpsertCluster(ctx, storage.Cluster{ID: id, Name: id, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("UpsertCluster: %v", err)
		}
	}

	if err := store.MarkClustersDeletedExcept(ctx, []string{"c1", "c3"}); err != nil {
		t.Fatalf("MarkClustersDeletedExcept: %v", err)
	}

	live, err := store.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	got := map[string]bool{}
	for _, c := range live {
		got[c.ID] = true
	}
	if !got["c1"] || !got["c3"] {
		t.Errorf("expected c1 and c3 to stay live, got %v", got)
	}
	if got["c2"] {
		t.Error("expected c2 to be soft-deleted")
	}
}

// TestMarkClustersDeletedExcept_EmptyIsNoOp guards the failure mode where a
// config directory that momentarily reads as empty would wipe every cluster
// from the UI.
func TestMarkClustersDeletedExcept_EmptyIsNoOp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "c1", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertCluster: %v", err)
	}

	if err := store.MarkClustersDeletedExcept(ctx, nil); err != nil {
		t.Fatalf("MarkClustersDeletedExcept: %v", err)
	}

	live, _ := store.ListClusters(ctx)
	if len(live) != 1 {
		t.Errorf("expected cluster to survive an empty keep-list, got %d", len(live))
	}
}

func TestUpsertCluster_RevivesDeletedRow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "c1", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertCluster: %v", err)
	}
	if err := store.MarkClustersDeletedExcept(ctx, []string{"other"}); err != nil {
		t.Fatalf("MarkClustersDeletedExcept: %v", err)
	}
	if live, _ := store.ListClusters(ctx); len(live) != 0 {
		t.Fatalf("expected c1 to be soft-deleted, got %d", len(live))
	}

	if err := store.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "c1", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertCluster (revive): %v", err)
	}
	if live, _ := store.ListClusters(ctx); len(live) != 1 {
		t.Error("expected c1 to be live again after re-registration")
	}
}

// TestPurgeDeletedCronJobs verifies the purge waits for run history to age out
// before dropping the row — the cascade would otherwise take the history with it.
func TestPurgeDeletedCronJobs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")
	addFinishedRun(t, store, "r1", "c1/default/test-cron", "succeeded", time.Hour, 1000)

	if err := store.MarkCronJobDeleted(ctx, "c1/default/test-cron"); err != nil {
		t.Fatalf("MarkCronJobDeleted: %v", err)
	}

	// Runs still present → the row must stay.
	if err := store.PurgeDeletedCronJobs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PurgeDeletedCronJobs: %v", err)
	}
	if cj, _ := store.GetCronJobByName(ctx, "c1", "default", "test-cron"); cj == nil {
		t.Fatal("cronjob must not be purged while it still has runs")
	}

	// Once the runs are gone, the row can go too.
	if err := store.DeleteOldData(ctx, time.Now()); err != nil {
		t.Fatalf("DeleteOldData: %v", err)
	}
	if err := store.PurgeDeletedCronJobs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PurgeDeletedCronJobs: %v", err)
	}
	if cj, _ := store.GetCronJobByName(ctx, "c1", "default", "test-cron"); cj != nil {
		t.Error("expected cronjob row to be purged once its runs aged out")
	}
}

// TestPurgeDeletedCronJobs_LeavesLiveRows guards against the purge touching
// CronJobs that are still present in the cluster.
func TestPurgeDeletedCronJobs_LeavesLiveRows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	if err := store.PurgeDeletedCronJobs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PurgeDeletedCronJobs: %v", err)
	}
	if live, _ := store.ListCronJobs(ctx, "c1"); len(live) != 1 {
		t.Error("purge must not touch live cronjobs")
	}
}

// TestUpsertCronJob_PersistsTimeZone verifies the zone round-trips through the
// store (DOM-1).
func TestUpsertCronJob_PersistsTimeZone(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "c1", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertCluster: %v", err)
	}

	tz := "Europe/Paris"
	if err := store.UpsertCronJob(ctx, storage.CronJob{
		ID: "c1/default/tz-cron", ClusterID: "c1", Namespace: "default", Name: "tz-cron",
		Schedule: "0 4 * * *", TimeZone: &tz, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCronJob: %v", err)
	}

	live, _ := store.ListCronJobs(ctx, "c1")
	if len(live) != 1 || live[0].TZ() != tz {
		t.Fatalf("expected time zone %q to round-trip, got %+v", tz, live)
	}

	// Clearing spec.timeZone on the CronJob must clear the stored zone too.
	if err := store.UpsertCronJob(ctx, storage.CronJob{
		ID: "c1/default/tz-cron", ClusterID: "c1", Namespace: "default", Name: "tz-cron",
		Schedule: "0 4 * * *", TimeZone: nil, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCronJob (clear tz): %v", err)
	}
	live, _ = store.ListCronJobs(ctx, "c1")
	if live[0].TimeZone != nil {
		t.Errorf("expected time zone to be cleared, got %q", live[0].TZ())
	}
}
