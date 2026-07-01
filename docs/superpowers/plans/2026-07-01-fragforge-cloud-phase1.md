# FragForge Cloud - Fase 1 (Esqueleto andante: emparejamiento + subida + roster) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Probar end-to-end el bucle Vercel (cerebro TS) <-> agente Go (PC) <-> Supabase: el usuario empareja su PC, sube una `.dem`, el agente la reclama y la parsea (roster) localmente, y la UI muestra los jugadores.

**Architecture:** El control-plane vive en route handlers Next.js sobre Vercel y usa Supabase (Postgres + Storage) como único estado compartido. El agente es un binario Go nuevo (`cmd/zv-agent`) que conecta hacia fuera por HTTP (long-poll), reclama trabajos, y reutiliza `ParserWorker` del repo implementando `workers.JobRepository` y `storage.Storage` contra la nube. Vercel no ejecuta ningún binario nativo.

**Tech Stack:** Go 1.26.1 (stdlib `net/http`, `google/uuid`, reuse `internal/*`), Next.js App Router + TypeScript, `@supabase/supabase-js`, Supabase (Postgres + Storage), Playwright + `node --test`.

## Global Constraints

- Módulo Go: `github.com/rechedev9/fragforge`; Go `1.26.1`.
- No añadir dependencias Go ni ejecutar `go mod tidy` sin aprobación explícita; el agente usa solo `net/http`, `encoding/json`, `google/uuid` (ya presentes) y `internal/*`.
- Commits: `git add` directo está bloqueado por política del repo; usar `committer "mensaje" archivo [archivo ...]` (en `PATH`). Terminar cada mensaje de commit con `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- El agente solo conecta hacia fuera; nunca abre puertos entrantes.
- No ejecutar HLAE/CS2/FFmpeg ni migraciones destructivas en tests; captura es Fase 2 (fuera de alcance aquí).
- Gate Go: `scripts/go-gate.sh` (fmt, vet, build, tests). Gate web: `npm run typecheck` y `npm run test:e2e` en `web/`.
- No editar `AGENTS.md` a mano (se autogenera desde `CLAUDE.md`).
- Estilo Markdown de specs/planes: una frase por línea física; nunca em dash, usar `-`.
- Contrato del control-plane (fijo para todo el plan):
  - El handle de demo que ve el navegador es el `demo_id` (uuid).
  - El agente ve un job como el JSON de `job.Job` con `id = demo_id`, `demo_path = demos.storage_key`, `demo_sha256`, `target_steamid` (vacío en scan), `rules`.
  - La nube garantiza como mucho un job activo por demo (el flujo es secuencial), así que direccionar un job por `demo_id` no es ambiguo.
  - Claves de artefactos (las escribe el agente vía `storage.Storage`): `artifacts.RosterKey(demoID)` = `jobs/{demoID}/roster.json`, en el bucket Storage `artifacts`.
  - La `.dem` vive en el bucket `demos` con clave `demos/{userId}/{demoId}.dem`.

---

## File Structure

Go (nuevo salvo el refactor):

- Modify: `internal/workers/parser_worker.go` - extraer `ProcessParseDemo`/`ProcessScanRoster` exportados.
- Create: `cmd/zv-agent/main.go` - entrypoint, flags (`--pair`, run loop).
- Create: `cmd/zv-agent/config.go` - lectura/escritura del fichero de config con el token (perms 0600).
- Create: `internal/agent/client.go` - cliente HTTP hacia la nube (auth, base URL, backoff).
- Create: `internal/agent/pair.go` - canje del código de emparejamiento.
- Create: `internal/agent/heartbeat.go` - bucle de heartbeat.
- Create: `internal/agent/jobrepo.go` - `cloudJobRepo` implementa `workers.JobRepository`.
- Create: `internal/agent/storage.go` - `cloudStorage` implementa `storage.Storage`.
- Create: `internal/agent/runner.go` - bucle claim + dispatch a `ParserWorker`.
- Create tests junto a cada fichero (`*_test.go`).

TypeScript / Next.js (bajo `web/`):

- Create: `web/supabase/migrations/0001_cloud_schema.sql` - esquema + `claim_next_job()`.
- Create: `web/lib/supabase/server.ts` - cliente service-role (solo servidor).
- Create: `web/lib/cloud/users.ts` - `ensureUser()`.
- Create: `web/lib/cloud/agentAuth.ts` - resolver el agente desde el bearer token.
- Create: `web/app/api/pc/pair/route.ts` - navegador: emite código.
- Create: `web/app/api/pc/status/route.ts` - navegador: estado online del PC.
- Create: `web/app/api/agent/pair/route.ts` - agente: canjea código -> token.
- Create: `web/app/api/agent/heartbeat/route.ts` - agente: latido.
- Create: `web/app/api/agent/jobs/claim/route.ts` - agente: long-poll claim.
- Create: `web/app/api/agent/jobs/[id]/route.ts` - agente: GET job DTO.
- Create: `web/app/api/agent/jobs/[id]/status/route.ts` - agente: update estado.
- Create: `web/app/api/agent/jobs/[id]/complete/route.ts` - agente: cierre ok.
- Create: `web/app/api/agent/jobs/[id]/fail/route.ts` - agente: cierre error.
- Create: `web/app/api/agent/blobs/sign-upload/route.ts` - agente: URL firmada de subida.
- Create: `web/app/api/agent/blobs/download/route.ts` - agente: URL firmada de bajada.
- Create: `web/app/api/agent/blobs/exists/route.ts` - agente: existencia.
- Modify: `web/app/api/demos/scan/route.ts` - subir a Supabase + crear scan job.
- Modify: `web/app/api/demos/[jobId]/status/route.ts` - leer estado desde `jobs`.
- Modify: `web/app/api/demos/[jobId]/roster/route.ts` - leer `roster.json` de Storage.
- Modify: `web/app/api/auth/steam/callback/route.ts` - upsert `users`.
- Modify: `web/lib/api/real.ts` - `scanDemo`/`pairPc`/`getPcStatus` async reales.
- Modify: `web/app/upload/page.tsx` - polling scanning -> scanned -> roster, estado "PC offline".

---

## Task 1: Refactor ParserWorker para invocación sin Asynq

**Files:**
- Modify: `internal/workers/parser_worker.go`
- Test: `internal/workers/parser_worker_test.go`

**Interfaces:**
- Consumes: `workers.JobRepository` (`GetMeta`, `UpdateStatus`, `SetKillPlan`), `storage.Storage`.
- Produces: `func (w *ParserWorker) ProcessParseDemo(ctx context.Context, jobID uuid.UUID) error` y `func (w *ParserWorker) ProcessScanRoster(ctx context.Context, jobID uuid.UUID) error`. El agente (Task 10) los llama directamente.

- [ ] **Step 1: Write the failing test**

Añadir a `internal/workers/parser_worker_test.go`:

```go
func TestProcessScanRoster_BadDemoMarksFailed(t *testing.T) {
	repo := newFakeJobRepo(job.Job{ID: uuid.New(), Status: job.StatusQueued, DemoPath: "demos/missing.dem"})
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	w := workers.NewParserWorker(repo, store)

	got := w.ProcessScanRoster(context.Background(), repo.only().ID)
	if got == nil {
		t.Fatalf("got nil error, want failure opening missing demo")
	}
	if repo.lastStatus() != job.StatusFailed {
		t.Errorf("got status %v, want %v", repo.lastStatus(), job.StatusFailed)
	}
}
```

If `newFakeJobRepo`/`only`/`lastStatus` do not exist yet, add a minimal fake in the test file implementing `workers.JobRepository` and recording the last status set by `UpdateStatus`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workers -run TestProcessScanRoster_BadDemoMarksFailed -count=1`
Expected: FAIL with `w.ProcessScanRoster undefined`.

- [ ] **Step 3: Extract the exported methods**

In `internal/workers/parser_worker.go`, change `HandleParseDemo` and `HandleScanRoster` to decode the payload and delegate, and add the two exported methods that hold the body that used to live in the handlers:

