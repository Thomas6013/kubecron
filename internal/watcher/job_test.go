package watcher

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubecron/kubecron/internal/storage"
)

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	ctx := context.Background()
	if err := s.UpsertCluster(ctx, storage.Cluster{ID: "c1", Name: "c1", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("UpsertCluster: %v", err)
	}
	if err := s.UpsertCronJob(ctx, storage.CronJob{
		ID:        "c1/default/my-cron",
		ClusterID: "c1",
		Namespace: "default",
		Name:      "my-cron",
		Schedule:  "* * * * *",
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCronJob: %v", err)
	}
	return s
}

func makeCronJobOwner(cronName string) metav1.OwnerReference {
	return metav1.OwnerReference{Kind: "CronJob", Name: cronName, APIVersion: "batch/v1"}
}

// TestJobHandler_OnAdd_NewJob verifies that a live Job creates a running run record
// and populates the RunIndex.
func TestJobHandler_OnAdd_NewJob(t *testing.T) {
	store := newTestStore(t)
	idx := NewRunIndex()
	h := NewJobHandler("c1", store, fake.NewSimpleClientset(), nil, idx)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-cron-abc123",
			Namespace:       "default",
			UID:             types.UID("run-uid-1"),
			OwnerReferences: []metav1.OwnerReference{makeCronJobOwner("my-cron")},
		},
	}
	h.OnAdd(job, false)

	run, err := store.GetJobRun(context.Background(), "run-uid-1")
	if err != nil {
		t.Fatalf("GetJobRun: %v", err)
	}
	if run == nil {
		t.Fatal("expected run record to exist")
	}
	if run.Status != "running" {
		t.Errorf("expected status=running, got %q", run.Status)
	}
	if idx.Get("my-cron-abc123") != "run-uid-1" {
		t.Errorf("expected RunIndex entry, got %q", idx.Get("my-cron-abc123"))
	}
}

// TestJobHandler_OnAdd_InitialListDoneJob verifies that a completed job in the
// initial list is backfilled with its real status (not "running").
func TestJobHandler_OnAdd_InitialListDoneJob(t *testing.T) {
	store := newTestStore(t)
	idx := NewRunIndex()
	h := NewJobHandler("c1", store, fake.NewSimpleClientset(), nil, idx)

	done := metav1.Now()
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-cron-done",
			Namespace:       "default",
			UID:             types.UID("run-uid-2"),
			OwnerReferences: []metav1.OwnerReference{makeCronJobOwner("my-cron")},
		},
		Status: batchv1.JobStatus{
			CompletionTime: &done,
			Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	h.OnAdd(job, true)

	run, err := store.GetJobRun(context.Background(), "run-uid-2")
	if err != nil {
		t.Fatalf("GetJobRun: %v", err)
	}
	if run == nil {
		t.Fatal("expected backfilled run record")
	}
	if run.Status != "succeeded" {
		t.Errorf("expected status=succeeded, got %q", run.Status)
	}
	// Backfilled runs must NOT enter the RunIndex (pod events for them are done).
	if idx.Get("my-cron-done") != "" {
		t.Error("backfilled run must not be in RunIndex")
	}
}

// TestJobHandler_OnDelete_MarksRunFailed verifies that deleting a Job whose run
// is still "running" transitions it to "failed" and cleans up the RunIndex.
func TestJobHandler_OnDelete_MarksRunFailed(t *testing.T) {
	store := newTestStore(t)
	idx := NewRunIndex()
	h := NewJobHandler("c1", store, fake.NewSimpleClientset(), nil, idx)

	// Pre-insert a running run.
	ctx := context.Background()
	if err := store.UpsertJobRun(ctx, storage.JobRun{
		ID:        "run-uid-3",
		CronJobID: "c1/default/my-cron",
		PodName:   "my-cron-xyz",
		Trigger:   "scheduled",
		Status:    "running",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertJobRun: %v", err)
	}
	idx.Set("my-cron-xyz", "run-uid-3")

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cron-xyz",
			Namespace: "default",
			UID:       types.UID("run-uid-3"),
		},
	}
	h.OnDelete(job)

	run, _ := store.GetJobRun(ctx, "run-uid-3")
	if run == nil || run.Status != "failed" {
		t.Errorf("expected status=failed after delete, got %v", run)
	}
	if idx.Get("my-cron-xyz") != "" {
		t.Error("expected RunIndex entry removed after OnDelete")
	}
}
