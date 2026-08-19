ALTER TABLE job_runs
ADD COLUMN IF NOT EXISTS external_job_id TEXT,
ADD COLUMN IF NOT EXISTS external_service TEXT,
ADD COLUMN IF NOT EXISTS poll_started_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_job_runs_external_job_id ON job_runs(external_job_id) WHERE external_job_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_job_runs_status_external_job_id ON job_runs(status, external_job_id) WHERE status = 'running' AND external_job_id IS NOT NULL;
