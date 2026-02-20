DROP INDEX IF EXISTS idx_jobs_next_run_at;

ALTER TABLE jobs DROP COLUMN next_run_at;
