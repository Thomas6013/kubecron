package storage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/migrations"
)

// TestOpen_UpgradesExistingDatabase applies only the migrations that shipped in
// 0.1.0, then opens the database with the current code. Every other storage test
// starts from an empty file, which exercises the fresh-install path only — this
// one covers the upgrade path an existing deployment actually takes, where the
// new columns are added by ALTER TABLE to tables that already hold rows.
func TestOpen_UpgradesExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubecron.db")

	// ── Build a 0.1.0-era database ────────────────────────────────────────────
	shipped := []string{
		"000001_init.up.sql",
		"000002_resource_samples.up.sql",
		"000003_duration_ms.up.sql",
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		name       TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create _migrations: %v", err)
	}
	for _, name := range shipped {
		data, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatalf("exec %s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO _migrations(name) VALUES (?)`, name); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}

	// Existing data, so the new columns are added to populated tables.
	if _, err := db.Exec(
		`INSERT INTO clusters(id, name, metrics_enabled, created_at) VALUES ('c1','c1',0,datetime('now'))`,
	); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO cronjobs(id, cluster_id, namespace, name, schedule, updated_at)
		VALUES ('c1/default/legacy','c1','default','legacy','0 4 * * *', datetime('now'))`); err != nil {
		t.Fatalf("seed cronjob: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// ── Upgrade ───────────────────────────────────────────────────────────────
	store, err := storage.Open(path)
	if err != nil {
		t.Fatalf("storage.Open on an existing database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	// Pre-existing rows must survive and be readable through the new schema.
	cronjobs, err := store.ListCronJobs(ctx, "c1")
	if err != nil {
		t.Fatalf("ListCronJobs after upgrade: %v", err)
	}
	if len(cronjobs) != 1 || cronjobs[0].Name != "legacy" {
		t.Fatalf("expected the pre-existing cronjob to survive, got %+v", cronjobs)
	}
	if cronjobs[0].TimeZone != nil {
		t.Errorf("expected no zone on a migrated row, got %q", cronjobs[0].TZ())
	}
	if cronjobs[0].DeletedAt != nil {
		t.Error("expected a migrated row to be live")
	}
	if clusters, err := store.ListClusters(ctx); err != nil || len(clusters) != 1 {
		t.Fatalf("expected the pre-existing cluster to survive, got %v (err=%v)", clusters, err)
	}

	// The columns added by the upgrade must be writable, not just readable.
	tz := "Europe/Paris"
	if err := store.UpsertCronJob(ctx, storage.CronJob{
		ID: "c1/default/legacy", ClusterID: "c1", Namespace: "default", Name: "legacy",
		Schedule: "0 4 * * *", TimeZone: &tz, UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertCronJob after upgrade: %v", err)
	}
	if err := store.MarkCronJobDeleted(ctx, "c1/default/legacy"); err != nil {
		t.Fatalf("MarkCronJobDeleted after upgrade: %v", err)
	}
	if live, _ := store.ListCronJobs(ctx, "c1"); len(live) != 0 {
		t.Error("expected soft delete to work on a migrated row")
	}

	// Re-opening must be a no-op rather than re-applying the ALTERs.
	reopened, err := storage.Open(path)
	if err != nil {
		t.Fatalf("re-opening an already-migrated database: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
