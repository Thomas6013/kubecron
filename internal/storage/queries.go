package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

func (s *SQLiteStore) UpsertCluster(ctx context.Context, c Cluster) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO clusters(id, name, metrics_enabled, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name`,
		c.ID, c.Name, boolToInt(c.MetricsEnabled), c.CreatedAt,
	)
	return wrapErr("UpsertCluster", err)
}

func (s *SQLiteStore) ListClusters(ctx context.Context) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, metrics_enabled, created_at FROM clusters ORDER BY name`)
	if err != nil {
		return nil, wrapErr("ListClusters", err)
	}
	defer rows.Close()

	var out []Cluster
	for rows.Next() {
		var c Cluster
		var metricsInt int
		if err := rows.Scan(&c.ID, &c.Name, &metricsInt, &c.CreatedAt); err != nil {
			return nil, wrapErr("ListClusters scan", err)
		}
		c.MetricsEnabled = metricsInt != 0
		out = append(out, c)
	}
	return out, wrapErr("ListClusters rows", rows.Err())
}

func (s *SQLiteStore) SetClusterMetricsEnabled(ctx context.Context, clusterID string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE clusters SET metrics_enabled = ? WHERE id = ?`,
		boolToInt(enabled), clusterID,
	)
	return wrapErr("SetClusterMetricsEnabled", err)
}

// ---------------------------------------------------------------------------
// CronJob
// ---------------------------------------------------------------------------

func (s *SQLiteStore) UpsertCronJob(ctx context.Context, cj CronJob) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cronjobs(
			id, cluster_id, namespace, name, schedule, suspended,
			cpu_request, cpu_limit, memory_request, memory_limit,
			last_successful_time, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			schedule             = excluded.schedule,
			suspended            = excluded.suspended,
			cpu_request          = excluded.cpu_request,
			cpu_limit            = excluded.cpu_limit,
			memory_request       = excluded.memory_request,
			memory_limit         = excluded.memory_limit,
			last_successful_time = excluded.last_successful_time,
			updated_at           = excluded.updated_at`,
		cj.ID, cj.ClusterID, cj.Namespace, cj.Name, cj.Schedule, boolToInt(cj.Suspended),
		cj.CPURequest, cj.CPULimit, cj.MemoryRequest, cj.MemoryLimit,
		cj.LastSuccessfulTime, cj.UpdatedAt,
	)
	return wrapErr("UpsertCronJob", err)
}

func (s *SQLiteStore) ListCronJobs(ctx context.Context, clusterID string) ([]CronJob, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, cluster_id, namespace, name, schedule, suspended,
		       cpu_request, cpu_limit, memory_request, memory_limit,
		       last_successful_time, updated_at
		FROM cronjobs WHERE cluster_id = ? ORDER BY namespace, name`,
		clusterID,
	)
	if err != nil {
		return nil, wrapErr("ListCronJobs", err)
	}
	defer rows.Close()

	var out []CronJob
	for rows.Next() {
		cj, err := scanCronJob(rows)
		if err != nil {
			return nil, wrapErr("ListCronJobs scan", err)
		}
		out = append(out, *cj)
	}
	return out, wrapErr("ListCronJobs rows", rows.Err())
}

func (s *SQLiteStore) GetCronJobByName(ctx context.Context, clusterID, namespace, name string) (*CronJob, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, cluster_id, namespace, name, schedule, suspended,
		       cpu_request, cpu_limit, memory_request, memory_limit,
		       last_successful_time, updated_at
		FROM cronjobs WHERE cluster_id = ? AND namespace = ? AND name = ?`,
		clusterID, namespace, name,
	)
	cj, err := scanCronJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return cj, wrapErr("GetCronJobByName", err)
}

// ---------------------------------------------------------------------------
// JobRun
// ---------------------------------------------------------------------------

