-- PostgreSQL enum values cannot be removed safely without rebuilding the type
-- and rewriting dependent columns; see 004_job_status_scan.down.sql. Keep the
-- enum change as a documented no-op and only drop the added column.
ALTER TABLE stream_jobs DROP COLUMN IF EXISTS source_url;
