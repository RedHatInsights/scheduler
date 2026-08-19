DROP INDEX IF EXISTS idx_job_runs_status_external_job_id;
DROP INDEX IF EXISTS idx_job_runs_external_job_id;

ALTER TABLE job_runs
DROP COLUMN IF EXISTS poll_started_at,
DROP COLUMN IF EXISTS external_service,
DROP COLUMN IF EXISTS external_job_id;
