DROP INDEX IF EXISTS idx_cronjobs_cluster_live;
ALTER TABLE clusters DROP COLUMN deleted_at;
ALTER TABLE cronjobs DROP COLUMN deleted_at;
