-- Rename last_run column to last_run_at for consistency
ALTER TABLE jobs RENAME COLUMN last_run TO last_run_at;
