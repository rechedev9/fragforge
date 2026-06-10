CREATE TYPE stream_job_status AS ENUM (
    'uploaded',
    'ready',
    'rendering',
    'rendered',
    'failed'
);

CREATE TABLE stream_jobs (
    id              UUID PRIMARY KEY,
    status          stream_job_status NOT NULL DEFAULT 'uploaded',
    failure_reason  TEXT,
    source_path     TEXT NOT NULL,
    source_sha256   TEXT NOT NULL,
    title           TEXT,
    probe           JSONB NOT NULL,
    edit_plan       JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX stream_jobs_status_idx ON stream_jobs(status);
