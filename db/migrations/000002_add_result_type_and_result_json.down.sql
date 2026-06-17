-- Drop GIN index
DROP INDEX IF EXISTS idx_job_runs_result_json_gin;

-- Remove result_json column
ALTER TABLE job_runs DROP COLUMN result_json;

-- Remove result_type column
ALTER TABLE job_runs DROP COLUMN result_type;
