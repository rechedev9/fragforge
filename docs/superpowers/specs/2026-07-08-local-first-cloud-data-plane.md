# Local-first cloud data plane

Date: 2026-07-08.
Status: approved for implementation.
Supersedes the data-plane sections (§7 storage, §9 signed URLs) of `2026-07-01-fragforge-cloud-design.md`.

## Invariant

The hosted web is control plane only: identity, pairing, agent liveness.
Every byte of media (`.dem`, segments, rendered MP4) stays on the user's PC and is processed there.
Verifiable form: no control-plane route accepts or serves media bytes, and no Supabase Storage bucket exists.

## Architecture

```text
Browser (hosted web, e.g. https://fragforge.<vps-domain>)
  |- control plane (HTTPS -> Next.js on the VPS + Supabase): Steam session, pairing, heartbeat/liveness
  |- data plane (fetch -> http://127.0.0.1:<port>): upload .dem, scan, roster, parse, plan, record, render
Agent (zv-agent, user's PC)
  |- supervises a child zv-orchestrator (local pipeline, sqlite job repo)
  |- loopback auth proxy in front of it: Bearer token + CORS allowlist + PNA preflight
```

The browser and the agent run on the same machine, so the data plane is a loopback hop.
Cloud mode's data plane becomes the same orchestrator API that local mode already proxies to; the modes differ only in transport (client-side direct with a token vs same-origin server-side proxy).

## Component contract

### zv-agent (Go)

- The agent spawns and supervises `zv-orchestrator` as a child process bound to `127.0.0.1:0` (dynamic port), `ZV_DATABASE_URL=sqlite` (persistent local job history), inline queue.
- In front of it the agent serves a reverse proxy on `FRAGFORGE_LOOPBACK_ADDR` (default `127.0.0.1:8090`); non-loopback binds are rejected at startup.
- Auth: every request except CORS preflight requires `Authorization: Bearer <loopback_token>`; constant-time compare; 401 otherwise.
- The token is 32 random bytes (crypto/rand, base64url), generated at pair time, persisted in `agent.json` (0600) next to the cloud token.
- CORS: `Access-Control-Allow-Origin` echoes the request origin only when it is in the allowlist (`FRAGFORGE_WEB_ORIGIN`, comma-separated, default `https://app.fragforge.gg`); `Vary: Origin`; allow methods `GET, POST, DELETE`, allow headers `Authorization, Content-Type`.
- Private Network Access: when a preflight carries `Access-Control-Request-Private-Network: true`, respond `Access-Control-Allow-Private-Network: true`.
- Removed entirely: the cloud claim loop (`internal/agent/runner.go` job claiming), `CloudJobRepo`, `CloudStorage`, and the `/api/agent/blobs/*` client calls.
- Kept: pairing (`POST /api/agent/pair`) and heartbeat (`POST /api/agent/heartbeat`), both extended below.

### Control plane HTTP contract (web server routes, consumed by the Go agent)

- `POST /api/agent/pair` request body gains `loopbackToken` (string, required) and `loopbackPort` (int, required); stored on the `agents` row.
- `POST /api/agent/heartbeat` body gains the same two fields; the row is updated on every heartbeat so a re-paired or re-configured agent propagates.
- `GET /api/pc/status` (session-gated, browser-facing) returns `{ paired, online, loopback: { port, token } | null }`; `loopback` is non-null only when an agent is paired.

### Supabase

- Tables kept: `users`, `agents`.
- `agents` gains `loopback_token text not null default ''` and `loopback_port integer not null default 8090`.
- Dropped: `demos` and `jobs` tables, the `claim_next_job` RPC, and the `demos` and `artifacts` Storage buckets.
- Job history lives in the agent's local sqlite; cross-device history is explicitly out of scope for this phase.

### Web client (cloud mode)

- After login/pairing the client reads `/api/pc/status`; with loopback info it talks straight to `http://127.0.0.1:<port>` with the Bearer token for the entire data plane: `POST /api/jobs` (multipart `demo` upload = scan), `GET /api/jobs/{id}`, `/roster`, `/parse`, `/plan`, record and render routes, `GET /api/capabilities`, `GET /healthz`.
- Offline semantics: `PC_OFFLINE` when the loopback probe (`GET /healthz`) fails; the control-plane heartbeat distinguishes "PC off" from "agent not running" in the message.
- Removed: the Supabase upload branch of `/api/demos/scan`, `web/lib/cloud/demos.ts`, `web/lib/cloud/blobAuth.ts`, all `/api/agent/blobs/*` routes, and the cloud branches of `/api/demos/[jobId]/status` and `/roster`.
- The `ORCHESTRATOR_URL` server-side proxy remains only for local mode (`isLocalMode()`), unchanged.
- The claim "EL .DEM NUNCA SALE DE TU PC" becomes true in both modes and stays.

### Deployment

- The hosted web (control plane) deploys on the user's Hetzner VPS (gr-prod) via Docker Compose behind the existing Basic Auth middleware (`FRAGFORGE_WEB_PASSWORD`), `NEXT_PUBLIC_FRAGFORGE_MODE=cloud`, Supabase env for users/agents.
- No orchestrator runs on the VPS; the VPS never sees media.

## Browser policy risk

Chrome's Private Network Access / Local Network Access rules gate public-HTTPS to loopback fetches.
The agent implements the PNA preflight from day one; if a browser still refuses, the web detects the failed probe and offers the fallback: open the agent-served UI directly at `http://localhost:<port>` (local mode UX) via deep link.
A manual cross-browser spike (Chrome, Edge, Safari) validates this before the feature is announced; the code paths are testable without it.
