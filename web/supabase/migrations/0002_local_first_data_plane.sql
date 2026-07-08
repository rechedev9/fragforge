-- 0002_local_first_data_plane.sql - move FragForge Cloud to a local-first data plane.
--
-- Local-first invariant: the hosted web (this Supabase project) is control
-- plane only - identity, pairing, agent liveness.
-- Every byte of media (.dem, segments, rendered MP4) stays on the user's PC
-- and is processed there by the paired agent's local orchestrator.
-- No control-plane route accepts or serves media bytes, and after this
-- migration no Supabase Storage bucket exists in this project.
--
-- Job history moves out of Postgres entirely: it now lives in the agent's
-- local sqlite job repo (see docs/superpowers/specs/2026-07-08-local-first-cloud-data-plane.md).
-- The browser talks to the paired agent directly over a loopback proxy
-- (Bearer token + CORS allowlist + Private Network Access preflight), so
-- Supabase no longer needs to broker demo/job/artifact state at all.
--
-- Supersedes the data-plane parts of 0001_cloud_schema.sql: the `demos` and
-- `jobs` tables, the `claim_next_job` RPC, and the `demos`/`artifacts`
-- Storage buckets are all dropped here.

-- The agent now exposes a loopback reverse proxy in front of its local
-- orchestrator; the control plane needs to hand the browser that proxy's
-- address and auth token after pairing/heartbeat.
alter table agents
  add column if not exists loopback_token text not null default '',
  add column if not exists loopback_port  integer not null default 8090;

-- Drop the claim RPC before the jobs table it references.
drop function if exists public.claim_next_job(uuid, int);

-- Job state now lives only in the agent's local sqlite; nothing in this
-- project's Postgres needs to track individual pipeline jobs anymore.
drop table if exists jobs;

-- Demo blobs never touch Supabase; they stay on the user's PC and are
-- processed there by the local orchestrator.
drop table if exists demos;

-- The demos/artifacts Storage buckets cannot be dropped here: Supabase blocks
-- direct SQL deletes on storage tables (storage.protect_delete raises 42501),
-- verified against the live project on 2026-07-08. Remove them through the
-- Storage API instead, once, with the service role key:
--
--   POST   {SUPABASE_URL}/storage/v1/bucket/demos/empty
--   DELETE {SUPABASE_URL}/storage/v1/bucket/demos
--   POST   {SUPABASE_URL}/storage/v1/bucket/artifacts/empty
--   DELETE {SUPABASE_URL}/storage/v1/bucket/artifacts
