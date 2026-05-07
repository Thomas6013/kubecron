DROP INDEX IF EXISTS idx_cronjobs_cluster;
DROP INDEX IF EXISTS idx_job_runs_started;
DROP INDEX IF EXISTS idx_job_runs_cronjob;
DROP INDEX IF EXISTS idx_resource_samples_run;
DROP INDEX IF EXISTS idx_log_lines_run;

DROP TABLE IF EXISTS log_lines;
DROP TABLE IF EXISTS resource_samples;
DROP TABLE IF EXISTS job_runs;
DROP TABLE IF EXISTS cronjobs;
DROP TABLE IF EXISTS clusters;
