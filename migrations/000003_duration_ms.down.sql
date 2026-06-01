ALTER TABLE job_runs DROP COLUMN duration_ms;
ALTER TABLE job_runs ADD COLUMN duration_ms INTEGER GENERATED ALWAYS AS (
  CASE WHEN finished_at IS NOT NULL
  THEN CAST((julianday(finished_at) - julianday(started_at)) * 86400000 AS INTEGER)
  ELSE NULL END
) STORED;
