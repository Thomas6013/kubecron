package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// TestListJobRunsPagedWalksTheWholeHistory is a regression test for a paging
// query that returned nothing at all.
//
// The driver stores a time.Time as Go's time.Time.String() layout
// ("2006-01-02 15:04:05.999999999 -0700 MST"), which is not ISO 8601. The query
// used to compare datetime(started_at) < datetime(?), and SQLite's datetime()
// yields NULL on that layout — so the predicate was NULL for every row and every
// page after the first came back empty, in the UI's "Load more" as well as over
// the API. Nothing failed loudly: the history simply appeared to stop at 50 runs.
func TestListJobRunsPagedWalksTheWholeHistory(t *testing.T) {
	store := newTestStore(t)
	defer store.Close() //nolint:errcheck
	ctx := context.Background()

	const clusterID, cronJobID = "c1", "c1/default/test-cron"
	seed(t, store, clusterID, cronJobID)

	const total = 25
	base := time.Now().Add(-time.Duration(total) * time.Hour)
	for i := range total {
		if err := store.UpsertJobRun(ctx, storage.JobRun{
			ID:        "run-" + time.Duration(i).String(),
			CronJobID: cronJobID,
			PodName:   "pod",
			Trigger:   "scheduled",
			Status:    "succeeded",
			StartedAt: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}
	}

	// Walk every page and prove the whole history is reachable and each run is
	// seen exactly once.
	const pageSize = 10
	seen := map[string]bool{}
	var cursor time.Time
	var prev time.Time
	for page := 0; ; page++ {
		if page > total {
			t.Fatal("paging did not terminate")
		}
		runs, err := store.ListJobRunsPaged(ctx, cronJobID, cursor, pageSize)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(runs) == 0 {
			break
		}
		for _, r := range runs {
			if seen[r.ID] {
				t.Errorf("run %s returned on more than one page", r.ID)
			}
			seen[r.ID] = true
			if !prev.IsZero() && !r.StartedAt.Before(prev) {
				t.Errorf("run %s is not older than the previous row", r.ID)
			}
			prev = r.StartedAt
		}
		if len(runs) < pageSize {
			break
		}
		cursor = runs[len(runs)-1].StartedAt
	}

	if len(seen) != total {
		t.Errorf("paged through %d runs, want %d", len(seen), total)
	}
}

// TestListJobRunsPagedZeroCursorStartsAtNewest: the zero time means "first
// page", not "everything before the epoch".
func TestListJobRunsPagedZeroCursorStartsAtNewest(t *testing.T) {
	store := newTestStore(t)
	defer store.Close() //nolint:errcheck
	ctx := context.Background()

	const clusterID, cronJobID = "c1", "c1/default/test-cron"
	seed(t, store, clusterID, cronJobID)

	newest := time.Now().Add(-time.Minute)
	for i, at := range []time.Time{newest.Add(-2 * time.Hour), newest.Add(-time.Hour), newest} {
		if err := store.UpsertJobRun(ctx, storage.JobRun{
			ID:        "run-" + string(rune('a'+i)),
			CronJobID: cronJobID,
			PodName:   "pod",
			Trigger:   "scheduled",
			Status:    "succeeded",
			StartedAt: at,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	runs, err := store.ListJobRunsPaged(ctx, cronJobID, time.Time{}, 2)
	if err != nil {
		t.Fatalf("ListJobRunsPaged: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	if runs[0].ID != "run-c" {
		t.Errorf("first row = %s, want the newest run", runs[0].ID)
	}
}

// TestListJobRunsPagedSameSecond: two runs that started in the same second must
// both be reachable. A cursor truncated to whole seconds would skip the second
// one — parallel pods of one CronJob start within milliseconds of each other.
func TestListJobRunsPagedSameSecond(t *testing.T) {
	store := newTestStore(t)
	defer store.Close() //nolint:errcheck
	ctx := context.Background()

	const clusterID, cronJobID = "c1", "c1/default/test-cron"
	seed(t, store, clusterID, cronJobID)

	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i, offset := range []time.Duration{0, 200 * time.Millisecond, 400 * time.Millisecond} {
		if err := store.UpsertJobRun(ctx, storage.JobRun{
			ID:        "run-" + string(rune('a'+i)),
			CronJobID: cronJobID,
			PodName:   "pod",
			Trigger:   "scheduled",
			Status:    "succeeded",
			StartedAt: at.Add(offset),
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	first, err := store.ListJobRunsPaged(ctx, cronJobID, time.Time{}, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first page: %d runs, err %v", len(first), err)
	}
	rest, err := store.ListJobRunsPaged(ctx, cronJobID, first[0].StartedAt, 10)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(rest) != 2 {
		t.Fatalf("want the 2 remaining same-second runs, got %d", len(rest))
	}
}
