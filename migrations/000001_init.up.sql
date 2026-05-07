CREATE TABLE clusters (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  metrics_enabled INTEGER NOT NULL DEFAULT 0,
  created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE cronjobs (
  id                   TEXT PRIMARY KEY,
  cluster_id           TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  namespace            TEXT NOT NULL,
  name                 TEXT NOT NULL,
  schedule             TEXT NOT NULL,
  suspended            INTEGER NOT NULL DEFAULT 0,
  cpu_request          TEXT,
  cpu_limit            TEXT,
  memory_request       TEXT,
  memory_limit         TEXT,
  last_successful_time DATETIME,
  updated_at           DATETIME NOT NULL DEFAULT (datetime('now')),
  UNIQUE(cluster_id, namespace, name)
);

CREATE TABLE job_runs (
  id               TEXT PRIMARY KEY,
  cronjob_id       TEXT NOT NULL REFERENCES cronjobs(id) ON DELETE CASCADE,
  pod_name         TEXT NOT NULL,
  node_name        TEXT,
  container_image  TEXT,
  trigger          TEXT NOT NULL CHECK(trigger IN ('scheduled', 'manual')),
  started_at       DATETIME NOT NULL,
  finished_at      DATETIME,
  status           TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running','succeeded','failed')),
  exit_code        INTEGER,
  retry_count      INTEGER NOT NULL DEFAULT 0,
  log_size_bytes   INTEGER NOT NULL DEFAULT 0,
  avg_cpu_millicores   INTEGER,
  max_cpu_millicores   INTEGER,
  avg_memory_bytes     INTEGER,
  max_memory_bytes     INTEGER,
  duration_ms INTEGER GENERATED ALWAYS AS (
    CASE WHEN finished_at IS NOT NULL
    THEN CAST((julianday(finished_at) - julianday(started_at)) * 86400000 AS INTEGER)
    ELSE NULL END
  ) STORED
);

CREATE TABLE resource_samples (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id          TEXT NOT NULL REFERENCES job_runs(id) ON DELETE CASCADE,
  sampled_at      DATETIME NOT NULL,
  cpu_millicores  INTEGER NOT NULL,
  memory_bytes    INTEGER NOT NULL
);

CREATE TABLE log_lines (
  id     INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL REFERENCES job_runs(id) ON DELETE CASCADE,
  ts     DATETIME NOT NULL DEFAULT (datetime('now')),
  line   TEXT NOT NULL
);

CREATE INDEX idx_log_lines_run        ON log_lines(run_id);
CREATE INDEX idx_resource_samples_run  ON resource_samples(run_id);
CREATE INDEX idx_job_runs_cronjob      ON job_runs(cronjob_id);
CREATE INDEX idx_job_runs_started      ON job_runs(started_at);
CREATE INDEX idx_cronjobs_cluster      ON cronjobs(cluster_id);
