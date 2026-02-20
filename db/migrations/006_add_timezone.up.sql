ALTER TABLE jobs ADD COLUMN timezone TEXT DEFAULT 'UTC';

CREATE INDEX IF NOT EXISTS idx_jobs_timezone ON jobs(timezone);
