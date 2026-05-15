# zackvideo

Pipeline for generating CS2 highlight clips from `.dem` files. See [`docs/README.md`](./docs/README.md) for design docs.

## Status

- ✅ `zv-parser` CLI: demo → kill plan JSON
- ✅ `zv-recorder` local HLAE/CS2 recording CLI
- ✅ `zv-composer` local concat composer
- ✅ `zv-pipeline` local recorder → composer runner
- ✅ `zv-orchestrator` HTTP API + Asynq parser/media workers
- ⏳ Effects, music sync, overlays, cleanup policy, frontend

## Quick start (local development)

Requires Go 1.26+, Docker, and Make.

```bash
# 1. Bring up Postgres + Redis
make up
# wait ~10s for healthchecks
make migrate-up  # requires ZV_DATABASE_URL exported, see below

# 2. Set env
export ZV_DATABASE_URL="postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable"
export ZV_REDIS_ADDR="localhost:6379"
export ZV_DATA_DIR="./data"
# Optional:
# export ZV_HTTP_ADDR=":8080"
# export ZV_WORKER_CONCURRENCY="2"
# export ZV_RECORDER_PATH="/path/to/zv-recorder"
# export ZV_HLAE_PATH="/path/to/HLAE.exe"
# export ZV_CS2_PATH="/path/to/cs2.exe"
# export ZV_COMPOSER_PATH="/path/to/zv-composer"
# export ZV_FFMPEG_PATH="/path/to/ffmpeg"

# 3. Build binaries
make build

# 4. Run the orchestrator
./bin/zv-orchestrator
```

In another terminal:

```bash
# Smoke-test end-to-end (requires a .dem in testdata/)
./scripts/smoke.sh testdata/<your-demo>.dem <SteamID64>
```

## API

| Method | Path                       | Description                                |
|--------|----------------------------|--------------------------------------------|
| POST   | `/api/jobs`                | Multipart upload: `demo` file + `config` JSON (`{"target_steamid":"..."}`). Returns `201 {id, status}`. |
| GET    | `/api/jobs/{id}`           | Job metadata and status.                   |
| GET    | `/api/jobs/{id}/plan`      | Kill plan JSON (200) or 409 if not ready.  |
| POST   | `/api/jobs/{id}/record`    | Enqueue recording after parse approval.    |
| POST   | `/api/jobs/{id}/compose`   | Enqueue final composition after recording. |
| GET    | `/api/jobs/{id}/final`     | Stream the composed MP4 when ready.         |

## Standalone CLI

`zv-parser` parses a demo without the orchestrator:

```bash
./bin/zv-parser parse \
  --demo testdata/foo.dem \
  --steamid 76561198000000000 \
  --rules testdata/example.rules.json \
  --out plan.json --verbose
```

## Tests

```bash
make test
# Repository and worker integration tests skip if Postgres / TEST_DEMO_PATH is unavailable.
```

See [`docs/specs/`](./docs/specs/) for the specs and plans that produced this code.