// jobRunCols is the canonical SELECT column list for job_runs, shared by all
// queries that need to scan a full JobRun row.
const jobRunCols = `id, cronjob_id, pod_name, node_name, container_image,
	       trigger, started_at, finished_at, status, exit_code,
	       retry_count, log_size_bytes, duration_ms,
	       avg_cpu_millicores, max_cpu_millicores,
	       avg_memory_bytes, max_memory_bytes`

func (s *SQLiteStore) UpsertJobRun(ctx context.Context, r JobRun) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_runs(id, cronjob_id, pod_name, trigger, started_at, status)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		    cronjob_id = excluded.cronjob_id,
		    pod_name   = excluded.pod_name,
		    trigger    = excluded.trigger,
		    started_at = excluded.started_at,
		    status     = excluded.status`,
		r.ID, r.CronJobID, r.PodName, r.Trigger, r.StartedAt, r.Status,
	)
	return wrapErr("UpsertJobRun", err)
}

func (s *SQLiteStore) GetJobRun(ctx context.Context, id string) (*JobRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+jobRunCols+` FROM job_runs WHERE id = ?`, id)
	r, err := scanJobRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, wrapErr("GetJobRun", err)
}

func (s *SQLiteStore) ListJobRuns(ctx context.Context, cronjobID string) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+jobRunCols+` FROM job_runs WHERE cronjob_id = ? ORDER BY started_at DESC`,
		cronjobID,
	)
	if err != nil {
		return nil, wrapErr("ListJobRuns", err)
	}
	defer rows.Close()

	var out []JobRun
	for rows.Next() {
		r, err := scanJobRun(rows)
		if err != nil {
			return nil, wrapErr("ListJobRuns scan", err)
		}
		out = append(out, *r)
	}
	return out, wrapErr("ListJobRuns rows", rows.Err())
}

func (s *SQLiteStore) GetLastJobRun(ctx context.Context, cronjobID string) (*JobRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+jobRunCols+` FROM job_runs WHERE cronjob_id = ? ORDER BY started_at DESC LIMIT 1`,
		cronjobID,
	)
	r, err := scanJobRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, wrapErr("GetLastJobRun", err)
}

func (s *SQLiteStore) GetRunStats7d(ctx context.Context, cronjobID string) (*RunStats, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'failed'    THEN 1 ELSE 0 END),
			CAST(AVG(duration_ms) AS INTEGER),
			MAX(duration_ms),
			CAST(AVG(avg_cpu_millicores) AS INTEGER),
			CAST(AVG(avg_memory_bytes)   AS INTEGER)
		FROM job_runs
		WHERE cronjob_id = ?
		  AND started_at > datetime('now', '-7 days')`,
		cronjobID,
	)

	var stats RunStats
	var avgDur, maxDur, avgCPU, avgMem sql.NullInt64
	if err := row.Scan(
		&stats.Total, &stats.Succeeded, &stats.Failed,
		&avgDur, &maxDur, &avgCPU, &avgMem,
	); err != nil {
		return nil, wrapErr("GetRunStats7d", err)
	}
	if avgDur.Valid {
		stats.AvgDurationMs = &avgDur.Int64
	}
	if maxDur.Valid {
		stats.MaxDurationMs = &maxDur.Int64
	}
	if avgCPU.Valid {
		stats.AvgCPUMillicores = &avgCPU.Int64
	}
	if avgMem.Valid {
		stats.AvgMemoryBytes = &avgMem.Int64
	}
	return &stats, nil
}

