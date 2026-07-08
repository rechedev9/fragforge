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

-- Remove the demos/artifacts Storage buckets and their objects. Deleting the
-- objects rows first satisfies the storage.objects -> storage.buckets FK;
-- this mirrors Supabase's own documented pattern for dropping a bucket in a
-- migration (storage.objects rows do not cascade automatically).
delete from storage.objects where bucket_id in ('demos', 'artifacts');
delete from storage.buckets where id in ('demos', 'artifacts');
