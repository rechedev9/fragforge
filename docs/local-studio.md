# FragForge Local Studio

Local Studio lets an end user run the whole product from the web UI on their own
Windows + GPU PC, including local HLAE + CS2 capture.

It is the "local mode" counterpart to the hosted cloud deployment.
There is no Supabase and no paired desktop agent: the web UI proxies the entire
pipeline to a local orchestrator (`zv serve`) on the same machine, which parses
the demo, records with HLAE/CS2, and renders the Short.

The browser flow is exactly the one the product is built around:
upload a demo -> scan the roster -> pick a player -> pick specific kills ->
create the reel, and at that moment HLAE + CS2 open to capture the frames,
then the edit is applied.

## Why a separate mode

Capture needs Windows + a GPU + CS2 + HLAE, which cannot run in a Linux
container or in a hosted cloud runtime.
So the product ships two data planes selected by one flag,
`NEXT_PUBLIC_FRAGFORGE_MODE`:

- `cloud` (default): uploads and scan go to Supabase, and a paired desktop agent
  on the user's PC does the capture.
  This is the hosted control-plane.
- `local`: the web talks only to a local orchestrator on the same machine.
  Everything (scan, parse, record, render) runs on the user's PC.

The two modes differ only in the data plane.
In local mode the `/api/demos/*` routes proxy scan/status/roster to the local
orchestrator (`scan` -> `POST /api/jobs`, `status` -> `GET /api/jobs/{id}`,
`roster` -> `GET /api/jobs/{id}/roster`), and the rest of the pipeline (parse,
plan, record, renders, capabilities) already proxies to the same orchestrator.
Because a single orchestrator job UUID flows through the whole pipeline, the
record button captures with the same job that scan created.

## Run it

Prerequisites:

- Build the binaries once: `.\scripts\build.ps1` (produces `.\bin\zv.exe`).
- Node.js + npm (the launcher runs `npm install` on first run).
- CS2 + HLAE installed.
  Use `C:\HLAE-2.190.1\HLAE.exe`; the orchestrator auto-detects HLAE, CS2 (via
  Steam's `libraryfolders.vdf`), and `zv-recorder` next to the binary, so you do
  not set any tool-path env vars.

One command:

```powershell
.\scripts\local-studio.ps1
```

It starts the orchestrator in memory mode (in-memory jobs + inline queue, no
Postgres/Redis), waits for `GET /healthz`, starts the web UI in local mode, and
opens `http://localhost:3000/upload`.
Ctrl+C stops the web UI and then the orchestrator.

## What the flag switches

`NEXT_PUBLIC_FRAGFORGE_MODE=local` is read identically on the client and inside
the Next.js server route handlers (it is a `NEXT_PUBLIC_` var):

- Client (`web/lib/api/index.ts`): selects the real API client (the one that
  drives the orchestrator) instead of the in-memory mock.
- Client (`web/lib/api/real.ts`): `scanDemo` waits for the orchestrator status
  to reach `scanned` (synchronous, on this machine) instead of the cloud's
  agent-handoff wait that also watches whether the user's PC is online.
- Server (`web/app/api/demos/{scan,[jobId]/status,[jobId]/roster}/route.ts`):
  proxy to the local orchestrator (see `web/app/api/demos/_local.ts`) instead of
  Supabase.

The launcher also sets `ORCHESTRATOR_URL=http://127.0.0.1:8080` for the Next.js
server so the proxy reaches the orchestrator over loopback.
A loopback bind needs no token.

## If capture is not configured

If HLAE or CS2 is missing, the app still runs the analyze flow
(upload -> roster -> scoreboard -> pick player -> match/highlights), and the
sidebar "Capture" card reads what is missing (it reflects the orchestrator's
real `/api/capabilities`, not a mock).
Creating a reel then surfaces a clear "recording is not configured" failure
instead of silently doing nothing.