func (s *SQLiteStore) GetRecentDurations(ctx context.Context, cronjobID string, limit int) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT duration_ms FROM job_runs
		WHERE cronjob_id = ? AND duration_ms IS NOT NULL AND status IN ('succeeded','failed')
		ORDER BY started_at DESC LIMIT ?`,
		cronjobID, limit,
	)
	if err != nil {
		return nil, wrapErr("GetRecentDurations", err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var d int64
		if err := rows.Scan(&d); err != nil {
			return nil, wrapErr("GetRecentDurations scan", err)
		}
		out = append(out, d)
	}
	return out, wrapErr("GetRecentDurations rows", rows.Err())
}

func (s *SQLiteStore) GetDailyRunStats(ctx context.Context, cronjobID string, days int) ([]DailyRunStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(started_at, 1, 10), COUNT(*),
		       SUM(CASE WHEN status='succeeded' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN status='running'   THEN 1 ELSE 0 END)
		FROM job_runs
		WHERE cronjob_id = ? AND started_at > datetime('now', ?)
		GROUP BY substr(started_at, 1, 10)
		ORDER BY substr(started_at, 1, 10)`,
		cronjobID, fmt.Sprintf("-%d days", days),
	)
	if err != nil {
		return nil, wrapErr("GetDailyRunStats", err)
	}
	defer rows.Close()

	var out []DailyRunStat
	for rows.Next() {
		var d DailyRunStat
		if err := rows.Scan(&d.Day, &d.Total, &d.Succeeded, &d.Running); err != nil {
			return nil, wrapErr("GetDailyRunStats scan", err)
		}
		out = append(out, d)
	}
	return out, wrapErr("GetDailyRunStats rows", rows.Err())
}

func (s *SQLiteStore) ListJobRunsPaged(ctx context.Context, cronjobID, beforeCursor string, limit int) ([]JobRun, error) {
	var (
		r   *sql.Rows
		err error
	)
	if beforeCursor == "" {
		r, err = s.db.QueryContext(ctx,
			`SELECT `+jobRunCols+` FROM job_runs WHERE cronjob_id = ? ORDER BY started_at DESC LIMIT ?`,
			cronjobID, limit,
		)
	} else {
		r, err = s.db.QueryContext(ctx,
			`SELECT `+jobRunCols+` FROM job_runs WHERE cronjob_id = ? AND datetime(started_at) < datetime(?) ORDER BY started_at DESC LIMIT ?`,
			cronjobID, beforeCursor, limit,
		)
	}
	if err != nil {
		return nil, wrapErr("ListJobRunsPaged", err)
	}
	defer r.Close()
	var out []JobRun
	for r.Next() {
		run, err := scanJobRun(r)
		if err != nil {
			return nil, wrapErr("ListJobRunsPaged scan", err)
		}
		out = append(out, *run)
	}
	return out, wrapErr("ListJobRunsPaged rows", r.Err())
}

func (s *SQLiteStore) ListJobRunsByDay(ctx context.Context, cronjobID, day string) ([]JobRun, error) {
	r, err := s.db.QueryContext(ctx,
		`SELECT `+jobRunCols+` FROM job_runs WHERE cronjob_id = ? AND substr(started_at, 1, 10) = ? ORDER BY started_at DESC`,
		cronjobID, day,
	)
	if err != nil {
		return nil, wrapErr("ListJobRunsByDay", err)
	}
	defer r.Close()
	var out []JobRun
	for r.Next() {
		run, err := scanJobRun(r)
		if err != nil {
			return nil, wrapErr("ListJobRunsByDay scan", err)
		}
		out = append(out, *run)
	}
	return out, wrapErr("ListJobRunsByDay rows", r.Err())
}

func (s *SQLiteStore) UpdateJobRunStatus(ctx context.Context, id, status string, finishedAt *time.Time, exitCode, retryCount int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_runs
		SET status = ?, finished_at = ?, exit_code = ?, retry_count = ?
		WHERE id = ?`,
		status, finishedAt, exitCode, retryCount, id,
	)
	return wrapErr("UpdateJobRunStatus", err)
}

func (s *SQLiteStore) UpdateJobRunNode(ctx context.Context, id, nodeName, containerImage string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_runs SET node_name = ?, container_image = ? WHERE id = ?`,
		nodeName, containerImage, id,
	)
	return wrapErr("UpdateJobRunNode", err)
}

