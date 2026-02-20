DROP INDEX IF EXISTS idx_jobs_timezone;

ALTER TABLE jobs DROP COLUMN timezone;
