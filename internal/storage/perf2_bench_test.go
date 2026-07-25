package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// BenchmarkGetCronJobSummaries measures one cluster-page render's worth of
// reads. The HTMX poll re-runs this every 10 s per open tab, so it is the hot
// read path (PERF-2). Run it before and after touching those queries or the
// job_runs indexes:
//
//	go test ./internal/storage/ -run XXX -bench GetCronJobSummaries -benchtime 10x
func BenchmarkGetCronJobSummaries(b *testing.B) {
	const (
		nCronJobs = 200
		nRuns     = 30
	)
	store := newBenchStore(b)
	seedBenchCluster(b, store, nCronJobs, nRuns)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		if _, err := store.GetCronJobSummaries(ctx, "c1", 20); err != nil {
			b.Fatal(err)
		}
		if _, err := store.CountRunningRuns(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func newBenchStore(b *testing.B) storage.Store {
	b.Helper()
	s, err := storage.Open(":memory:")
	if err != nil {
		b.Fatalf("storage.Open: %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })
	return s
}

func seedBenchCluster(b *testing.B, store storage.Store, nCronJobs, nRuns int) {
	b.Helper()
	ctx := context.Background()
	if err := store.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "c1", CreatedAt: time.Now()}); err != nil {
		b.Fatal(err)
	}
	now := time.Now()
	for i := range nCronJobs {
		id := fmt.Sprintf("c1/default/cron-%04d", i)
		if err := store.UpsertCronJob(ctx, storage.CronJob{
			ID: id, ClusterID: "c1", Namespace: "default", Name: fmt.Sprintf("cron-%04d", i),
			Schedule: "* * * * *", UpdatedAt: now,
		}); err != nil {
			b.Fatal(err)
		}
		for j := range nRuns {
			runID := fmt.Sprintf("%s/run-%04d", id, j)
			started := now.Add(-time.Duration(j) * time.Hour)
			if err := store.UpsertJobRun(ctx, storage.JobRun{
				ID: runID, CronJobID: id, PodName: "p", Trigger: "scheduled",
				Status: "running", StartedAt: started,
			}); err != nil {
				b.Fatal(err)
			}
			finished := started.Add(time.Minute)
			if err := store.UpdateJobRunStatus(ctx, runID, "succeeded", &finished, 0, 0); err != nil {
				b.Fatal(err)
			}
		}
	}
}