func (s *SQLiteStore) GetRunningRuns(ctx context.Context) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+jobRunCols+` FROM job_runs WHERE status = 'running'`,
	)
	if err != nil {
		return nil, wrapErr("GetRunningRuns", err)
	}
	defer rows.Close()

	var out []JobRun
	for rows.Next() {
		r, err := scanJobRun(rows)
		if err != nil {
			return nil, wrapErr("GetRunningRuns scan", err)
		}
		out = append(out, *r)
	}
	return out, wrapErr("GetRunningRuns rows", rows.Err())
}

func (s *SQLiteStore) MarkRunFailed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_runs
		SET status = 'failed', exit_code = -1, finished_at = datetime('now')
		WHERE id = ?`, id,
	)
	return wrapErr("MarkRunFailed", err)
}

func (s *SQLiteStore) AddLogSize(ctx context.Context, id string, bytes int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_runs SET log_size_bytes = log_size_bytes + ? WHERE id = ?`,
		bytes, id,
	)
	return wrapErr("AddLogSize", err)
}

func (s *SQLiteStore) FinalizeResourceUsage(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_runs SET
			avg_cpu_millicores = (SELECT CAST(AVG(cpu_millicores) AS INTEGER) FROM resource_samples WHERE run_id = ?),
			max_cpu_millicores = (SELECT MAX(cpu_millicores)                  FROM resource_samples WHERE run_id = ?),
			avg_memory_bytes   = (SELECT CAST(AVG(memory_bytes) AS INTEGER)   FROM resource_samples WHERE run_id = ?),
			max_memory_bytes   = (SELECT MAX(memory_bytes)                    FROM resource_samples WHERE run_id = ?)
		WHERE id = ?`,
		runID, runID, runID, runID, runID,
	)
	return wrapErr("FinalizeResourceUsage", err)
}

// ---------------------------------------------------------------------------
// Log lines
// ---------------------------------------------------------------------------

func (s *SQLiteStore) BatchInsertLogLines(ctx context.Context, runID string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapErr("BatchInsertLogLines begin", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO log_lines(run_id, line) VALUES (?,?)`)
	if err != nil {
		return wrapErr("BatchInsertLogLines prepare", err)
	}
	defer stmt.Close()

	for _, line := range lines {
		if _, err = stmt.ExecContext(ctx, runID, line); err != nil {
			return wrapErr("BatchInsertLogLines exec", err)
		}
	}

	return wrapErr("BatchInsertLogLines commit", tx.Commit())
}

func (s *SQLiteStore) GetLogLines(ctx context.Context, runID string) ([]LogLine, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, ts, line FROM log_lines WHERE run_id = ? ORDER BY id`,
		runID,
	)
	if err != nil {
		return nil, wrapErr("GetLogLines", err)
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.RunID, &l.Ts, &l.Line); err != nil {
			return nil, wrapErr("GetLogLines scan", err)
		}
		out = append(out, l)
	}
	return out, wrapErr("GetLogLines rows", rows.Err())
}


func (s *SQLiteStore) GetLogLinesTail(ctx context.Context, runID string, limit int) ([]LogLine, error) {
	if limit <= 0 {
		return s.GetLogLines(ctx, runID)
	}
	// Fetch the last `limit` lines via a DESC subquery, then re-order ASC so the
	// caller always receives lines in chronological order.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, ts, line FROM (
			SELECT id, run_id, ts, line FROM log_lines WHERE run_id = ? ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`,
		runID, limit,
	)
	if err != nil {
		return nil, wrapErr("GetLogLinesTail", err)
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.RunID, &l.Ts, &l.Line); err != nil {
			return nil, wrapErr("GetLogLinesTail scan", err)
		}
		out = append(out, l)
	}
	return out, wrapErr("GetLogLinesTail rows", rows.Err())
}

// ---------------------------------------------------------------------------
// Resource samples
// ---------------------------------------------------------------------------