```go
// HandleParseDemo is the Asynq handler signature.
func (w *ParserWorker) HandleParseDemo(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ParseDemoPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return w.ProcessParseDemo(ctx, payload.JobID)
}

// ProcessParseDemo runs the parse stage for one job, independent of any queue.
func (w *ParserWorker) ProcessParseDemo(ctx context.Context, jobID uuid.UUID) error {
	j, err := w.repo.GetMeta(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", jobID, err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusParsing, ""); err != nil {
		return fmt.Errorf("mark parsing: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeParseDemo, job.StatusParsing)

	plan, parseErr := w.parse(ctx, j)
	if parseErr != nil {
		recordTaskFailure(ctx, w.repo, j.ID, tasks.TypeParseDemo, parseErr)
		return parseErr
	}
	if err := w.repo.SetKillPlan(ctx, j.ID, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	momentsKey, err := w.writeMoments(j.ID, plan)
	if err != nil {
		return fmt.Errorf("write moments: %w", err)
	}
	logWorkerArtifacts(j.ID, tasks.TypeParseDemo, []string{"kill_plan", momentsKey})
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusParsed, ""); err != nil {
		return fmt.Errorf("mark parsed: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeParseDemo, job.StatusParsed)
	return nil
}

// HandleScanRoster is the Asynq handler for scan:roster.
func (w *ParserWorker) HandleScanRoster(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ScanRosterPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return w.ProcessScanRoster(ctx, payload.JobID)
}

// ProcessScanRoster runs the roster scan for one job, independent of any queue.
func (w *ParserWorker) ProcessScanRoster(ctx context.Context, jobID uuid.UUID) error {
	j, err := w.repo.GetMeta(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", jobID, err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusScanning, ""); err != nil {
		return fmt.Errorf("mark scanning: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeScanRoster, job.StatusScanning)

	rosterKey, scanErr := w.scanRoster(ctx, j)
	if scanErr != nil {
		recordTaskFailure(ctx, w.repo, j.ID, tasks.TypeScanRoster, scanErr)
		return scanErr
	}
	logWorkerArtifacts(j.ID, tasks.TypeScanRoster, []string{rosterKey})
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusScanned, ""); err != nil {
		return fmt.Errorf("mark scanned: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeScanRoster, job.StatusScanned)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/workers -count=1`
Expected: PASS (new test plus all existing `HandleParseDemo`/`HandleScanRoster` tests, now routed through `Process*`).

- [ ] **Step 5: Commit**

```bash
committer "refactor(workers): expose ProcessParseDemo/ProcessScanRoster for non-Asynq callers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" internal/workers/parser_worker.go internal/workers/parser_worker_test.go
```

---

## Task 2: Esquema Supabase + funcion de claim atomico

**Files:**
- Create: `web/supabase/migrations/0001_cloud_schema.sql`

**Interfaces:**
- Produces: tablas `users`, `agents`, `demos`, `jobs`; función `claim_next_job(p_agent uuid, p_lease_seconds int)`; buckets `demos` y `artifacts`. Consumido por todas las tareas TS.

- [ ] **Step 1: Write the migration SQL**

```sql
-- 0001_cloud_schema.sql - FragForge Cloud control-plane schema.

create extension if not exists pgcrypto;

create table users (
  id          uuid primary key default gen_random_uuid(),
  steam_id    text unique not null,
  persona     text not null default '',
  avatar      text not null default '',
  created_at  timestamptz not null default now()
);

create table agents (
  id                uuid primary key default gen_random_uuid(),
  user_id           uuid not null references users(id) on delete cascade,
  name              text not null default 'PC',
  token_hash        text not null,
  capabilities      jsonb not null default '{}'::jsonb,
  last_heartbeat_at timestamptz,
  created_at        timestamptz not null default now()
);
create index agents_user_idx on agents(user_id);

create table demos (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users(id) on delete cascade,
  storage_key text not null,
  filename    text not null,
  size        bigint not null,
  sha256      text not null default '',
  state       text not null default 'uploaded',
  created_at  timestamptz not null default now()
);
create index demos_user_idx on demos(user_id);

-- One row per unit of agent work. state uses the same vocabulary as the Go
-- job.Status enum so the agent and cloud agree without translation surprises.
create table jobs (
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
create index jobs_claimable_idx on jobs(user_id, state, created_at);

-- Atomically hand the oldest queued job for the agent's user to that agent.
-- FOR UPDATE SKIP LOCKED prevents two agents grabbing the same row.
create or replace function claim_next_job(p_agent uuid, p_lease_seconds int)
returns jobs
language plpgsql
as $$
declare
  v_user uuid;
  v_job  jobs;
begin
  select user_id into v_user from agents where id = p_agent;
  if v_user is null then
    return null;
  end if;

  select * into v_job
  from jobs
  where user_id = v_user and state = 'queued'
  order by created_at
  for update skip locked
  limit 1;

  if v_job.id is null then
    return null;
  end if;

  update jobs
  set state = 'claimed',
      agent_id = p_agent,
      attempt = attempt + 1,
      lease_expires_at = now() + make_interval(secs => p_lease_seconds),
      updated_at = now()
  where id = v_job.id
  returning * into v_job;

  return v_job;
end;
$$;
```

- [ ] **Step 2: Apply the migration and create buckets**

Provisiona un proyecto Supabase (o usa el existente) y exporta en `web/.env.local`:
`SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`, `SUPABASE_ANON_KEY`.

Aplica la migración (usa el MCP de Supabase `apply_migration`, o la CLI):

Run: `supabase db push` (o el equivalente `apply_migration` del MCP con el contenido de `0001_cloud_schema.sql`).
Crea dos buckets privados: `demos` y `artifacts` (MCP/CLI/consola; privados, sin acceso público).

- [ ] **Step 3: Verify the schema and the claim function**

Run (psql o SQL editor):
`select claim_next_job('00000000-0000-0000-0000-000000000000', 60);`
Expected: una fila `null` (no hay agente ni jobs), sin error de sintaxis.
`\d jobs` muestra las columnas anteriores.

- [ ] **Step 4: Commit**

```bash
committer "feat(cloud): Supabase schema and atomic claim_next_job for control-plane

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/supabase/migrations/0001_cloud_schema.sql
```

---

## Task 3: Cliente Supabase server-side + ensureUser

**Files:**
- Create: `web/lib/supabase/server.ts`
- Create: `web/lib/cloud/users.ts`
- Test: `web/lib/cloud/users.test.mjs`
- Modify: `web/package.json` (dep `@supabase/supabase-js`)

**Interfaces:**
- Produces: `supabaseAdmin(): SupabaseClient` (service-role, solo servidor) y `ensureUser(steamId: string, persona: string, avatar: string): Promise<string>` que devuelve `user_id`.

- [ ] **Step 1: Add the dependency**

Run: `cd web && npm install @supabase/supabase-js`

- [ ] **Step 2: Write the server client**

`web/lib/supabase/server.ts`:

```typescript
import 'server-only';
import { createClient, type SupabaseClient } from '@supabase/supabase-js';

let client: SupabaseClient | null = null;

/** Service-role Supabase client. Server-only: never import into a client component. */
export function supabaseAdmin(): SupabaseClient {
  if (client) return client;
  const url = process.env.SUPABASE_URL;
  const key = process.env.SUPABASE_SERVICE_ROLE_KEY;
  if (!url || !key) throw new Error('supabase env not configured');
  client = createClient(url, key, { auth: { persistSession: false } });
  return client;
}
```

- [ ] **Step 3: Write ensureUser with an injectable client for testing**

`web/lib/cloud/users.ts`:

```typescript
import type { SupabaseClient } from '@supabase/supabase-js';
import { supabaseAdmin } from '@/lib/supabase/server';

/** Upsert the Steam user and return its internal user id. */
export async function ensureUser(
  steamId: string,
  persona: string,
  avatar: string,
  db: SupabaseClient = supabaseAdmin(),
): Promise<string> {
  const { data, error } = await db
    .from('users')
    .upsert({ steam_id: steamId, persona, avatar }, { onConflict: 'steam_id' })
    .select('id')
    .single();
  if (error) throw new Error(`ensureUser: ${error.message}`);
  return data.id as string;
}
```

- [ ] **Step 4: Write the test with a fake client**

