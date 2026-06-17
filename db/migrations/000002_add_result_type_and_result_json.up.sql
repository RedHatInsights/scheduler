-- Add result_type column for type discrimination
ALTER TABLE job_runs ADD COLUMN result_type TEXT;

-- Add new result_json column as JSONB (leaves old 'result' TEXT column intact)
-- The application code will use result_json going forward
-- Old 'result' column is orphaned but preserved for safety
ALTER TABLE job_runs ADD COLUMN result_json JSONB;

-- Create GIN index for efficient JSON queries on the new column
CREATE INDEX idx_job_runs_result_json_gin ON job_runs USING gin(result_json);