func (s *SQLiteStore) InsertResourceSample(ctx context.Context, runID string, cpuMillicores, memoryBytes int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO resource_samples(run_id, sampled_at, cpu_millicores, memory_bytes)
		VALUES (?, datetime('now'), ?, ?)`,
		runID, cpuMillicores, memoryBytes,
	)
	return wrapErr("InsertResourceSample", err)
}

func (s *SQLiteStore) GetResourceSamples(ctx context.Context, runID string) ([]ResourceSample, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, sampled_at, cpu_millicores, memory_bytes
		FROM resource_samples WHERE run_id = ? ORDER BY id`,
		runID,
	)
	if err != nil {
		return nil, wrapErr("GetResourceSamples", err)
	}
	defer rows.Close()

	var out []ResourceSample
	for rows.Next() {
		var rs ResourceSample
		if err := rows.Scan(&rs.ID, &rs.RunID, &rs.SampledAt, &rs.CPUMillicores, &rs.MemoryBytes); err != nil {
			return nil, wrapErr("GetResourceSamples scan", err)
		}
		out = append(out, rs)
	}
	return out, wrapErr("GetResourceSamples rows", rows.Err())
}

// ---------------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------------

func (s *SQLiteStore) DeleteOldData(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_runs WHERE started_at < ?`, before)
	return wrapErr("DeleteOldData", err)
}

func (s *SQLiteStore) DeleteOldLogLines(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM log_lines WHERE run_id IN (SELECT id FROM job_runs WHERE started_at < ?)`,
		before,
	)
	return wrapErr("DeleteOldLogLines", err)
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

// rowScanner abstracts *sql.Row and *sql.Rows so we can share scan logic.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCronJob(s rowScanner) (*CronJob, error) {
	var cj CronJob
	var suspended int
	var lastSuccess sql.NullTime
	err := s.Scan(
		&cj.ID, &cj.ClusterID, &cj.Namespace, &cj.Name, &cj.Schedule, &suspended,
		&cj.CPURequest, &cj.CPULimit, &cj.MemoryRequest, &cj.MemoryLimit,
		&lastSuccess, &cj.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	cj.Suspended = suspended != 0
	if lastSuccess.Valid {
		cj.LastSuccessfulTime = &lastSuccess.Time
	}
	return &cj, nil
}

func scanJobRun(r rowScanner) (*JobRun, error) {
	var jr JobRun
	var nodeName, containerImage sql.NullString
	var finishedAt sql.NullTime
	var exitCode sql.NullInt64
	var durationMs sql.NullInt64
	var avgCPU, maxCPU, avgMem, maxMem sql.NullInt64

	err := r.Scan(
		&jr.ID, &jr.CronJobID, &jr.PodName,
		&nodeName, &containerImage,
		&jr.Trigger, &jr.StartedAt, &finishedAt,
		&jr.Status, &exitCode,
		&jr.RetryCount, &jr.LogSizeBytes, &durationMs,
		&avgCPU, &maxCPU, &avgMem, &maxMem,
	)
	if err != nil {
		return nil, err
	}
	if nodeName.Valid {
		jr.NodeName = &nodeName.String
	}
	if containerImage.Valid {
		jr.ContainerImage = &containerImage.String
	}
	if finishedAt.Valid {
		jr.FinishedAt = &finishedAt.Time
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		jr.ExitCode = &v
	}
	if durationMs.Valid {
		jr.DurationMs = &durationMs.Int64
	} else if jr.FinishedAt != nil {
		// julianday() can't parse Go's RFC3339Nano format; compute in Go.
		ms := jr.FinishedAt.Sub(jr.StartedAt).Milliseconds()
		if ms < 0 {
			ms = 0
		}
		jr.DurationMs = &ms
	}
	if avgCPU.Valid {
		jr.AvgCPUMillicores = &avgCPU.Int64
	}
	if maxCPU.Valid {
		jr.MaxCPUMillicores = &maxCPU.Int64
	}
	if avgMem.Valid {
		jr.AvgMemoryBytes = &avgMem.Int64
	}
	if maxMem.Valid {
		jr.MaxMemoryBytes = &maxMem.Int64
	}
	return &jr, nil
}

// ---------------------------------------------------------------------------
// Small utilities
// ---------------------------------------------------------------------------

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("storage.%s: %w", op, err)
}
