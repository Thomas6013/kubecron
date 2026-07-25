-- BUG-20: CronJobs and clusters removed from Kubernetes were never removed or
-- flagged, so they lingered as ghost rows in the UI and as stale Prometheus
-- series forever. Soft-delete them instead: listings hide them, run history is
-- preserved and stays reachable by direct link, and the retention job purges
-- the rows once their runs have aged out.
ALTER TABLE cronjobs ADD COLUMN deleted_at DATETIME;
ALTER TABLE clusters ADD COLUMN deleted_at DATETIME;

-- Listings always filter on deleted_at IS NULL; a partial index keeps that the
-- cheap path as deleted rows accumulate.
CREATE INDEX idx_cronjobs_cluster_live ON cronjobs(cluster_id) WHERE deleted_at IS NULL;
