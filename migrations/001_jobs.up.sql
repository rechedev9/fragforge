CREATE TYPE job_status AS ENUM (
    'queued', 'parsing', 'parsed', 'failed'
);

CREATE TABLE jobs (
    id              UUID PRIMARY KEY,
    status          job_status NOT NULL DEFAULT 'queued',
    failure_reason  TEXT,
    demo_path       TEXT NOT NULL,
    demo_sha256     TEXT NOT NULL,
    target_steamid  TEXT NOT NULL,
    rules           JSONB NOT NULL,
    kill_plan       JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX jobs_status_idx ON jobs(status);
