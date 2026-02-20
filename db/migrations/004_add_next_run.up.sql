ALTER TABLE jobs ADD COLUMN next_run_at TEXT;

CREATE INDEX IF NOT EXISTS idx_jobs_next_run_at ON jobs(next_run_at);
