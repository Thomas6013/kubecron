package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open(:memory:): %v", err)
	}
	return s
}

// seed inserts the minimal cluster + cronjob needed for job_run tests.
func seed(t *testing.T, store storage.Store, clusterID, cronJobID string) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertCluster(ctx, storage.Cluster{
		ID:        clusterID,
		Name:      clusterID,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCluster: %v", err)
	}
	if err := store.UpsertCronJob(ctx, storage.CronJob{
		ID:        cronJobID,
		ClusterID: clusterID,
		Namespace: "default",
		Name:      "test-cron",
		Schedule:  "* * * * *",
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCronJob: %v", err)
	}
}

// ── Cluster ───────────────────────────────────────────────────────────────────

func TestUpsertAndListClusters(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "Cluster One", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertCluster: %v", err)
	}

	clusters, err := store.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 1 || clusters[0].ID != "c1" {
		t.Errorf("ListClusters: got %+v", clusters)
	}
}

// ── JobRun — SEC-3 regression ─────────────────────────────────────────────────

// TestUpsertJobRun_DoesNotOverwriteExistingFields verifies that calling
// UpsertJobRun on a run that already has finished_at, exit_code, etc. does
// NOT reset those columns to NULL. This is the regression test for SEC-3
// (INSERT OR REPLACE → ON CONFLICT DO UPDATE).
func TestUpsertJobRun_DoesNotOverwriteExistingFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	runID := "run-abc123"
	run := storage.JobRun{
		ID:        runID,
		CronJobID: "c1/default/test-cron",
		PodName:   "job-xyz",
		Trigger:   "scheduled",
		Status:    "running",
		StartedAt: time.Now().Add(-30 * time.Second),
	}
	if err := store.UpsertJobRun(ctx, run); err != nil {
		t.Fatalf("UpsertJobRun (insert): %v", err)
	}

	// Simulate pod completion.
	finished := time.Now()
	exitCode := 0
	if err := store.UpdateJobRunStatus(ctx, runID, "succeeded", &finished, exitCode, 0); err != nil {
		t.Fatalf("UpdateJobRunStatus: %v", err)
	}

	// Informer re-lists and calls UpsertJobRun again with the original data.
	if err := store.UpsertJobRun(ctx, run); err != nil {
		t.Fatalf("UpsertJobRun (re-upsert): %v", err)
	}

	got, err := store.GetJobRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetJobRun: %v", err)
	}
	if got == nil {
		t.Fatal("GetJobRun: run not found")
	}
	if got.ExitCode == nil {
		t.Error("ExitCode was wiped by re-upsert — ON CONFLICT regression")
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt was wiped by re-upsert — ON CONFLICT regression")
	}
}

// ── Log lines ─────────────────────────────────────────────────────────────────

func TestBatchInsertLogLines_AndGetLogLinesTail(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	run := storage.JobRun{
		ID:        "run-1",
		CronJobID: "c1/default/test-cron",
		PodName:   "job-1",
		Trigger:   "scheduled",
		Status:    "running",
		StartedAt: time.Now(),
	}
	if err := store.UpsertJobRun(ctx, run); err != nil {
		t.Fatalf("UpsertJobRun: %v", err)
	}

	lines := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	if err := store.BatchInsertLogLines(ctx, "run-1", lines); err != nil {
		t.Fatalf("BatchInsertLogLines: %v", err)
	}

	// Tail of 3 should return last 3 lines in ascending order.
	got, err := store.GetLogLinesTail(ctx, "run-1", 3)
	if err != nil {
		t.Fatalf("GetLogLinesTail: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("GetLogLinesTail: got %d lines, want 3", len(got))
	}
	wantLines := []string{"gamma", "delta", "epsilon"}
	for i, want := range wantLines {
		if got[i].Line != want {
			t.Errorf("line[%d]: got %q, want %q", i, got[i].Line, want)
		}
	}
}

func TestGetLogLinesTail_LimitGreaterThanTotal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seed(t, store, "c1", "c1/default/test-cron")

	store.UpsertJobRun(ctx, storage.JobRun{ //nolint:errcheck
		ID: "run-2", CronJobID: "c1/default/test-cron",
		PodName: "j", Trigger: "scheduled", Status: "running", StartedAt: time.Now(),
	})
	store.BatchInsertLogLines(ctx, "run-2", []string{"only line"}) //nolint:errcheck

	got, err := store.GetLogLinesTail(ctx, "run-2", 1000)
	if err != nil {
		t.Fatalf("GetLogLinesTail: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 line, got %d", len(got))
	}
}
