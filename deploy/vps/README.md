# FragForge Cloud - VPS deploy (gr-prod)

Runs the FragForge Cloud web app, and only the web app, on the Hetzner VPS in cloud mode.
No orchestrator runs here.
The VPS is control plane only: identity, pairing, agent liveness.
Every byte of media (`.dem`, segments, rendered MP4) stays on the paired agent's PC.
See `docs/superpowers/specs/2026-07-08-local-first-cloud-data-plane.md` for the full contract.

## Prerequisites

- Docker and the Docker Compose plugin installed on the VPS.
- A Supabase project with `web/supabase/migrations/0001_cloud_schema.sql` and `0002_local_first_data_plane.sql` applied, in that order.
- The VPS's existing reverse proxy (or Tailscale) already routes a hostname to `127.0.0.1:3000`.
  This compose file does not add nginx or any other proxy; it only binds the container to loopback.

## Setup

1. Copy the env template and fill in real values:

   ```bash
   cd deploy/vps
   cp .env.example .env
   ```

   `.env` needs:

   - `FRAGFORGE_WEB_PASSWORD` - HTTP Basic Auth password gating every route (see `web/middleware.ts`).
   - `ZV_SESSION_SECRET` - signs the Steam login session cookie.
   - `STEAM_WEB_API_KEY` - Steam Web API key for match-history linking.
   - `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY` - the Supabase project holding `users` and `agents` only.

   These mirror `web/.env.example`; that file documents the same variables for local development.

2. Build and start:

   ```bash
   docker compose up -d --build
   ```

   This builds `web/Dockerfile` with build arg `FRAGFORGE_MODE=cloud` (bakes `NEXT_PUBLIC_FRAGFORGE_MODE=cloud` into the client bundle) and runs it bound to `127.0.0.1:3000`, so it is reachable only through the VPS's own reverse proxy or Tailscale, never directly on the public IP.

3. Check it is up:

   ```bash
   curl -u anyuser:$FRAGFORGE_WEB_PASSWORD http://127.0.0.1:3000/
   docker compose logs -f web
   ```

## How the gaming PC pairs against it

1. On the hosted web, log in with Steam and open the pairing screen; it shows a short-lived pairing code.
2. On the Windows gaming PC, run the local agent and pair it against the hosted URL, e.g. `zv-agent --pair <code> --cloud-url https://fragforge.<your-vps-domain>`.
3. The agent stores its pairing token and a random loopback token in `agent.json` (0600), starts its local orchestrator on a dynamic port, and serves a loopback reverse proxy (default `127.0.0.1:8090`) in front of it.
4. On every heartbeat the agent reports its loopback port and token to the control plane (`agents.loopback_port`, `agents.loopback_token`).
5. The browser (on the same PC as the agent) reads `/api/pc/status`, then talks directly to `http://127.0.0.1:<port>` with the Bearer token for the entire data plane: upload `.dem`, scan, roster, parse, plan, record, render.
   The `.dem` and every rendered artifact never leave the PC; the VPS only ever sees control-plane calls (pairing, heartbeat, session).

## Updating

```bash
git pull
docker compose up -d --build
```

## Stopping

```bash
docker compose down
```
