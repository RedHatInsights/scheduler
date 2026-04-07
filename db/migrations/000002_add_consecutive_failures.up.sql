-- Add consecutive_failures column to jobs table
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS consecutive_failures INTEGER NOT NULL DEFAULT 0;
