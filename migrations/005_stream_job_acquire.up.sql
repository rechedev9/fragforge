-- Acquisition-by-URL: a stream job can now be created from a source_url
-- (yt-dlp download) instead of a multipart upload. 'acquiring' is the status
-- while the download+probe runs; appended before 'uploaded' to mirror the
-- pipeline order (acquire -> ready), matching the Go Status iota order.
ALTER TYPE stream_job_status ADD VALUE IF NOT EXISTS 'acquiring' BEFORE 'uploaded';

ALTER TABLE stream_jobs ADD COLUMN IF NOT EXISTS source_url TEXT;
