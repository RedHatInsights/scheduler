-- Remove consecutive_failures column from jobs table
ALTER TABLE jobs DROP COLUMN IF EXISTS consecutive_failures;
