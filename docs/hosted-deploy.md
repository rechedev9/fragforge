# Hosted web deploy (Topology A) on gr-prod

This is the server-side half of hosted mode.
It hosts ONLY the thin FragForge web frontend: the SPA plus a small SQLite accounts DB (Steam login).
No orchestrator, no Postgres, no Redis, no jobs, and no video ever run here.

The browser on the end-user PC loads the SPA from our HTTPS domain and then talks DIRECTLY to a LOCAL agent at `http://127.0.0.1:8787`, sending `X-FragForge-Token` on every request.
Job traffic and video bytes never transit this server.
The end-user Agent half is documented separately in [hosted-agent.md](hosted-agent.md).

> Flipping production is a MANUAL, confirmed step.
> Nothing in this doc runs automatically.
> The commands below start a NEW container published on loopback only; they do not touch the running orchestrator, other Docker stacks, or the Tailscale config.
> Run them only after an explicit go-ahead.

## What gets deployed

- One Docker service (`web`) from [`docker-compose.hosted.yml`](../docker-compose.hosted.yml), built from `web/Dockerfile`.
- Client bundle baked with `NEXT_PUBLIC_FRAGFORGE_MODE=hosted` (build arg) so the SPA uses the direct-to-local-agent API client.
- Published on `127.0.0.1:3000` only, to be fronted by `tailscale serve`.
- A named volume `accountsdata` holding `/data/fragforge.db` (the SQLite accounts DB + its WAL sidecars).

## 1. Configure the environment

Create an `.env` file next to the compose file (it is gitignored).
Set at least a strong session secret and the served origin.

```bash
cd /opt/fragforge   # or the worktree while testing

cat > .env <<'EOF'
# Signs the ff_session cookie. REQUIRED. Use a strong random value.
ZV_SESSION_SECRET=<paste a long random string>

# Exact HTTPS origin this deployment is served at (see step 3). This is the
# origin the end-user Agent must allowlist via ZV_ALLOWED_WEB_ORIGINS on their
# PC for CORS + Private Network Access to succeed.
ZV_ALLOWED_WEB_ORIGINS=https://fragforge.gr-prod.taila10698.ts.net

# Optional: real Steam match-history linking. Free key at
# https://steamcommunity.com/dev/apikey
STEAM_WEB_API_KEY=

# Optional: gate the whole UI behind HTTP Basic Auth. Empty leaves it open.
FRAGFORGE_WEB_PASSWORD=
EOF
```

Generate a session secret with:

```bash
openssl rand -base64 48
```

## 2. Build and run (loopback only)

```bash
cd /opt/fragforge
docker compose -f docker-compose.hosted.yml up --build -d
```

This publishes the web UI on `127.0.0.1:3000` only.
Confirm it is up (still loopback, not public):

```bash
curl -fsS http://127.0.0.1:3000/ >/dev/null && echo WEB_OK
docker compose -f docker-compose.hosted.yml ps
docker compose -f docker-compose.hosted.yml logs --tail=50 web
```

Validate the compose file without starting anything:

```bash
docker compose -f docker-compose.hosted.yml config >/dev/null && echo COMPOSE_OK
```

## 3. Expose via `tailscale serve`

Front the loopback port with Tailscale so users reach it over HTTPS at the tailnet domain.
This is a change to the Tailscale config, so treat it as production and only run it after explicit confirmation.

```bash
# Serve the loopback web UI at the HTTPS tailnet origin.
tailscale serve --bg --https=443 --set-path / http://127.0.0.1:3000
tailscale serve status
```

The resulting origin must EXACTLY match `ZV_ALLOWED_WEB_ORIGINS` from step 1:

```
https://fragforge.gr-prod.taila10698.ts.net
```

If you serve at the bare host origin (`https://gr-prod.taila10698.ts.net`) instead of a `fragforge.` subdomain, set `ZV_ALLOWED_WEB_ORIGINS` to that exact value and rebuild so the browser and the agent agree on the origin.
A mismatch means the local agent sends no CORS headers and the browser blocks every call.

## 4. Point users at their Agent

Once the SPA is reachable:

1. The user installs and runs the FragForge Agent on their PC (see [hosted-agent.md](hosted-agent.md)).
2. The Agent prints its pairing token and binds `127.0.0.1:8787`.
3. On the user PC the Agent must have `ZV_ALLOWED_WEB_ORIGINS` set to this deployment's origin so CORS + Private Network Access preflight succeeds.
4. In the web UI's "Agent connection" panel the user confirms the URL `http://127.0.0.1:8787` and pastes the pairing token.

## 5. Back up the SQLite accounts DB

The accounts DB is the only stateful thing on this server.
It lives in the named volume `accountsdata` at `/data/fragforge.db` (with `-wal` / `-shm` sidecars because WAL is enabled).

Back it up online with SQLite's own backup so you capture a consistent snapshot including the WAL:

```bash
# Consistent hot backup into ./backups on the host.
mkdir -p /opt/fragforge/backups
docker compose -f docker-compose.hosted.yml exec web \
  node -e "const {DatabaseSync}=require('node:sqlite');\
const db=new DatabaseSync(process.env.FRAGFORGE_SQLITE_PATH);\
db.exec(\"VACUUM INTO '/data/backup-'+Date.now()+'.db'\");db.close();"

# Copy the snapshot out of the container/volume.
cid=$(docker compose -f docker-compose.hosted.yml ps -q web)
docker cp "$cid:/data/." /opt/fragforge/backups/
```

Alternatively, back up the whole volume when the service is stopped:

```bash
docker compose -f docker-compose.hosted.yml down
docker run --rm -v fragforge_accountsdata:/data -v /opt/fragforge/backups:/backup \
  alpine tar czf /backup/accountsdata-$(date +%F).tar.gz -C /data .
```

Adjust the volume name to match `docker volume ls` (Compose prefixes it with the project name).

## 6. Stop / roll back

```bash
docker compose -f docker-compose.hosted.yml down          # keeps the DB volume
docker compose -f docker-compose.hosted.yml down -v       # ALSO deletes the DB volume (destructive)
```

To stop exposing the site, remove the Tailscale serve mapping (confirmed, production):

```bash
tailscale serve --https=443 off
```

## Notes

- This stack never runs the orchestrator or any capture/render code; those live on the end-user PC (the Agent).
- Do not publish port 3000 beyond loopback; all public access goes through `tailscale serve`.
- Rebuild (`up --build`) after changing `ZV_ALLOWED_WEB_ORIGINS` or the served origin, since the mode and origin expectations feed the client bundle.