`web/lib/cloud/users.test.mjs`:

```javascript
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ensureUser } from './users.ts';

function fakeDb(captured) {
  return {
    from() { return this; },
    upsert(row, opts) { captured.row = row; captured.opts = opts; return this; },
    select() { return this; },
    async single() { return { data: { id: 'user-1' }, error: null }; },
  };
}

test('ensureUser upserts on steam_id and returns the id', async () => {
  const captured = {};
  const id = await ensureUser('7656', 'zack', 'http://a', fakeDb(captured));
  assert.equal(id, 'user-1');
  assert.equal(captured.row.steam_id, '7656');
  assert.equal(captured.opts.onConflict, 'steam_id');
});
```

- [ ] **Step 5: Run the test**

Run: `cd web && node --test lib/cloud/users.test.mjs`
Expected: PASS. (If `.ts` import needs a loader, run via the repo's existing test runner used for `web/lib/api/*.test.mjs`; mirror that invocation.)

- [ ] **Step 6: Commit**

```bash
committer "feat(cloud): Supabase server client and ensureUser upsert

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/lib/supabase/server.ts web/lib/cloud/users.ts web/lib/cloud/users.test.mjs web/package.json web/package-lock.json
```

---

## Task 4: Persistir el usuario Steam en el callback

**Files:**
- Modify: `web/app/api/auth/steam/callback/route.ts`

**Interfaces:**
- Consumes: `ensureUser` (Task 3), `signSession` (`web/lib/auth/session.ts`).
- Produces: al completar el login Steam existe una fila `users` para ese `steamid64`. La sesión sigue igual (identidad = `steamid64`).

- [ ] **Step 1: Call ensureUser after verifying the Steam identity**

En `web/app/api/auth/steam/callback/route.ts`, tras resolver `steamid`/`persona`/`avatar` y antes de `signSession`, insertar:

```typescript
import { ensureUser } from '@/lib/cloud/users';
// ...
await ensureUser(steamid, persona, avatar);
const token = signSession({ steamid64: steamid, persona, avatar, matchHistoryLinked: false });
```

- [ ] **Step 2: Typecheck**

Run: `cd web && npm run typecheck`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
committer "feat(auth): persist the Steam user on login callback

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/app/api/auth/steam/callback/route.ts
```

---

## Task 5: Emparejamiento - lado nube

**Files:**
- Create: `web/lib/cloud/agentAuth.ts`
- Create: `web/app/api/pc/pair/route.ts`
- Create: `web/app/api/agent/pair/route.ts`
- Test: `web/lib/cloud/agentAuth.test.mjs`

**Interfaces:**
- Consumes: `supabaseAdmin`, `ensureUser`, session cookie.
- Produces:
  - `hashToken(raw: string): string` (sha256 hex) y `resolveAgent(req: Request): Promise<{ agentId: string, userId: string } | null>` (lee `Authorization: Bearer`).
  - `POST /api/pc/pair` (navegador, autenticado) -> `{ pairingCode: string }`.
  - `POST /api/agent/pair` body `{ code: string, name?: string }` -> `{ token: string, agentId: string }`.
- Contrato de pairing: el código es un registro efímero en `agents` con `token_hash` = hash de un código de un solo uso, TTL 10 min. Al canjear, el agente recibe un token de agente definitivo (256-bit) cuyo hash se guarda en `token_hash`.

- [ ] **Step 1: Write agentAuth helpers + test**

`web/lib/cloud/agentAuth.ts`:

```typescript
import { createHash, randomBytes } from 'crypto';
import { supabaseAdmin } from '@/lib/supabase/server';

export function hashToken(raw: string): string {
  return createHash('sha256').update(raw).digest('hex');
}

export function newToken(): string {
  return randomBytes(32).toString('hex');
}

/** Resolve the agent from a Bearer token, or null if missing/unknown. */
export async function resolveAgent(req: Request): Promise<{ agentId: string; userId: string } | null> {
  const auth = req.headers.get('authorization') ?? '';
  const raw = auth.startsWith('Bearer ') ? auth.slice(7) : '';
  if (!raw) return null;
  const { data, error } = await supabaseAdmin()
    .from('agents')
    .select('id, user_id')
    .eq('token_hash', hashToken(raw))
    .maybeSingle();
  if (error || !data) return null;
  return { agentId: data.id as string, userId: data.user_id as string };
}
```

`web/lib/cloud/agentAuth.test.mjs`:

```javascript
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { hashToken, newToken } from './agentAuth.ts';

test('hashToken is stable sha256 hex', () => {
  assert.equal(hashToken('abc'), hashToken('abc'));
  assert.match(hashToken('abc'), /^[0-9a-f]{64}$/);
});

test('newToken is 64 hex chars', () => {
  assert.match(newToken(), /^[0-9a-f]{64}$/);
});
```

Run: `cd web && node --test lib/cloud/agentAuth.test.mjs` -> PASS.

- [ ] **Step 2: Write the browser pairing route**

`web/app/api/pc/pair/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { supabaseAdmin } from '@/lib/supabase/server';
import { hashToken } from '@/lib/cloud/agentAuth';

export const runtime = 'nodejs';

// A short, human-typeable one-time pairing code (no ambiguous chars).
function pairingCode(): string {
  const alphabet = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('');
}

export async function POST(): Promise<Response> {
  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
  const code = pairingCode();
  // Store the code hashed with a 10-minute TTL encoded in the name field.
  const expires = Date.now() + 10 * 60 * 1000;
  const { error } = await supabaseAdmin().from('agents').insert({
    user_id: userId,
    name: `pending:${expires}`,
    token_hash: hashToken(`code:${code}`),
  });
  if (error) return NextResponse.json({ error: 'pairing failed' }, { status: 500 });
  return NextResponse.json({ pairingCode: code });
}
```

- [ ] **Step 3: Write the agent pairing exchange route**

`web/app/api/agent/pair/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';
import { hashToken, newToken } from '@/lib/cloud/agentAuth';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const body = (await request.json().catch(() => null)) as { code?: string; name?: string } | null;
  const code = body?.code?.trim();
  if (!code) return NextResponse.json({ error: 'missing code' }, { status: 400 });

  const db = supabaseAdmin();
  const { data: pending } = await db
    .from('agents')
    .select('id, name')
    .eq('token_hash', hashToken(`code:${code}`))
    .like('name', 'pending:%')
    .maybeSingle();
  if (!pending) return NextResponse.json({ error: 'invalid code' }, { status: 404 });

  const expires = Number(pending.name.split(':')[1] ?? '0');
  if (Date.now() > expires) {
    await db.from('agents').delete().eq('id', pending.id);
    return NextResponse.json({ error: 'code expired' }, { status: 410 });
  }

  const token = newToken();
  const { error } = await db
    .from('agents')
    .update({ token_hash: hashToken(token), name: body?.name?.slice(0, 64) || 'PC' })
    .eq('id', pending.id);
  if (error) return NextResponse.json({ error: 'pairing failed' }, { status: 500 });
  return NextResponse.json({ token, agentId: pending.id });
}
```

- [ ] **Step 4: Typecheck**

Run: `cd web && npm run typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
committer "feat(cloud): PC pairing (browser code issue + agent code exchange)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/lib/cloud/agentAuth.ts web/lib/cloud/agentAuth.test.mjs web/app/api/pc/pair/route.ts web/app/api/agent/pair/route.ts
```

---

## Task 6: Agente Go - esqueleto, config y cliente HTTP

**Files:**
- Create: `cmd/zv-agent/main.go`
- Create: `cmd/zv-agent/config.go`
- Create: `internal/agent/client.go`
- Create: `internal/agent/pair.go`
- Test: `cmd/zv-agent/config_test.go`
- Test: `internal/agent/pair_test.go`

**Interfaces:**
- Produces:
  - `agent.Client` con `func NewClient(baseURL, token string) *Client` y `func (c *Client) Do(ctx context.Context, method, path string, body any, out any) (int, error)`.
  - `agent.Pair(ctx context.Context, baseURL, code, name string) (token string, agentID string, err error)`.
  - `config.Load()`/`config.Save(Config)` con `Config{BaseURL, Token, AgentID}` en `os.UserConfigDir()/fragforge/agent.json` (perms 0600).

- [ ] **Step 1: Write the client + a failing pair test**

`internal/agent/client.go`:

```go
// Package agent is the FragForge capture agent: it connects out to the cloud
// control-plane, claims jobs, and runs the reused media pipeline locally.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin authenticated HTTP client for the cloud control-plane.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Do sends a JSON request and decodes a JSON response into out (may be nil).
// It returns the HTTP status code so callers can branch on 204/4xx.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("cloud %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}
```

`internal/agent/pair.go`:

```go
package agent

