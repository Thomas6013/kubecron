-- duration_ms was a STORED generated column computed with julianday(), but the
-- timestamp format written by the SQLite driver is not parseable by julianday(),
-- so the column was always NULL. This broke every aggregate that read it
-- (run-duration sparklines, 7-day avg/max duration). Replace it with a plain
-- column that the application populates in Go when a run finishes.
ALTER TABLE job_runs DROP COLUMN duration_ms;
ALTER TABLE job_runs ADD COLUMN duration_ms INTEGER;
