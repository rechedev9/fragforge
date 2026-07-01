-- 0001_cloud_schema.sql - FragForge Cloud control-plane schema.
-- Deployed to the Supabase project `fragforge-cloud` (ref wbpjuilfrnzdxfluulnr).
--
-- Access model: the Next.js control-plane talks to Postgres only with the
-- service-role key (which bypasses RLS). RLS is enabled on every table with no
-- policies, so anon/authenticated get zero access through the Data API. The
-- `rls_enabled_no_policy` advisory is therefore expected and intentional.

create extension if not exists pgcrypto;

create table if not exists users (
  id          uuid primary key default gen_random_uuid(),
  steam_id    text unique not null,
  persona     text not null default '',
  avatar      text not null default '',
  created_at  timestamptz not null default now()
);

create table if not exists agents (
  id                uuid primary key default gen_random_uuid(),
  user_id           uuid not null references users(id) on delete cascade,
  name              text not null default 'PC',
  token_hash        text not null,
  capabilities      jsonb not null default '{}'::jsonb,
  last_heartbeat_at timestamptz,
  created_at        timestamptz not null default now()
);
create index if not exists agents_user_idx on agents(user_id);

create table if not exists demos (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users(id) on delete cascade,
  storage_key text not null,
  filename    text not null,
  size        bigint not null,
  sha256      text not null default '',
  state       text not null default 'uploaded',
  created_at  timestamptz not null default now()
);
create index if not exists demos_user_idx on demos(user_id);

-- One row per unit of agent work. state uses the same vocabulary as the Go
-- job.Status enum so the agent and cloud agree without translation surprises.
create table if not exists jobs (
  id                uuid primary key default gen_random_uuid(),
  demo_id           uuid not null references demos(id) on delete cascade,
  user_id           uuid not null references users(id) on delete cascade,
  agent_id          uuid references agents(id) on delete set null,
  type              text not null check (type in ('scan', 'parse', 'capture')),
  state             text not null default 'queued',
  target_steamid    text not null default '',
  rules             jsonb not null default '{}'::jsonb,
  kill_plan         jsonb,
  error             text not null default '',
  attempt           int not null default 0,
  lease_expires_at  timestamptz,
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now()
);
create index if not exists jobs_claimable_idx on jobs(user_id, state, created_at);

-- Atomically hand the oldest queued job for the agent's user to that agent.
-- FOR UPDATE SKIP LOCKED prevents two agents grabbing the same row.
-- search_path is pinned to '' (identifiers fully qualified) to avoid
-- search_path injection, per the Supabase database linter.
create or replace function public.claim_next_job(p_agent uuid, p_lease_seconds int)
returns public.jobs
language plpgsql
set search_path = ''
as $$
declare
  v_user uuid;
  v_job  public.jobs;
begin
  select user_id into v_user from public.agents where id = p_agent;
  if v_user is null then
    return null;
  end if;

  select * into v_job
  from public.jobs
  where user_id = v_user and state = 'queued'
  order by created_at
  for update skip locked
  limit 1;

  if v_job.id is null then
    return null;
  end if;

  update public.jobs
  set state = 'claimed',
      agent_id = p_agent,
      attempt = attempt + 1,
      lease_expires_at = pg_catalog.now() + pg_catalog.make_interval(secs => p_lease_seconds),
      updated_at = pg_catalog.now()
  where id = v_job.id
  returning * into v_job;

  return v_job;
end;
$$;

alter table users  enable row level security;
alter table agents enable row level security;
alter table demos  enable row level security;
alter table jobs   enable row level security;

-- Private storage buckets: demos in, artifacts (roster/moments/reels) out.
-- Access is server-side only via signed URLs minted by the control-plane.
insert into storage.buckets (id, name, public)
values ('demos', 'demos', false), ('artifacts', 'artifacts', false)
on conflict (id) do nothing;