import "context"

// Pair exchanges a one-time pairing code for a durable agent token.
func Pair(ctx context.Context, baseURL, code, name string) (string, string, error) {
	c := NewClient(baseURL, "")
	var out struct {
		Token   string `json:"token"`
		AgentID string `json:"agentId"`
	}
	if _, err := c.Do(ctx, "POST", "/api/agent/pair", map[string]string{"code": code, "name": name}, &out); err != nil {
		return "", "", err
	}
	return out.Token, out.AgentID, nil
}
```

`internal/agent/pair_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/pair" {
			t.Errorf("got path %s", r.URL.Path)
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "ABCD2345" {
			t.Errorf("got code %q", in["code"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok", "agentId": "ag1"})
	}))
	defer srv.Close()

	token, id, err := Pair(context.Background(), srv.URL, "ABCD2345", "PC")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if token != "tok" || id != "ag1" {
		t.Errorf("got (%q,%q), want (tok,ag1)", token, id)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/agent -run TestPair -count=1`
Expected: PASS.

- [ ] **Step 3: Write config load/save + test**

`cmd/zv-agent/config.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
	AgentID string `json:"agent_id"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fragforge", "agent.json"), nil
}

func loadConfig() (Config, error) {
	p, err := configPath()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Config{}, err
	}
	var c Config
	return c, json.Unmarshal(b, &c)
}

func saveConfig(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
```

`cmd/zv-agent/config_test.go`:

```go
package main

import (
	"os"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := Config{BaseURL: "https://x", Token: "tok", AgentID: "ag1"}
	if err := saveConfig(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	p, _ := configPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got perms %v, want 0600", info.Mode().Perm())
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
```

(On Windows `os.UserConfigDir` ignores `XDG_CONFIG_HOME`; this test targets the Linux/dev CI where the gate runs. The production path resolves under `%AppData%\fragforge` on the user's PC.)

- [ ] **Step 4: Write the entrypoint**

`cmd/zv-agent/main.go`:

```go
// Command zv-agent is the FragForge capture agent that runs on the user's PC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/rechedev9/fragforge/internal/agent"
)

func main() {
	baseURL := flag.String("cloud", envOr("FRAGFORGE_CLOUD_URL", "https://app.fragforge.gg"), "cloud base URL")
	pairCode := flag.String("pair", "", "pairing code from the web app")
	name := flag.String("name", hostname(), "agent display name")
	flag.Parse()

	ctx := context.Background()

	if *pairCode != "" {
		token, id, err := agent.Pair(ctx, *baseURL, *pairCode, *name)
		if err != nil {
			log.Fatalf("pair: %v", err)
		}
		if err := saveConfig(Config{BaseURL: *baseURL, Token: token, AgentID: id}); err != nil {
			log.Fatalf("save config: %v", err)
		}
		fmt.Println("paired. run zv-agent with no flags to start working.")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("not paired yet: run zv-agent --pair <code> first (%v)", err)
	}
	if err := run(ctx, cfg); err != nil {
		log.Fatalf("agent: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "PC"
	}
	return h
}
```

Add a temporary `run` stub so the package builds; Task 10 replaces it:

`cmd/zv-agent/run.go`:

```go
package main

import "context"

// run is replaced with the claim loop in Task 10.
func run(ctx context.Context, cfg Config) error {
	_ = ctx
	_ = cfg
	return nil
}
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/agent ./cmd/zv-agent -count=1 && go build ./cmd/zv-agent`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
committer "feat(agent): zv-agent skeleton, config store, HTTP client, pairing

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" cmd/zv-agent/main.go cmd/zv-agent/config.go cmd/zv-agent/config_test.go cmd/zv-agent/run.go internal/agent/client.go internal/agent/pair.go internal/agent/pair_test.go
```

---

## Task 7: Heartbeat - nube, agente y estado online en la UI

**Files:**
- Create: `web/app/api/agent/heartbeat/route.ts`
- Create: `web/app/api/pc/status/route.ts`
- Create: `internal/agent/heartbeat.go`
- Test: `internal/agent/heartbeat_test.go`

**Interfaces:**
- Consumes: `resolveAgent` (Task 5), `agent.Client` (Task 6).
- Produces:
  - `POST /api/agent/heartbeat` body `{ capabilities?: object }` -> 204; actualiza `agents.last_heartbeat_at` y `capabilities`.
  - `GET /api/pc/status` (navegador) -> `{ paired: boolean, online: boolean }` (online = heartbeat < 60s).
  - `agent.Heartbeat(ctx, c *Client)` envía un latido; `agent.HeartbeatLoop(ctx, c, every)` late periódicamente.

- [ ] **Step 1: Write the heartbeat route (agent)**

`web/app/api/agent/heartbeat/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const body = (await request.json().catch(() => ({}))) as { capabilities?: unknown };
  await supabaseAdmin()
    .from('agents')
    .update({ last_heartbeat_at: new Date().toISOString(), capabilities: body.capabilities ?? {} })
    .eq('id', agent.agentId);
  return new NextResponse(null, { status: 204 });
}
```

- [ ] **Step 2: Write the PC status route (browser)**

`web/app/api/pc/status/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(): Promise<Response> {
  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
  const { data } = await supabaseAdmin()
    .from('agents')
    .select('last_heartbeat_at')
    .eq('user_id', userId)
    .not('name', 'like', 'pending:%')
    .order('last_heartbeat_at', { ascending: false, nullsFirst: false })
    .limit(1);

  const agent = data?.[0];
  const online = !!agent?.last_heartbeat_at && Date.now() - new Date(agent.last_heartbeat_at).getTime() < 60_000;
  return NextResponse.json({ paired: !!agent, online });
}
```

- [ ] **Step 3: Write the agent heartbeat loop + test**

`internal/agent/heartbeat.go`:

```go
package agent

import (
	"context"
	"time"
)

// Heartbeat sends one liveness ping with the agent's current capabilities.
func Heartbeat(ctx context.Context, c *Client, capabilities map[string]any) error {
	_, err := c.Do(ctx, "POST", "/api/agent/heartbeat", map[string]any{"capabilities": capabilities}, nil)
	return err
}

// HeartbeatLoop pings every interval until ctx is cancelled.
func HeartbeatLoop(ctx context.Context, c *Client, capabilities map[string]any, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	_ = Heartbeat(ctx, c, capabilities)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = Heartbeat(ctx, c, capabilities)
		}
	}
}
```

`internal/agent/heartbeat_test.go`:

```go
package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeartbeat(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Heartbeat(context.Background(), NewClient(srv.URL, "tok"), map[string]any{"parser": true}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("got auth %q, want Bearer tok", gotAuth)
	}
}
```

- [ ] **Step 4: Run tests + typecheck**

Run: `go test ./internal/agent -run TestHeartbeat -count=1 && cd web && npm run typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
committer "feat(agent): heartbeat loop + cloud online status endpoints

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/app/api/agent/heartbeat/route.ts web/app/api/pc/status/route.ts internal/agent/heartbeat.go internal/agent/heartbeat_test.go
```

---

### MILESTONE: handshake probado

En este punto: `zv-agent --pair <code>` empareja, el agente late, y `GET /api/pc/status` reporta `online: true`. Antes de seguir, un checkpoint de revisión humana es recomendable.

---

## Task 8: Rutas de job para el agente (claim / get / status / complete / fail)

**Files:**
- Create: `web/lib/cloud/jobDto.ts`
- Create: `web/app/api/agent/jobs/claim/route.ts`
- Create: `web/app/api/agent/jobs/[id]/route.ts`
- Create: `web/app/api/agent/jobs/[id]/status/route.ts`
- Create: `web/app/api/agent/jobs/[id]/killplan/route.ts`
- Create: `web/app/api/agent/jobs/[id]/complete/route.ts`
- Create: `web/app/api/agent/jobs/[id]/fail/route.ts`
- Test: `web/lib/cloud/jobDto.test.mjs`

**Interfaces:**
- Consumes: `resolveAgent`, `supabaseAdmin`, `claim_next_job` RPC.
- Produces:
  - `toJobDto(row): { id, status, demo_path, demo_sha256, target_steamid, rules }` donde `id = demo_id` y `status` es el entero del enum Go `job.Status` (scan usa `9` StatusScanning / `10` StatusScanned; queued `0`; failed `8`).
  - `POST /api/agent/jobs/claim` -> `200 { job: JobDto, jobType: 'scan'|'parse'|'capture' }` o `204`.
  - `GET /api/agent/jobs/:demoId` -> `JobDto`.
  - `POST /api/agent/jobs/:demoId/status` body `{ status: number, failure_reason?: string }` -> 204.
  - `POST /api/agent/jobs/:demoId/killplan` body `{ kill_plan: object }` -> 204 (persiste `jobs.kill_plan`; solo lo usa el parse de Plan 1B, se añade aquí para cerrar el contrato de `CloudJobRepo.SetKillPlan`).
  - `POST /api/agent/jobs/:demoId/complete` -> 204 (marca `state` segun tipo).
  - `POST /api/agent/jobs/:demoId/fail` body `{ error: string }` -> 204.
- Mapeo `state` texto <-> `job.Status` entero (constantes del enum Go, ver `internal/job/job.go:15-31`):
  `queued=0`, `parsing=1`, `parsed=2`, `failed=8`, `scanning=9`, `scanned=10`.

- [ ] **Step 1: Write the DTO mapping + test**

`web/lib/cloud/jobDto.ts`:

```typescript
// Wire status ints mirror the Go job.Status enum (internal/job/job.go).
export const GO_STATUS = {
  queued: 0,
  parsing: 1,
  parsed: 2,
  failed: 8,
  scanning: 9,
  scanned: 10,
} as const;

