-- Roster-scan lifecycle: a demo uploaded without a target is scanned first so
-- the user can pick a player, then parsed. Appended after 'failed' to mirror the
-- Go Status iota order; enum position does not affect lookups (the column stores
-- the canonical name). target_steamid stays TEXT NOT NULL: a scan-first job
-- carries an empty string until the user picks a target via /parse.
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'scanning' AFTER 'failed';
ALTER TYPE job_status ADD VALUE IF NOT EXISTS 'scanned' AFTER 'scanning';
