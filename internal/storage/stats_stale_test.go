package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// TestGetRunStats7d_NoRunsInWindow guards the failure mode where a CronJob has
// run at some point but not within the 7-day window: the WHERE clause then
// matches no rows, and SUM() over zero rows is NULL while COUNT(*) is 0.
// Scanning that NULL into the non-nullable RunStats.Succeeded/Failed ints used
// to fail with "converting NULL to int is unsupported", which aborted the whole
// cluster page render (one stale CronJob took every other one down with it).
func TestGetRunStats7d_NoRunsInWindow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	// A single run, finished well outside the 7-day window.
	started := time.Now().Add(-30 * 24 * time.Hour)
	if err := store.UpsertJobRun(ctx, storage.JobRun{
		ID:        "run-old",
		CronJobID: "c1/default/test-cron",
		PodName:   "pod-old",
		Trigger:   "scheduled",
		Status:    "running",
		StartedAt: started,
	}); err != nil {
		t.Fatalf("UpsertJobRun: %v", err)
	}
	finished := started.Add(time.Minute)
	if err := store.UpdateJobRunStatus(ctx, "run-old", "succeeded", &finished, 0, 0); err != nil {
		t.Fatalf("UpdateJobRunStatus: %v", err)
	}

	stats, err := store.GetRunStats7d(ctx, "c1/default/test-cron")
	if err != nil {
		t.Fatalf("GetRunStats7d with no runs in window: %v", err)
	}
	if stats.Total != 0 || stats.Succeeded != 0 || stats.Failed != 0 {
		t.Errorf("expected a zeroed window, got total=%d succeeded=%d failed=%d",
			stats.Total, stats.Succeeded, stats.Failed)
	}
}

// TestGetCronJobSummaries_StaleCronJob is the end-to-end version of the same
// bug: GetCronJobSummaries skips CronJobs that never ran, so a CronJob whose
// only runs predate the window is exactly the case that reaches GetRunStats7d
// with an empty result set. This is the query path behind every cluster and
// namespace page.
func TestGetCronJobSummaries_StaleCronJob(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	started := time.Now().Add(-30 * 24 * time.Hour)
	if err := store.UpsertJobRun(ctx, storage.JobRun{
		ID:        "run-old",
		CronJobID: "c1/default/test-cron",
		PodName:   "pod-old",
		Trigger:   "scheduled",
		Status:    "running",
		StartedAt: started,
	}); err != nil {
		t.Fatalf("UpsertJobRun: %v", err)
	}
	finished := started.Add(time.Minute)
	if err := store.UpdateJobRunStatus(ctx, "run-old", "succeeded", &finished, 0, 0); err != nil {
		t.Fatalf("UpdateJobRunStatus: %v", err)
	}

	summaries, err := store.GetCronJobSummaries(ctx, "c1", 20)
	if err != nil {
		t.Fatalf("GetCronJobSummaries with a stale CronJob: %v", err)
	}
	sum := summaries["c1/default/test-cron"]
	if sum == nil {
		t.Fatal("expected a summary for a CronJob that has run before")
	}
	if sum.LastRun == nil || sum.LastRun.ID != "run-old" {
		t.Errorf("expected the out-of-window run to still be the last run, got %+v", sum.LastRun)
	}
	if sum.Stats7d.Total != 0 {
		t.Errorf("expected an empty 7-day window, got total=%d", sum.Stats7d.Total)
	}
}