export const STATE_FROM_GO: Record<number, string> = Object.fromEntries(
  Object.entries(GO_STATUS).map(([k, v]) => [v, k]),
);

export type JobRow = {
  demo_id: string;
  target_steamid: string;
  rules: unknown;
  demos: { storage_key: string; sha256: string };
};

export type JobDto = {
  id: string;
  status: number;
  demo_path: string;
  demo_sha256: string;
  target_steamid: string;
  rules: unknown;
};

export function toJobDto(row: JobRow): JobDto {
  return {
    id: row.demo_id,
    status: GO_STATUS.queued,
    demo_path: row.demos.storage_key,
    demo_sha256: row.demos.sha256,
    target_steamid: row.target_steamid,
    rules: row.rules ?? {},
  };
}
```

`web/lib/cloud/jobDto.test.mjs`:

```javascript
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { toJobDto, STATE_FROM_GO, GO_STATUS } from './jobDto.ts';

test('toJobDto uses demo_id as id and demo storage key as demo_path', () => {
  const dto = toJobDto({ demo_id: 'd1', target_steamid: '', rules: null, demos: { storage_key: 'demos/u/d1.dem', sha256: 'ab' } });
  assert.equal(dto.id, 'd1');
  assert.equal(dto.demo_path, 'demos/u/d1.dem');
  assert.equal(dto.demo_sha256, 'ab');
  assert.deepEqual(dto.rules, {});
});

test('status int/text mapping round-trips', () => {
  assert.equal(STATE_FROM_GO[GO_STATUS.scanned], 'scanned');
  assert.equal(STATE_FROM_GO[GO_STATUS.failed], 'failed');
});
```

Run: `cd web && node --test lib/cloud/jobDto.test.mjs` -> PASS.

- [ ] **Step 2: Write the claim route**

`web/app/api/agent/jobs/claim/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { toJobDto } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const db = supabaseAdmin();
  const { data: claimed, error } = await db.rpc('claim_next_job', { p_agent: agent.agentId, p_lease_seconds: 900 });
  if (error) return NextResponse.json({ error: 'claim failed' }, { status: 500 });
  if (!claimed) return new NextResponse(null, { status: 204 });

  const { data: row } = await db
    .from('jobs')
    .select('demo_id, target_steamid, rules, type, demos(storage_key, sha256)')
    .eq('id', claimed.id)
    .single();
  return NextResponse.json({ job: toJobDto(row), jobType: row.type });
}
```

- [ ] **Step 3: Write get/status/complete/fail routes**

`web/app/api/agent/jobs/[id]/route.ts` (GET by demo id, only the agent's active job):

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { toJobDto } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const { data: row } = await supabaseAdmin()
    .from('jobs')
    .select('demo_id, target_steamid, rules, type, demos(storage_key, sha256)')
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)')
    .maybeSingle();
  if (!row) return NextResponse.json({ error: 'not found' }, { status: 404 });
  return NextResponse.json(toJobDto(row));
}
```

`web/app/api/agent/jobs/[id]/status/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';
import { STATE_FROM_GO } from '@/lib/cloud/jobDto';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as { status?: number; failure_reason?: string };
  const state = STATE_FROM_GO[body.status ?? -1] ?? 'running';
  await supabaseAdmin()
    .from('jobs')
    .update({ state, error: body.failure_reason ?? '', lease_expires_at: new Date(Date.now() + 900_000).toISOString(), updated_at: new Date().toISOString() })
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)');
  return new NextResponse(null, { status: 204 });
}
```

`web/app/api/agent/jobs/[id]/killplan/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as { kill_plan?: unknown };
  await supabaseAdmin()
    .from('jobs')
    .update({ kill_plan: body.kill_plan ?? null, updated_at: new Date().toISOString() })
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)');
  return new NextResponse(null, { status: 204 });
}
```

`web/app/api/agent/jobs/[id]/complete/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const db = supabaseAdmin();
  const { data: job } = await db
    .from('jobs')
    .select('id, type')
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)')
    .maybeSingle();
  if (!job) return NextResponse.json({ error: 'not found' }, { status: 404 });
  const finalState = job.type === 'scan' ? 'scanned' : job.type === 'parse' ? 'parsed' : 'done';
  await db.from('jobs').update({ state: finalState, updated_at: new Date().toISOString() }).eq('id', job.id);
  return new NextResponse(null, { status: 204 });
}
```

`web/app/api/agent/jobs/[id]/fail/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { id } = await params;
  const body = (await request.json().catch(() => ({}))) as { error?: string };
  await supabaseAdmin()
    .from('jobs')
    .update({ state: 'failed', error: body.error ?? 'agent error', updated_at: new Date().toISOString() })
    .eq('demo_id', id)
    .eq('agent_id', agent.agentId)
    .not('state', 'in', '(done,failed)');
  return new NextResponse(null, { status: 204 });
}
```

- [ ] **Step 4: Typecheck**

Run: `cd web && npm run typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
committer "feat(cloud): agent job routes (claim/get/status/complete/fail)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/lib/cloud/jobDto.ts web/lib/cloud/jobDto.test.mjs web/app/api/agent/jobs/claim/route.ts "web/app/api/agent/jobs/[id]/route.ts" "web/app/api/agent/jobs/[id]/status/route.ts" "web/app/api/agent/jobs/[id]/killplan/route.ts" "web/app/api/agent/jobs/[id]/complete/route.ts" "web/app/api/agent/jobs/[id]/fail/route.ts"
```

---

## Task 9: Rutas de blobs firmados para el agente

**Files:**
- Create: `web/app/api/agent/blobs/sign-upload/route.ts`
- Create: `web/app/api/agent/blobs/download/route.ts`
- Create: `web/app/api/agent/blobs/exists/route.ts`

