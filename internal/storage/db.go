package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/kubecron/kubecron/migrations"
	_ "modernc.org/sqlite"
)

// Store defines all persistence operations for KubeCron.
type Store interface {
	// Cluster operations
	UpsertCluster(ctx context.Context, c Cluster) error
	ListClusters(ctx context.Context) ([]Cluster, error)
	SetClusterMetricsEnabled(ctx context.Context, clusterID string, enabled bool) error

	// CronJob operations
	UpsertCronJob(ctx context.Context, cj CronJob) error
	ListCronJobs(ctx context.Context, clusterID string) ([]CronJob, error)
	GetCronJobByName(ctx context.Context, clusterID, namespace, name string) (*CronJob, error)

	// JobRun operations
	UpsertJobRun(ctx context.Context, r JobRun) error
	GetJobRun(ctx context.Context, id string) (*JobRun, error)
	ListJobRuns(ctx context.Context, cronjobID string) ([]JobRun, error)
	GetLastJobRun(ctx context.Context, cronjobID string) (*JobRun, error)
	GetRunStats7d(ctx context.Context, cronjobID string) (*RunStats, error)
	GetRecentDurations(ctx context.Context, cronjobID string, limit int) ([]int64, error)
	GetDailyRunStats(ctx context.Context, cronjobID string, days int) ([]DailyRunStat, error)
	UpdateJobRunStatus(ctx context.Context, id, status string, finishedAt *time.Time, exitCode, retryCount int) error
	UpdateJobRunNode(ctx context.Context, id, nodeName, containerImage string) error
	GetRunningRuns(ctx context.Context) ([]JobRun, error)
	MarkRunFailed(ctx context.Context, id string) error
	AddLogSize(ctx context.Context, id string, bytes int64) error
	FinalizeResourceUsage(ctx context.Context, runID string) error

	// Log line operations
	BatchInsertLogLines(ctx context.Context, runID string, lines []string) error
	GetLogLines(ctx context.Context, runID string) ([]LogLine, error)

	// Resource sample operations
	InsertResourceSample(ctx context.Context, runID string, cpuMillicores, memoryBytes int64) error
	GetResourceSamples(ctx context.Context, runID string) ([]ResourceSample, error)

	// Maintenance
	DeleteOldData(ctx context.Context, before time.Time) error
	Ping(ctx context.Context) error
}

// SQLiteStore is the SQLite-backed implementation of Store.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path, applies WAL mode and
// busy timeout, then runs all pending migrations.
func Open(path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open db: %w", err)
	}

	// SQLite is single-writer; a single connection avoids "database is locked"
	// errors under concurrent reads.
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent read performance and set a busy
	// timeout so writers wait instead of immediately returning SQLITE_BUSY.
	if _, err = db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("storage: set WAL mode: %w", err)
	}
	if _, err = db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("storage: set busy_timeout: %w", err)
	}
	// Enforce foreign key constraints (off by default in SQLite).
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return nil, fmt.Errorf("storage: enable foreign keys: %w", err)
	}

	if err = runMigrations(db); err != nil {
		return nil, fmt.Errorf("storage: migrations: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// runMigrations applies any *.up.sql files from the embedded migrations.FS
// that have not yet been recorded in the _migrations table.
func runMigrations(db *sql.DB) error {
	// Bootstrap the migrations tracking table.
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		name       TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("create _migrations table: %w", err)
	}

	// Collect all *.up.sql entries from the embedded FS.
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var upFiles []fs.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e)
		}
	}
	sort.Slice(upFiles, func(i, j int) bool {
		return upFiles[i].Name() < upFiles[j].Name()
	})

	for _, entry := range upFiles {
		name := entry.Name()

		// Check whether this migration was already applied.
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		data, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		sql := strings.TrimSpace(string(data))
		if sql == "" || sql == "-- placeholder" {
			// Record even placeholder migrations so they are not re-visited.
			if _, err := db.Exec(`INSERT INTO _migrations(name) VALUES (?)`, name); err != nil {
				return fmt.Errorf("record placeholder migration %s: %w", name, err)
			}
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for migration %s: %w", name, err)
		}

		if _, err = tx.Exec(sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec migration %s: %w", name, err)
		}

		if _, err = tx.Exec(`INSERT INTO _migrations(name) VALUES (?)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}
