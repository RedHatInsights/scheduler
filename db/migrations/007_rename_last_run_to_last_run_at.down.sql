-- Revert last_run_at column name back to last_run
ALTER TABLE jobs RENAME COLUMN last_run_at TO last_run;