**Interfaces:**
- Consumes: `resolveAgent`, Supabase Storage (`createSignedUploadUrl`, `createSignedUrl`).
- Produces:
  - `POST /api/agent/blobs/sign-upload` body `{ key: string }` -> `{ url, token, bucket }` (bucket `artifacts`).
  - `GET /api/agent/blobs/download?key=...` -> `{ url }` (bucket `artifacts` para artefactos, `demos` para `.dem`, decidido por prefijo `demos/`).
  - `GET /api/agent/blobs/exists?key=...` -> `{ exists: boolean }`.
- Regla de bucket: claves que empiezan por `demos/` viven en el bucket `demos`; el resto en `artifacts`.

- [ ] **Step 1: Write a bucket helper inline and the three routes**

`web/app/api/agent/blobs/sign-upload/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

function bucketFor(key: string): { bucket: string; path: string } {
  return key.startsWith('demos/')
    ? { bucket: 'demos', path: key.slice('demos/'.length) }
    : { bucket: 'artifacts', path: key };
}

export async function POST(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const { key } = (await request.json()) as { key: string };
  const { bucket, path } = bucketFor(key);
  const { data, error } = await supabaseAdmin().storage.from(bucket).createSignedUploadUrl(path);
  if (error) return NextResponse.json({ error: error.message }, { status: 500 });
  return NextResponse.json({ url: data.signedUrl, token: data.token, bucket });
}
```

`web/app/api/agent/blobs/download/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

function bucketFor(key: string): { bucket: string; path: string } {
  return key.startsWith('demos/')
    ? { bucket: 'demos', path: key.slice('demos/'.length) }
    : { bucket: 'artifacts', path: key };
}

export async function GET(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const key = new URL(request.url).searchParams.get('key') ?? '';
  const { bucket, path } = bucketFor(key);
  const { data, error } = await supabaseAdmin().storage.from(bucket).createSignedUrl(path, 900);
  if (error) return NextResponse.json({ error: error.message }, { status: 500 });
  return NextResponse.json({ url: data.signedUrl });
}
```

`web/app/api/agent/blobs/exists/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { resolveAgent } from '@/lib/cloud/agentAuth';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(request: Request): Promise<Response> {
  const agent = await resolveAgent(request);
  if (!agent) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  const key = new URL(request.url).searchParams.get('key') ?? '';
  const isDemo = key.startsWith('demos/');
  const bucket = isDemo ? 'demos' : 'artifacts';
  const path = isDemo ? key.slice('demos/'.length) : key;
  const slash = path.lastIndexOf('/');
  const dir = slash >= 0 ? path.slice(0, slash) : '';
  const name = slash >= 0 ? path.slice(slash + 1) : path;
  const { data } = await supabaseAdmin().storage.from(bucket).list(dir, { search: name });
  return NextResponse.json({ exists: !!data?.some((f) => f.name === name) });
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && npm run typecheck`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
committer "feat(cloud): signed blob routes for the agent (upload/download/exists)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/app/api/agent/blobs/sign-upload/route.ts web/app/api/agent/blobs/download/route.ts web/app/api/agent/blobs/exists/route.ts
```

---

## Task 10: Adaptadores del agente (JobRepository + Storage) contra la nube

**Files:**
- Create: `internal/agent/jobrepo.go`
- Create: `internal/agent/storage.go`
- Test: `internal/agent/jobrepo_test.go`
- Test: `internal/agent/storage_test.go`

**Interfaces:**
- Consumes: `agent.Client`, `internal/job`, `internal/killplan`.
- Produces:
  - `NewCloudJobRepo(c *Client) *CloudJobRepo` que implementa `workers.JobRepository`: `GetMeta`, `UpdateStatus`, `SetKillPlan`.
  - `NewCloudStorage(c *Client) *CloudStorage` que implementa `storage.Storage`: `Put`, `Open`, `Exists`.

- [ ] **Step 1: Write CloudJobRepo + test**

`internal/agent/jobrepo.go`:

```go
package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
)

// CloudJobRepo implements workers.JobRepository against the cloud control-plane.
type CloudJobRepo struct {
	c *Client
}

func NewCloudJobRepo(c *Client) *CloudJobRepo { return &CloudJobRepo{c: c} }

func (r *CloudJobRepo) GetMeta(ctx context.Context, id uuid.UUID) (job.Job, error) {
	var j job.Job
	if _, err := r.c.Do(ctx, "GET", "/api/agent/jobs/"+id.String(), nil, &j); err != nil {
		return job.Job{}, err
	}
	return j, nil
}

func (r *CloudJobRepo) UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error {
	body := map[string]any{"status": int(s), "failure_reason": failureReason}
	_, err := r.c.Do(ctx, "POST", "/api/agent/jobs/"+id.String()+"/status", body, nil)
	return err
}

func (r *CloudJobRepo) SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error {
	_, err := r.c.Do(ctx, "POST", "/api/agent/jobs/"+id.String()+"/killplan", map[string]any{"kill_plan": plan}, nil)
	return err
}
```

`internal/agent/jobrepo_test.go`:

```go
package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rechedev9/fragforge/internal/job"
)

func TestCloudJobRepoUpdateStatus(t *testing.T) {
	id := uuid.New()
	var gotPath string
	var gotStatus float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var in map[string]any
		_ = decodeJSON(r, &in)
		gotStatus, _ = in["status"].(float64)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	repo := NewCloudJobRepo(NewClient(srv.URL, "tok"))
	if err := repo.UpdateStatus(context.Background(), id, job.StatusScanning, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if gotPath != "/api/agent/jobs/"+id.String()+"/status" {
		t.Errorf("got path %s", gotPath)
	}
	if int(gotStatus) != int(job.StatusScanning) {
		t.Errorf("got status %v, want %v", gotStatus, int(job.StatusScanning))
	}
}
```

Add a tiny `decodeJSON` test helper in one of the agent test files:

```go
func decodeJSON(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}
```

(import `encoding/json` and `net/http` in that helper file.)

- [ ] **Step 2: Write CloudStorage + test**

`internal/agent/storage.go`:

```go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// CloudStorage implements storage.Storage using signed URLs minted by the cloud.
type CloudStorage struct {
	c *Client
}

func NewCloudStorage(c *Client) *CloudStorage { return &CloudStorage{c: c} }

func (s *CloudStorage) Open(key string) (io.ReadCloser, error) {
	ctx := context.Background()
	var out struct {
		URL string `json:"url"`
	}
	if _, err := s.c.Do(ctx, "GET", "/api/agent/blobs/download?key="+url.QueryEscape(key), nil, &out); err != nil {
		return nil, err
	}
	resp, err := s.c.http.Get(out.URL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %d", key, resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *CloudStorage) Put(key string, r io.Reader) error {
	ctx := context.Background()
	var out struct {
		URL string `json:"url"`
	}
	if _, err := s.c.Do(ctx, "POST", "/api/agent/blobs/sign-upload", map[string]string{"key": key}, &out); err != nil {
		return err
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", out.URL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := s.c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upload %s: %d", key, resp.StatusCode)
	}
	return nil
}

func (s *CloudStorage) Exists(key string) (bool, error) {
	var out struct {
		Exists bool `json:"exists"`
	}
	if _, err := s.c.Do(context.Background(), "GET", "/api/agent/blobs/exists?key="+url.QueryEscape(key), nil, &out); err != nil {
		return false, err
	}
	return out.Exists, nil
}
```

`internal/agent/storage_test.go`:

```go
package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudStoragePut(t *testing.T) {
	var uploaded string
	blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		uploaded = string(b)
		w.WriteHeader(200)
	}))
	defer blob.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"` + blob.URL + `"}`))
	}))
	defer api.Close()

	s := NewCloudStorage(NewClient(api.URL, "tok"))
	if err := s.Put("jobs/x/roster.json", strings.NewReader(`{"players":[]}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if uploaded != `{"players":[]}` {
		t.Errorf("got upload %q", uploaded)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/agent -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
committer "feat(agent): cloud-backed JobRepository and Storage adapters

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" internal/agent/jobrepo.go internal/agent/jobrepo_test.go internal/agent/storage.go internal/agent/storage_test.go
```

---

## Task 11: Runner del agente - bucle claim + dispatch a ParserWorker

**Files:**
- Modify: `cmd/zv-agent/run.go`
- Create: `internal/agent/runner.go`
- Test: `internal/agent/runner_test.go`

**Interfaces:**
- Consumes: `CloudJobRepo`, `CloudStorage`, `workers.NewParserWorker`, `ProcessScanRoster`/`ProcessParseDemo`.
- Produces: `func Run(ctx context.Context, c *Client) error` que hace: heartbeat en background, luego un bucle que reclama jobs y los procesa. `type processor func(ctx, jobType string, demoID uuid.UUID) error` inyectable para test.

- [ ] **Step 1: Write the runner with an injectable processor + test**

`internal/agent/runner.go`:

```go
package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/workers"
)

type claimResponse struct {
	Job struct {
		ID string `json:"id"`
	} `json:"job"`
	JobType string `json:"jobType"`
}

// processFunc runs one claimed job. Swapped in tests.
type processFunc func(ctx context.Context, jobType string, demoID uuid.UUID) error

// Run starts the heartbeat and the claim loop until ctx is cancelled.
func Run(ctx context.Context, c *Client) error {
	repo := NewCloudJobRepo(c)
	var store storage.Storage = NewCloudStorage(c)
	pw := workers.NewParserWorker(repo, store)

	proc := func(ctx context.Context, jobType string, demoID uuid.UUID) error {
		switch jobType {
		case "scan":
			return pw.ProcessScanRoster(ctx, demoID)
		case "parse":
			return pw.ProcessParseDemo(ctx, demoID)
		default:
			return nil
		}
	}
	go HeartbeatLoop(ctx, c, map[string]any{"parser": true}, 20*time.Second)
	return loop(ctx, c, proc, 2*time.Second)
}

func loop(ctx context.Context, c *Client, proc processFunc, idle time.Duration) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var out claimResponse
		code, err := c.Do(ctx, "POST", "/api/agent/jobs/claim", nil, &out)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idle):
				continue
			}
		}
		if code == 204 || out.Job.ID == "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idle):
				continue
			}
		}
		demoID, perr := uuid.Parse(out.Job.ID)
		if perr != nil {
			continue
		}
		if err := proc(ctx, out.JobType, demoID); err != nil {
			_, _ = c.Do(ctx, "POST", "/api/agent/jobs/"+demoID.String()+"/fail", map[string]string{"error": err.Error()}, nil)
			continue
		}
		_, _ = c.Do(ctx, "POST", "/api/agent/jobs/"+demoID.String()+"/complete", nil, nil)
	}
}
```

`internal/agent/runner_test.go`:

```go
package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLoopClaimsProcessesCompletes(t *testing.T) {
	demoID := uuid.New()
	var served atomic.Bool
	var completed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agent/jobs/claim":
			if served.Swap(true) {
				w.WriteHeader(204)
				return
			}
			_, _ = w.Write([]byte(`{"job":{"id":"` + demoID.String() + `"},"jobType":"scan"}`))
		case r.URL.Path == "/api/agent/jobs/"+demoID.String()+"/complete":
			completed.Store(true)
			w.WriteHeader(204)
		default:
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var gotType string
	proc := func(ctx context.Context, jobType string, id uuid.UUID) error {
		gotType = jobType
		return nil
	}
	_ = loop(ctx, NewClient(srv.URL, "tok"), proc, 10*time.Millisecond)

	if gotType != "scan" {
		t.Errorf("got jobType %q, want scan", gotType)
	}
	if !completed.Load() {
		t.Errorf("job was not completed")
	}
}
```

- [ ] **Step 2: Wire run.go to Run**

Replace `cmd/zv-agent/run.go`:

```go
package main

import (
	"context"

	"github.com/rechedev9/fragforge/internal/agent"
)

func run(ctx context.Context, cfg Config) error {
	return agent.Run(ctx, agent.NewClient(cfg.BaseURL, cfg.Token))
}
```

- [ ] **Step 3: Run tests + build**

Run: `go test ./internal/agent -run TestLoop -count=1 && go build ./cmd/zv-agent`
Expected: PASS and clean build.

- [ ] **Step 4: Commit**

```bash
committer "feat(agent): claim loop dispatching scan/parse to ParserWorker

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" cmd/zv-agent/run.go internal/agent/runner.go internal/agent/runner_test.go
```

---

## Task 12: Ruta de subida - Supabase + creacion del scan job

**Files:**
- Modify: `web/app/api/demos/scan/route.ts`
- Create: `web/lib/cloud/demos.ts`
- Test: `web/lib/cloud/demos.test.mjs`

**Interfaces:**
- Consumes: session, `ensureUser`, `supabaseAdmin`.
- Produces:
  - `createScanJob(userId, file): Promise<{ demoId }>` que sube la `.dem` a `demos/{userId}/{demoId}.dem`, inserta `demos` y un `jobs` fila `type='scan', state='queued'`.
  - `POST /api/demos/scan` (FormData `file`) -> `201 { jobId }` (jobId = demoId), o `503 { code: 'service_unavailable' }` si Supabase no responde.
- Nota: se conserva el contrato de error `503 { code: 'service_unavailable' }` que la UI ya distingue.

- [ ] **Step 1: Write createScanJob + test (fake db/storage)**

`web/lib/cloud/demos.ts`:

```typescript
import type { SupabaseClient } from '@supabase/supabase-js';
import { supabaseAdmin } from '@/lib/supabase/server';

/** Upload the demo and queue a scan job. Returns the demo id (the browser handle). */
export async function createScanJob(
  userId: string,
  file: { name: string; size: number; bytes: ArrayBuffer },
  db: SupabaseClient = supabaseAdmin(),
): Promise<{ demoId: string }> {
  const demoId = crypto.randomUUID();
  const key = `demos/${userId}/${demoId}.dem`;
  // Direct service-role upload into the private demos bucket.
  const put = await db.storage.from('demos').upload(`${userId}/${demoId}.dem`, file.bytes, {
    contentType: 'application/octet-stream',
    upsert: false,
  });
  if (put.error) throw new Error(`upload: ${put.error.message}`);

  const demo = await db
    .from('demos')
    .insert({ id: demoId, user_id: userId, storage_key: key, filename: file.name, size: file.size });
  if (demo.error) throw new Error(`insert demo: ${demo.error.message}`);

  const job = await db.from('jobs').insert({ demo_id: demoId, user_id: userId, type: 'scan', state: 'queued' });
  if (job.error) throw new Error(`insert job: ${job.error.message}`);
  return { demoId };
}
```

`web/lib/cloud/demos.test.mjs`:

```javascript
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createScanJob } from './demos.ts';

// Each insert is awaited directly (no .select), so the builder is a thenable
// that resolves to { error }. The fake mirrors exactly that shape.
function fakeDb(captured) {
  return {
    storage: { from() { return { async upload(p) { captured.upload = p; return { error: null }; } }; } },
    from(table) {
      return {
        insert(row) {
          captured[table] = row;
          return { then: (resolve) => resolve({ error: null }) };
        },
      };
    },
  };
}

test('createScanJob uploads, inserts demo and a queued scan job', async () => {
  const captured = {};
  const { demoId } = await createScanJob('u1', { name: 'm.dem', size: 10, bytes: new ArrayBuffer(10) }, fakeDb(captured));
  assert.match(demoId, /[0-9a-f-]{36}/);
  assert.equal(captured.jobs.type, 'scan');
  assert.equal(captured.jobs.state, 'queued');
  assert.equal(captured.demos.user_id, 'u1');
  assert.equal(captured.demos.id, demoId);
});
```

- [ ] **Step 2: Rewrite the scan route**

`web/app/api/demos/scan/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { cookies } from 'next/headers';
import { verifySession, SESSION_COOKIE } from '@/lib/auth/session';
import { ensureUser } from '@/lib/cloud/users';
import { createScanJob } from '@/lib/cloud/demos';
import { SERVICE_UNAVAILABLE_CODE } from '@/lib/api/types';

export const runtime = 'nodejs';

export async function POST(request: Request): Promise<Response> {
  const jar = await cookies();
  const s = verifySession(jar.get(SESSION_COOKIE)?.value);
  if (!s) return NextResponse.json({ error: 'unauthorized' }, { status: 401 });

  const form = await request.formData();
  const file = form.get('file');
  if (!(file instanceof File)) return NextResponse.json({ error: 'missing file' }, { status: 400 });

  try {
    const userId = await ensureUser(s.steamid64, s.persona, s.avatar);
    const bytes = await file.arrayBuffer();
    const { demoId } = await createScanJob(userId, { name: file.name, size: file.size, bytes });
    return NextResponse.json({ jobId: demoId }, { status: 201 });
  } catch (err) {
    console.error('scan create failed', err);
    return NextResponse.json({ error: 'analysis service unavailable', code: SERVICE_UNAVAILABLE_CODE }, { status: 503 });
  }
}
```

- [ ] **Step 3: Run test + typecheck**

Run: `cd web && node --test lib/cloud/demos.test.mjs && npm run typecheck`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
committer "feat(cloud): scan upload route stores demo in Supabase and queues a scan job

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" web/app/api/demos/scan/route.ts web/lib/cloud/demos.ts web/lib/cloud/demos.test.mjs
```

---

## Task 13: Rutas de estado y roster + cableado async de la UI

**Files:**
- Modify: `web/app/api/demos/[jobId]/status/route.ts`
- Modify: `web/app/api/demos/[jobId]/roster/route.ts`
- Modify: `web/lib/api/real.ts`
- Modify: `web/app/upload/page.tsx`
- Test: `web/e2e/upload-cloud.spec.ts`

**Interfaces:**
- Consumes: `supabaseAdmin`, `RosterKey` layout (`jobs/{demoId}/roster.json`), `STATE_FROM_GO`.
- Produces:
  - `GET /api/demos/:demoId/status` -> `{ status: 'queued'|'scanning'|'scanned'|'failed'|..., failure_reason?: string, online: boolean }`.
  - `GET /api/demos/:demoId/roster` -> `{ players: RosterPlayer[] }` leyendo `artifacts/jobs/{demoId}/roster.json`.
  - `RealApiClient.scanDemo(file)` sube, hace polling de status hasta `scanned` (o lanza si `failed` / timeout con PC offline), luego pide roster.
  - Upload page muestra estados: `scanning` ("analizando en tu PC"), `waiting-for-pc` ("tu PC esta desconectado, abre FragForge Agent"), `failed`.

- [ ] **Step 1: Rewrite status route**

`web/app/api/demos/[jobId]/status/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(_req: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const db = supabaseAdmin();
  const { data: job } = await db
    .from('jobs')
    .select('state, error, user_id')
    .eq('demo_id', jobId)
    .order('created_at', { ascending: false })
    .limit(1)
    .maybeSingle();
  if (!job) return NextResponse.json({ status: 'unknown' }, { status: 404 });

  const { data: agents } = await db.from('agents').select('last_heartbeat_at').eq('user_id', job.user_id).not('name', 'like', 'pending:%');
  const online = (agents ?? []).some((a) => a.last_heartbeat_at && Date.now() - new Date(a.last_heartbeat_at).getTime() < 60_000);
  return NextResponse.json({ status: job.state, failure_reason: job.error || undefined, online });
}
```

- [ ] **Step 2: Rewrite roster route**

`web/app/api/demos/[jobId]/roster/route.ts`:

```typescript
import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase/server';

export const runtime = 'nodejs';

export async function GET(_req: Request, { params }: { params: Promise<{ jobId: string }> }): Promise<Response> {
  const { jobId } = await params;
  const path = `jobs/${jobId}/roster.json`;
  const { data, error } = await supabaseAdmin().storage.from('artifacts').download(path);
  if (error || !data) return NextResponse.json({ players: [] });
  const json = JSON.parse(await data.text()) as { players: unknown[] };
  return NextResponse.json({ players: json.players ?? [] });
}
```

- [ ] **Step 3: Make scanDemo async in the real client**

En `web/lib/api/real.ts`, reemplazar `scanDemo` por una versión que sube, hace polling de `status`, y al llegar a `scanned` pide el roster. Añadir el estado `waiting-for-pc` propagando un error tipado cuando `online === false` durante > 8s:

```typescript
async scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[] }> {
  const form = new FormData();
  form.append('file', file);
  const res = await fetch(`${this.base}/api/demos/scan`, { method: 'POST', body: form });
  if (!res.ok) throw await this.asError(res);
  const { jobId } = (await res.json()) as { jobId: string };

  const deadline = Date.now() + 5 * 60_000;
  let sawOffline = 0;
  for (;;) {
    await sleep(1500);
    const st = await fetch(`${this.base}/api/demos/${jobId}/status`).then((r) => r.json());
    if (st.status === 'scanned') break;
    if (st.status === 'failed') throw new Error(st.failure_reason || 'scan failed');
    if (st.online === false && ++sawOffline > 4) throw new Error('PC_OFFLINE');
    if (Date.now() > deadline) throw new Error('scan timed out');
  }
  const roster = await fetch(`${this.base}/api/demos/${jobId}/roster`).then((r) => r.json());
  return { jobId, players: mapRoster(roster.players) };
}
```

Reusar el `mapRoster`/mapping ya existente en `real.ts` (el que hoy convierte `RosterPlayer[]` a `DemoPlayer[]`); si está inline, extraerlo a una función `mapRoster`. Añadir un `sleep` helper local si no existe.

- [ ] **Step 4: Surface the offline state in the upload page**

En `web/app/upload/page.tsx`, en el `catch` de `onFile`, distinguir `PC_OFFLINE`:

```typescript
} catch (err) {
  if (err instanceof Error && err.message === 'PC_OFFLINE') {
    setStage('waiting-for-pc');
  } else {
    setStage('error');
  }
}
```

Añadir el render del estado `waiting-for-pc` con copy: "Tu PC está desconectado. Abre FragForge Agent en tu ordenador para analizar la demo." y un botón "Reintentar" que reejecuta `onFile` con el mismo fichero.

- [ ] **Step 5: Write a Playwright spec for scanning + offline**

`web/e2e/upload-cloud.spec.ts`: mockear la red (como en los specs de error existentes) para tres casos:
1. status devuelve `scanning` dos veces y luego `scanned`, roster con 2 jugadores -> aparece el PlayerPicker con 2 jugadores.
2. status devuelve `online: false` repetido -> aparece el copy "Tu PC está desconectado".
3. status devuelve `failed` con `failure_reason` -> aparece el estado de error.

Seguir el patrón de `web/e2e/*.spec.ts` que ya mockea `page.route('**/api/demos/**', ...)`.

- [ ] **Step 6: Run typecheck + e2e**

Run: `cd web && npm run typecheck && npm run test:e2e -- upload-cloud`
Expected: PASS (los specs mockean la red; no necesitan orquestador ni Supabase).

- [ ] **Step 7: Commit**

```bash
committer "feat(cloud): async scan status/roster routes and PC-offline upload UX

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" "web/app/api/demos/[jobId]/status/route.ts" "web/app/api/demos/[jobId]/roster/route.ts" web/lib/api/real.ts web/app/upload/page.tsx web/e2e/upload-cloud.spec.ts
```

---

## Verificacion final de la Fase 1

- [ ] **Go gate:** `scripts/go-gate.sh` -> fmt, vet, build, tests en verde (incluye `cmd/zv-agent` y `internal/agent`).
- [ ] **Web:** `cd web && npm run typecheck && npm run test:e2e` -> verde.
- [ ] **Prueba manual del bucle (opcional, requiere Supabase real + un PC):**
  1. `zv-agent --pair <code>` con el código de `/connect`.
  2. Arrancar el agente (sin flags); `/connect` muestra el PC online.
  3. Subir una `.dem` en `/upload`; el agente la reclama, la escanea, y aparece el roster.

## Alcance explicito: que NO entra en la Fase 1

- Parse con target + momentos (feeds del selector) -> Plan 1B.
- Captura (HLAE+CS2) + composición + entrega del reel -> Plan 2.
- Selector multi-jugada y pack de publicación -> Plan 3.
- Endurecimiento de leases/reaper, subidas resumables (TUS), y Realtime en la UI (aquí se usa polling; Realtime es una mejora de Fase 3).
