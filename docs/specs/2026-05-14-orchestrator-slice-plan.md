# Orchestrator Slice — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go orchestrator (`zv-orchestrator`) that accepts CS2 demos by HTTP upload, queues a parsing job via Asynq, runs the existing `parser.Run` in a worker, persists the resulting kill plan in Postgres, and exposes it via REST.

**Architecture:** Single Go binary that runs an HTTP API and an Asynq worker side-by-side. Demos are stored on local disk (`./data/demos/<job-id>.dem`); the kill plan is stored as JSONB in Postgres. Redis backs the Asynq queue. Local dev brought up via `docker-compose`.

**Tech Stack:** Go 1.26 · chi v5 (HTTP router) · pgx/v5 (Postgres driver) · hibiken/asynq (Redis-backed job queue) · golang-migrate (CLI, run once per dev) · existing `internal/parser` for demo logic.

## Current media-flow addendum

The orchestrator now covers the media stages as well as parsing:

- Status enum values are `queued`, `parsing`, `parsed`, `recording`,
  `recorded`, `composing`, `composed`, `done`, and `failed`.
- `001_jobs.up.sql` is the fresh schema. `002_job_status_media.up.sql` exists
  for databases migrated before media statuses were added; its down migration is
  a documented no-op because PostgreSQL enum values are not safely reversible.
- `scripts/smoke-real.ps1` is the Windows smoke for API -> parse -> record ->
  compose -> download with real workers. It checks required media env vars,
  starts Postgres/Redis through Docker Compose when available, retries manual
  `record` and `compose` calls to exercise idempotent skips, and downloads the
  final MP4.
- Media workdirs are deleted after each task when `ZV_MEDIA_WORK_DIR` is unset.
  Set `ZV_MEDIA_WORK_DIR` to preserve them for debugging.
- Durable artifacts are `recording-result.json`, `recording.js`,
  `segments/*.mp4`, `composition-result.json`, and `final.mp4` under
  `jobs/{id}/...`.

---

## Slice scope

**In:**
- `POST /api/jobs` — multipart upload (.dem + JSON config) → returns `{id, status: "queued"}`.
- `GET /api/jobs/{id}` — returns job status and metadata.
- `GET /api/jobs/{id}/plan` — returns the kill plan JSON when status=`done`.
- Asynq worker that processes `parse:demo` tasks, calls `parser.Run`, persists the plan.
- Postgres table `jobs` with status + `kill_plan` JSONB column.
- Local filesystem storage abstraction (interface-based so S3/MinIO can plug in later).

**Out:**
- Frontend.
- Auth / multi-tenant.
- Real object storage (S3/MinIO).
- WebSocket progress events (status polling only in this slice).
- Recording driver, composer, mixer, encoder — separate future slices.

Design references: [`../architecture/01-components.md`](../architecture/01-components.md), [`../architecture/02-data-flow.md`](../architecture/02-data-flow.md).

---

## File structure

```
zackvideo/
├── cmd/
│   ├── zv-parser/                 ← existing (no changes)
│   └── zv-orchestrator/           ← NEW
│       ├── main.go                ← wires API + worker, signal handling
│       └── config.go              ← env-based config
├── internal/
│   ├── parser/, killplan/, rules/ ← existing
│   ├── job/                       ← NEW · job domain types and repository
│   │   ├── job.go
│   │   ├── job_test.go
│   │   ├── repository.go
│   │   └── repository_test.go
│   ├── storage/                   ← NEW · local filesystem demo storage
│   │   ├── storage.go             (interface + Local impl)
│   │   └── storage_test.go
│   ├── tasks/                     ← NEW · Asynq task payload definitions
│   │   ├── tasks.go
│   │   └── tasks_test.go
│   ├── httpapi/                   ← NEW · HTTP handlers
│   │   ├── handlers.go
│   │   ├── handlers_test.go
│   │   └── routes.go
│   └── workers/                   ← NEW · Asynq worker handlers
│       ├── parser_worker.go
│       └── parser_worker_test.go
├── migrations/                    ← NEW
│   ├── 001_jobs.up.sql
│   └── 001_jobs.down.sql
├── docker-compose.yml             ← NEW (postgres + redis for dev)
└── Makefile                       ← NEW (build / test / db helpers)
```

Each unit has one clear responsibility:

- `job` — domain types + Postgres CRUD.
- `storage` — bytes in/out of a backing store.
- `tasks` — Asynq task type + payload (shared producer/consumer contract).
- `httpapi` — HTTP routing and request/response shaping.
- `workers` — task handlers; depends on `parser`, `storage`, `job`.
- `cmd/zv-orchestrator` — bootstrap and lifecycle.

---

## Task 1: Project setup — dependencies, Makefile, docker-compose

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `docker-compose.yml`, `Makefile`, `.env.example`

- [ ] **Step 1: Add Go dependencies**

Run:
```bash
go get github.com/go-chi/chi/v5@v5.1.0
go get github.com/jackc/pgx/v5@v5.7.1
go get github.com/jackc/pgx/v5/pgxpool
go get github.com/hibiken/asynq@v0.25.1
go get github.com/google/uuid@v1.6.0
go mod tidy
```

- [ ] **Step 2: Create `docker-compose.yml`**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: zackvideo
      POSTGRES_PASSWORD: zackvideo
      POSTGRES_DB: zackvideo
    ports: ["5432:5432"]
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zackvideo"]
      interval: 5s
      retries: 5
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      retries: 5
volumes:
  pgdata:
```

- [ ] **Step 3: Create `.env.example`**

```bash
ZV_HTTP_ADDR=:8080
ZV_DATABASE_URL=postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable
ZV_REDIS_ADDR=localhost:6379
ZV_DATA_DIR=./data
ZV_WORKER_CONCURRENCY=2
```

- [ ] **Step 4: Create `Makefile`**

```makefile
.PHONY: build test up down migrate-up migrate-down fmt vet

build:
	go build -o bin/zv-parser ./cmd/zv-parser
	go build -o bin/zv-orchestrator ./cmd/zv-orchestrator

test:
	go test ./... -count=1

up:
	docker compose up -d

down:
	docker compose down

migrate-up:
	@psql "$$ZV_DATABASE_URL" -f migrations/001_jobs.up.sql

migrate-down:
	@psql "$$ZV_DATABASE_URL" -f migrations/001_jobs.down.sql

fmt:
	gofmt -w .

vet:
	go vet ./...
```

- [ ] **Step 5: Run `make up` and verify**

Run: `make up && docker compose ps`
Expected: postgres and redis show `healthy`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum docker-compose.yml Makefile .env.example
git commit -m "feat(orchestrator): add dependencies, docker-compose, and Makefile"
```

---

## Task 2: Database migration — `jobs` table

**Files:**
- Create: `migrations/001_jobs.up.sql`, `migrations/001_jobs.down.sql`

- [ ] **Step 1: Write the migration**

`migrations/001_jobs.up.sql`:
```sql
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
```

`migrations/001_jobs.down.sql`:
```sql
DROP TABLE IF EXISTS jobs;
DROP TYPE IF EXISTS job_status;
```

- [ ] **Step 2: Apply the migration**

Run: `make migrate-up`
Expected: no errors.

- [ ] **Step 3: Verify the schema**

Run: `psql "$ZV_DATABASE_URL" -c "\d jobs"`
Expected: table with the columns above.

- [ ] **Step 4: Commit**

```bash
git add migrations/
git commit -m "feat(orchestrator): add jobs table migration"
```

---

## Task 3: `internal/job` — job types + JSON serialization

**Files:**
- Create: `internal/job/job.go`, `internal/job/job_test.go`

- [ ] **Step 1: Write the failing test**

`internal/job/job_test.go`:
```go
package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStatusStringMapping(t *testing.T) {
	cases := map[Status]string{
		StatusQueued:  "queued",
		StatusParsing: "parsing",
		StatusParsed:  "parsed",
		StatusFailed:  "failed",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestParseStatusValid(t *testing.T) {
	s, err := ParseStatus("parsed")
	if err != nil {
		t.Fatalf("ParseStatus(parsed) error = %v", err)
	}
	if s != StatusParsed {
		t.Errorf("ParseStatus(parsed) = %v, want %v", s, StatusParsed)
	}
}

func TestParseStatusInvalid(t *testing.T) {
	if _, err := ParseStatus("bogus"); err == nil {
		t.Error("ParseStatus(bogus) error = nil, want error")
	}
}

func TestJobMarshalsToExpectedShape(t *testing.T) {
	j := Job{
		ID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Status:        StatusQueued,
		DemoPath:      "/tmp/x.dem",
		DemoSHA256:    "abc",
		TargetSteamID: "76561198000000000",
		CreatedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	out := string(b)
	if !contains(out, `"status":"queued"`) {
		t.Errorf("status not rendered as string: %s", out)
	}
	if !contains(out, `"id":"11111111-1111-1111-1111-111111111111"`) {
		t.Errorf("id not rendered as UUID string: %s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/job/...`
Expected: FAIL with "undefined: Status / Job / ..."

- [ ] **Step 3: Write minimal implementation**

`internal/job/job.go`:
```go
// Package job defines the orchestrator's core domain type and helpers.
package job

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

// Status is the lifecycle state of a Job.
type Status int

const (
	StatusQueued Status = iota
	StatusParsing
	StatusParsed
	StatusFailed
)

var statusNames = [...]string{"queued", "parsing", "parsed", "failed"}

// String returns the canonical lowercase representation used in JSON and DB.
func (s Status) String() string {
	if int(s) < 0 || int(s) >= len(statusNames) {
		return "unknown"
	}
	return statusNames[s]
}

// MarshalJSON renders Status as the canonical string.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON parses a JSON string into a Status.
func (s *Status) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseStatus(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ParseStatus converts a canonical name into a Status.
func ParseStatus(name string) (Status, error) {
	for i, n := range statusNames {
		if n == name {
			return Status(i), nil
		}
	}
	return 0, fmt.Errorf("unknown job status %q", name)
}

// Job is the canonical domain model used by the API, the worker, and the DB.
type Job struct {
	ID            uuid.UUID      `json:"id"`
	Status        Status         `json:"status"`
	FailureReason string         `json:"failure_reason,omitempty"`
	DemoPath      string         `json:"demo_path"`
	DemoSHA256    string         `json:"demo_sha256"`
	TargetSteamID string         `json:"target_steamid"`
	Rules         rules.Rules    `json:"rules"`
	KillPlan      *killplan.Plan `json:"kill_plan,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/job/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/job/
git commit -m "feat(job): add Status enum and Job domain type"
```

---

## Task 4: `internal/job` — Postgres repository

**Files:**
- Create: `internal/job/repository.go`, `internal/job/repository_test.go`

- [ ] **Step 1: Write the failing test**

`internal/job/repository_test.go`:
```go
package job

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("no Postgres available: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "TRUNCATE jobs"); err != nil {
		t.Skipf("Postgres reachable but schema not migrated: %v (run `make migrate-up`)", err)
	}
	return pool
}

func TestRepositoryCreateAndGet(t *testing.T) {
	pool := testPool(t)
	repo := NewRepository(pool)

	want := Job{
		ID:            uuid.New(),
		Status:        StatusQueued,
		DemoPath:      "/tmp/demo.dem",
		DemoSHA256:    "abc123",
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	if err := repo.Create(context.Background(), &want); err != nil {
		t.Fatalf("Create error = %v", err)
	}

	got, err := repo.Get(context.Background(), want.ID)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Status != StatusQueued {
		t.Errorf("Status = %v, want %v", got.Status, StatusQueued)
	}
	if got.DemoSHA256 != "abc123" {
		t.Errorf("DemoSHA256 = %q, want %q", got.DemoSHA256, "abc123")
	}
}

func TestRepositoryGetMissingReturnsNotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewRepository(pool)

	_, err := repo.Get(context.Background(), uuid.New())
	if err != ErrNotFound {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
}

func TestRepositoryUpdateStatusPersists(t *testing.T) {
	pool := testPool(t)
	repo := NewRepository(pool)

	j := Job{
		ID:            uuid.New(),
		Status:        StatusQueued,
		DemoPath:      "/tmp/demo.dem",
		DemoSHA256:    "abc",
		TargetSteamID: "1",
		Rules:         rules.Default(),
	}
	_ = repo.Create(context.Background(), &j)

	if err := repo.UpdateStatus(context.Background(), j.ID, StatusParsing, ""); err != nil {
		t.Fatalf("UpdateStatus error = %v", err)
	}
	got, _ := repo.Get(context.Background(), j.ID)
	if got.Status != StatusParsing {
		t.Errorf("Status = %v, want StatusParsing", got.Status)
	}
}

func TestRepositorySetKillPlanPersists(t *testing.T) {
	pool := testPool(t)
	repo := NewRepository(pool)

	j := Job{
		ID:            uuid.New(),
		Status:        StatusParsing,
		DemoPath:      "/tmp/demo.dem",
		DemoSHA256:    "abc",
		TargetSteamID: "1",
		Rules:         rules.Default(),
	}
	_ = repo.Create(context.Background(), &j)

	plan := killplan.NewPlan()
	plan.Demo.Map = "de_nuke"
	if err := repo.SetKillPlan(context.Background(), j.ID, plan); err != nil {
		t.Fatalf("SetKillPlan error = %v", err)
	}
	got, _ := repo.Get(context.Background(), j.ID)
	if got.KillPlan == nil {
		t.Fatalf("KillPlan = nil, want set")
	}
	if got.KillPlan.Demo.Map != "de_nuke" {
		t.Errorf("KillPlan.Demo.Map = %q, want de_nuke", got.KillPlan.Demo.Map)
	}

	// Sanity check that JSONB round-trips via DB
	b, _ := json.Marshal(got.KillPlan)
	if !contains(string(b), "de_nuke") {
		t.Errorf("marshaled plan does not contain map name: %s", string(b))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make up && make migrate-up && go test ./internal/job/...`
Expected: FAIL with "undefined: NewRepository / ErrNotFound / ..."

- [ ] **Step 3: Write minimal implementation**

`internal/job/repository.go`:
```go
package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reche/zackvideo/internal/killplan"
)

// ErrNotFound is returned by Get when no job has the requested id.
var ErrNotFound = errors.New("job not found")

// Repository persists Jobs in Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository wraps a pgx pool for job persistence.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts the job. Sets ID if zero, plus CreatedAt/UpdatedAt.
func (r *Repository) Create(ctx context.Context, j *Job) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now

	rulesJSON, err := json.Marshal(j.Rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO jobs (id, status, demo_path, demo_sha256, target_steamid, rules, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		j.ID, j.Status.String(), j.DemoPath, j.DemoSHA256, j.TargetSteamID, rulesJSON, j.CreatedAt, j.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

// Get returns the job with the given id or ErrNotFound.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Job, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), demo_path, demo_sha256,
		        target_steamid, rules, kill_plan, created_at, updated_at
		 FROM jobs WHERE id = $1`, id,
	)
	var j Job
	var statusStr string
	var rulesJSON []byte
	var planJSON []byte
	err := row.Scan(&j.ID, &statusStr, &j.FailureReason, &j.DemoPath, &j.DemoSHA256,
		&j.TargetSteamID, &rulesJSON, &planJSON, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}

	j.Status, err = ParseStatus(statusStr)
	if err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(rulesJSON, &j.Rules); err != nil {
		return Job{}, fmt.Errorf("unmarshal rules: %w", err)
	}
	if len(planJSON) > 0 {
		var p killplan.Plan
		if err := json.Unmarshal(planJSON, &p); err != nil {
			return Job{}, fmt.Errorf("unmarshal kill_plan: %w", err)
		}
		j.KillPlan = &p
	}
	return j, nil
}

// UpdateStatus moves the job to a new status. failureReason is set when status=failed.
func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status, failureReason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = $2, failure_reason = NULLIF($3,''), updated_at = NOW()
		 WHERE id = $1`,
		id, status.String(), failureReason,
	)
	return err
}

// SetKillPlan persists the kill plan JSONB.
func (r *Repository) SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error {
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE jobs SET kill_plan = $2, updated_at = NOW() WHERE id = $1`,
		id, planJSON,
	)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/job/...`
Expected: PASS (skipped automatically if Postgres unreachable).

- [ ] **Step 5: Commit**

```bash
git add internal/job/
git commit -m "feat(job): add Postgres repository with Create/Get/UpdateStatus/SetKillPlan"
```

---

## Task 5: `internal/storage` — local filesystem storage

**Files:**
- Create: `internal/storage/storage.go`, `internal/storage/storage_test.go`

- [ ] **Step 1: Write the failing test**

`internal/storage/storage_test.go`:
```go
package storage

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestLocalPutAndOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}

	want := []byte("demo bytes")
	if err := store.Put("demos/abc.dem", bytes.NewReader(want)); err != nil {
		t.Fatalf("Put error = %v", err)
	}

	rc, err := store.Open("demos/abc.dem")
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, want) {
		t.Errorf("Open returned %q, want %q", got, want)
	}
}

func TestLocalOpenMissingReturnsErrNotExist(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	_, err := store.Open("nope.dem")
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected file-not-found error, got %v", err)
	}
}

func TestLocalRejectsEscapingKeys(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	err := store.Put("../escaped.dem", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Error("expected error rejecting key with ..")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/...`
Expected: FAIL with "undefined: NewLocal".

- [ ] **Step 3: Write minimal implementation**

`internal/storage/storage.go`:
```go
// Package storage abstracts where the orchestrator keeps demo files and
// other artifacts. V1 ships a Local filesystem implementation; future
// slices can add an S3/MinIO backend behind the same interface.
package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Storage is the minimal contract for reading and writing artifact blobs.
type Storage interface {
	Put(key string, r io.Reader) error
	Open(key string) (io.ReadCloser, error)
}

// Local implements Storage backed by the local filesystem under a root dir.
type Local struct {
	root string
}

// NewLocal returns a Local rooted at the given absolute or relative path.
// The root directory is created if it does not exist.
func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Local{root: abs}, nil
}

// Put writes r's contents to the file at key inside the storage root.
func (l *Local) Put(key string, r io.Reader) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// Open returns a ReadCloser for the file at key.
func (l *Local) Open(key string) (io.ReadCloser, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (l *Local) resolve(key string) (string, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	abs := filepath.Join(l.root, filepath.FromSlash(key))
	if !strings.HasPrefix(abs, l.root+string(os.PathSeparator)) && abs != l.root {
		return "", errors.New("storage: key escapes root")
	}
	return abs, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/
git commit -m "feat(storage): add Local filesystem storage with path safety"
```

---

## Task 6: `internal/tasks` — Asynq task type for parsing

**Files:**
- Create: `internal/tasks/tasks.go`, `internal/tasks/tasks_test.go`

- [ ] **Step 1: Write the failing test**

`internal/tasks/tasks_test.go`:
```go
package tasks

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestNewParseDemoTaskRoundtrip(t *testing.T) {
	id := uuid.New()
	tk, err := NewParseDemoTask(id)
	if err != nil {
		t.Fatalf("NewParseDemoTask error = %v", err)
	}
	if tk.Type() != TypeParseDemo {
		t.Errorf("Type() = %q, want %q", tk.Type(), TypeParseDemo)
	}

	var payload ParseDemoPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.JobID != id {
		t.Errorf("JobID = %v, want %v", payload.JobID, id)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tasks/...`
Expected: FAIL with "undefined: NewParseDemoTask".

- [ ] **Step 3: Write minimal implementation**

`internal/tasks/tasks.go`:
```go
// Package tasks defines the Asynq task types and payloads shared between
// the orchestrator (producer) and the workers (consumer).
package tasks

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// TypeParseDemo is the Asynq task type for parsing a demo into a kill plan.
const TypeParseDemo = "parse:demo"

// ParseDemoPayload carries the inputs the worker needs to fetch from the DB.
type ParseDemoPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// NewParseDemoTask returns an Asynq task that, when consumed, processes the
// job identified by id.
func NewParseDemoTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(ParseDemoPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeParseDemo, payload), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tasks/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tasks/
git commit -m "feat(tasks): add ParseDemo task type and payload"
```

---

## Task 7: `internal/httpapi` — POST /api/jobs

**Files:**
- Create: `internal/httpapi/handlers.go`, `internal/httpapi/routes.go`, `internal/httpapi/handlers_test.go`

- [ ] **Step 1: Write the failing test**

`internal/httpapi/handlers_test.go`:
```go
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

// fakeRepo implements JobRepository for tests.
type fakeRepo struct {
	jobs map[uuid.UUID]job.Job
}

func newFakeRepo() *fakeRepo { return &fakeRepo{jobs: map[uuid.UUID]job.Job{}} }
func (f *fakeRepo) Create(_ context.Context, j *job.Job) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	f.jobs[j.ID] = *j
	return nil
}
func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (job.Job, error) {
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return j, nil
}

// fakeStorage records every Put call.
type fakeStorage struct {
	puts map[string][]byte
}

func newFakeStorage() *fakeStorage { return &fakeStorage{puts: map[string][]byte{}} }
func (f *fakeStorage) Put(key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.puts[key] = b
	return nil
}
func (f *fakeStorage) Open(key string) (io.ReadCloser, error) {
	b, ok := f.puts[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// fakeQueue captures enqueued tasks.
type fakeQueue struct {
	enqueued []*asynq.Task
}

func (q *fakeQueue) Enqueue(t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.enqueued = append(q.enqueued, t)
	return &asynq.TaskInfo{ID: "x"}, nil
}

func multipartBody(t *testing.T, demoBytes []byte, configJSON string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	demoPart, _ := mw.CreateFormFile("demo", "test.dem")
	demoPart.Write(demoBytes)
	mw.WriteField("config", configJSON)
	mw.Close()
	return body, mw.FormDataContentType()
}

func TestPostJobsCreatesJobAndEnqueues(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	h := NewHandlers(repo, store, queue)

	body, ct := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &resp)
	if resp.Status != "queued" {
		t.Errorf("status = %q, want queued", resp.Status)
	}
	if len(repo.jobs) != 1 {
		t.Errorf("repo has %d jobs, want 1", len(repo.jobs))
	}
	if len(store.puts) != 1 {
		t.Errorf("storage has %d puts, want 1", len(store.puts))
	}
	if len(queue.enqueued) != 1 {
		t.Errorf("queue has %d tasks, want 1", len(queue.enqueued))
	}
}

func TestPostJobsRejectsMissingDemo(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	mw.WriteField("config", `{"target_steamid":"76561198000000000"}`)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestPostJobsRejectsInvalidSteamID(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	body, ct := multipartBody(t, []byte("x"), `{"target_steamid":"not-a-number"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

// keep imports needed by later tasks
var _ = killplan.SchemaVersion
var _ = rules.Default
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/httpapi/...`
Expected: FAIL with "undefined: NewHandlers / JobRepository / ..."

- [ ] **Step 3: Write minimal implementation**

`internal/httpapi/handlers.go`:
```go
// Package httpapi exposes the orchestrator's HTTP endpoints.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
)

const maxDemoBytes = 500 << 20 // 500 MiB

// JobRepository is the subset of *job.Repository used by handlers.
type JobRepository interface {
	Create(ctx context.Context, j *job.Job) error
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
}

// Enqueuer is the subset of *asynq.Client used by handlers.
type Enqueuer interface {
	Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

// Handlers bundles the dependencies needed by every endpoint.
type Handlers struct {
	repo    JobRepository
	storage storage.Storage
	queue   Enqueuer
}

// NewHandlers constructs an HTTP handler set.
func NewHandlers(repo JobRepository, store storage.Storage, queue Enqueuer) *Handlers {
	return &Handlers{repo: repo, storage: store, queue: queue}
}

// CreateJob handles POST /api/jobs.
type createJobConfig struct {
	TargetSteamID string       `json:"target_steamid"`
	Rules         *rules.Rules `json:"rules,omitempty"`
}

func (h *Handlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxDemoBytes); err != nil {
		writeError(w, http.StatusBadRequest, "parsing multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("demo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing demo file: "+err.Error())
		return
	}
	defer file.Close()

	var cfg createJobConfig
	if raw := r.FormValue("config"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid config JSON: "+err.Error())
			return
		}
	}
	if cfg.TargetSteamID == "" {
		writeError(w, http.StatusBadRequest, "target_steamid is required")
		return
	}
	if _, err := strconv.ParseUint(cfg.TargetSteamID, 10, 64); err != nil {
		writeError(w, http.StatusBadRequest, "target_steamid must be a 64-bit unsigned integer")
		return
	}

	effectiveRules := rules.Default()
	if cfg.Rules != nil {
		effectiveRules = *cfg.Rules
		if err := effectiveRules.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid rules: "+err.Error())
			return
		}
	}

	// Buffer the upload so we can both hash and store it. For 500MiB worst
	// case this fits in memory; if it ever doesn't, replace with a tempfile.
	buf, err := io.ReadAll(io.LimitReader(file, maxDemoBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "reading demo: "+err.Error())
		return
	}
	if int64(len(buf)) > maxDemoBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "demo exceeds size limit")
		return
	}

	sum := sha256.Sum256(buf)
	sha := hex.EncodeToString(sum[:])

	j := &job.Job{
		ID:            uuid.New(),
		Status:        job.StatusQueued,
		DemoSHA256:    sha,
		TargetSteamID: cfg.TargetSteamID,
		Rules:         effectiveRules,
	}
	key := fmt.Sprintf("demos/%s.dem", j.ID)
	j.DemoPath = key

	if err := h.storage.Put(key, bytesReader(buf)); err != nil {
		writeError(w, http.StatusInternalServerError, "storing demo: "+err.Error())
		return
	}
	if err := h.repo.Create(r.Context(), j); err != nil {
		writeError(w, http.StatusInternalServerError, "creating job: "+err.Error())
		return
	}

	task, err := tasks.NewParseDemoTask(j.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building task: "+err.Error())
		return
	}
	if _, err := h.queue.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueueing task: "+err.Error())
		return
	}

	_ = header
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     j.ID,
		"status": j.Status,
	})
}

// helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func bytesReader(b []byte) io.Reader { return errReader{r: newBytesReader(b)} }

// newBytesReader / errReader: tiny indirection so we can avoid pulling in
// bytes.NewReader directly here for symmetry with future streaming impls.
type byteReadCloser struct{ b []byte; off int }

func newBytesReader(b []byte) *byteReadCloser { return &byteReadCloser{b: b} }
func (r *byteReadCloser) Read(p []byte) (int, error) {
	if r.off >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.off:])
	r.off += n
	return n, nil
}

type errReader struct{ r io.Reader; err error }

func (e errReader) Read(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	return e.r.Read(p)
}

// ensure unused imports compile out
var _ = errors.New
```

`internal/httpapi/routes.go`:
```go
package httpapi

import "github.com/go-chi/chi/v5"

// Routes returns a chi router with all orchestrator routes wired.
func Routes(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Post("/api/jobs", h.CreateJob)
	return r
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/httpapi/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/
git commit -m "feat(httpapi): add POST /api/jobs with multipart upload + Asynq enqueue"
```

---

## Task 8: `internal/httpapi` — GET /api/jobs/{id}

**Files:**
- Modify: `internal/httpapi/handlers.go`, `internal/httpapi/routes.go`, `internal/httpapi/handlers_test.go`

- [ ] **Step 1: Add the failing test**

Append to `internal/httpapi/handlers_test.go`:
```go
func TestGetJobReturnsJob(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{
		ID:            uuid.New(),
		Status:        job.StatusQueued,
		DemoPath:      "demos/x.dem",
		DemoSHA256:    "abc",
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	repo.jobs[j.ID] = j

	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if got.ID != j.ID.String() {
		t.Errorf("id = %q, want %q", got.ID, j.ID.String())
	}
	if got.Status != "queued" {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

func TestGetJobReturns404WhenMissing(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+uuid.New().String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

func TestGetJobReturns400OnInvalidUUID(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/not-a-uuid", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}
```

Also add to imports at the top of the test file:
```go
import (
	"github.com/go-chi/chi/v5"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/httpapi/...`
Expected: FAIL with "undefined: h.GetJob".

- [ ] **Step 3: Implement GetJob**

Append to `internal/httpapi/handlers.go`:
```go
import "github.com/go-chi/chi/v5"  // add to existing imports if not present

// GetJob handles GET /api/jobs/{id}.
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	j, err := h.repo.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, j)
}
```

Update `routes.go`:
```go
func Routes(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Post("/api/jobs", h.CreateJob)
	r.Get("/api/jobs/{id}", h.GetJob)
	return r
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/httpapi/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/
git commit -m "feat(httpapi): add GET /api/jobs/{id}"
```

---

## Task 9: `internal/httpapi` — GET /api/jobs/{id}/plan

**Files:**
- Modify: `internal/httpapi/handlers.go`, `internal/httpapi/routes.go`, `internal/httpapi/handlers_test.go`

- [ ] **Step 1: Add the failing test**

Append to `handlers_test.go`:
```go
func TestGetPlanReturns409WhenJobNotParsed(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusQueued, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/plan", h.GetPlan)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/plan", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (not yet ready)", rw.Code)
	}
}

func TestGetPlanReturnsPlanWhenReady(t *testing.T) {
	repo := newFakeRepo()
	plan := killplan.NewPlan()
	plan.Demo.Map = "de_inferno"
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/plan", h.GetPlan)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/plan", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	if !contains(rw.Body.String(), "de_inferno") {
		t.Errorf("body does not include map: %s", rw.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/httpapi/...`
Expected: FAIL with "undefined: h.GetPlan".

- [ ] **Step 3: Implement GetPlan**

Append to `handlers.go`:
```go
// GetPlan handles GET /api/jobs/{id}/plan.
func (h *Handlers) GetPlan(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	j, err := h.repo.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job not ready (status=%s)", j.Status))
		return
	}
	writeJSON(w, http.StatusOK, j.KillPlan)
}
```

Update `routes.go`:
```go
r.Get("/api/jobs/{id}/plan", h.GetPlan)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/httpapi/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/
git commit -m "feat(httpapi): add GET /api/jobs/{id}/plan"
```

---

## Task 10: `internal/workers` — Asynq parser worker

**Files:**
- Create: `internal/workers/parser_worker.go`, `internal/workers/parser_worker_test.go`

- [ ] **Step 1: Write the failing test**

`internal/workers/parser_worker_test.go`:
```go
package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/tasks"
)

// in-memory fakes -------------------------------------------------------

type fakeRepo struct {
	jobs map[uuid.UUID]*job.Job
}

func newFakeRepo() *fakeRepo { return &fakeRepo{jobs: map[uuid.UUID]*job.Job{}} }
func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (job.Job, error) {
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return *j, nil
}
func (f *fakeRepo) UpdateStatus(_ context.Context, id uuid.UUID, s job.Status, reason string) error {
	j := f.jobs[id]
	j.Status = s
	j.FailureReason = reason
	return nil
}
func (f *fakeRepo) SetKillPlan(_ context.Context, id uuid.UUID, p killplan.Plan) error {
	f.jobs[id].KillPlan = &p
	return nil
}

type fakeStorage struct{ files map[string][]byte }

func newFakeStorage() *fakeStorage { return &fakeStorage{files: map[string][]byte{}} }
func (f *fakeStorage) Put(key string, r io.Reader) error {
	b, _ := io.ReadAll(r)
	f.files[key] = b
	return nil
}
func (f *fakeStorage) Open(key string) (io.ReadCloser, error) {
	b, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// real demo helper ------------------------------------------------------

func loadRealDemo(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("TEST_DEMO_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "testdata", "lavked-vs-tnc-m2-nuke.dem")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no test demo at %s: %v", path, err)
	}
	return b
}

func TestParserWorkerRunsAgainstRealDemo(t *testing.T) {
	demo := loadRealDemo(t)
	repo := newFakeRepo()
	store := newFakeStorage()

	id := uuid.New()
	repo.jobs[id] = &job.Job{
		ID:            id,
		Status:        job.StatusQueued,
		DemoPath:      "demos/test.dem",
		DemoSHA256:    "fake",
		TargetSteamID: "76561198148986856", // maaryy
		Rules:         rules.Default(),
	}
	_ = store.Put("demos/test.dem", bytes.NewReader(demo))

	w := NewParserWorker(repo, store)

	payload, _ := json.Marshal(tasks.ParseDemoPayload{JobID: id})
	if err := w.HandleParseDemo(context.Background(), asynq.NewTask(tasks.TypeParseDemo, payload)); err != nil {
		t.Fatalf("HandleParseDemo error = %v", err)
	}

	got := repo.jobs[id]
	if got.Status != job.StatusParsed {
		t.Errorf("Status = %v, want StatusParsed", got.Status)
	}
	if got.KillPlan == nil {
		t.Fatal("KillPlan = nil after successful parse")
	}
	if got.KillPlan.Stats.SegmentsCreated == 0 {
		t.Error("SegmentsCreated = 0, expected > 0 (parser regression)")
	}
}

func TestParserWorkerMarksJobFailedOnUnknownTarget(t *testing.T) {
	demo := loadRealDemo(t)
	repo := newFakeRepo()
	store := newFakeStorage()

	id := uuid.New()
	repo.jobs[id] = &job.Job{
		ID:            id,
		Status:        job.StatusQueued,
		DemoPath:      "demos/test.dem",
		TargetSteamID: "1", // not in demo
		Rules:         rules.Default(),
	}
	_ = store.Put("demos/test.dem", bytes.NewReader(demo))

	w := NewParserWorker(repo, store)
	payload, _ := json.Marshal(tasks.ParseDemoPayload{JobID: id})
	err := w.HandleParseDemo(context.Background(), asynq.NewTask(tasks.TypeParseDemo, payload))
	if err == nil {
		t.Fatal("HandleParseDemo error = nil, want non-nil so Asynq won't retry forever")
	}

	got := repo.jobs[id]
	if got.Status != job.StatusFailed {
		t.Errorf("Status = %v, want StatusFailed", got.Status)
	}
	if got.FailureReason == "" {
		t.Errorf("FailureReason empty, want a message")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workers/...`
Expected: FAIL with "undefined: NewParserWorker".

- [ ] **Step 3: Write minimal implementation**

`internal/workers/parser_worker.go`:
```go
// Package workers implements the Asynq task handlers that drive the
// orchestrator's pipeline. Each worker is a thin glue layer that pulls
// a job from the repository, delegates the heavy lifting to a domain
// package (parser, composer, ...), and writes the result back.
package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"

	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/parser"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
)

// JobRepository is the subset of *job.Repository the worker needs.
type JobRepository interface {
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
	SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error
}

// ParserWorker handles the "parse:demo" Asynq task.
type ParserWorker struct {
	repo    JobRepository
	storage storage.Storage
}

// NewParserWorker returns a worker that processes parse:demo tasks.
func NewParserWorker(repo JobRepository, store storage.Storage) *ParserWorker {
	return &ParserWorker{repo: repo, storage: store}
}

// HandleParseDemo is the Asynq handler signature.
func (w *ParserWorker) HandleParseDemo(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ParseDemoPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}

	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusParsing, ""); err != nil {
		return fmt.Errorf("mark parsing: %w", err)
	}

	plan, parseErr := w.parse(ctx, j)
	if parseErr != nil {
		_ = w.repo.UpdateStatus(ctx, j.ID, job.StatusFailed, parseErr.Error())
		return parseErr
	}

	if err := w.repo.SetKillPlan(ctx, j.ID, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusParsed, ""); err != nil {
		return fmt.Errorf("mark parsed: %w", err)
	}
	return nil
}

func (w *ParserWorker) parse(_ context.Context, j job.Job) (killplan.Plan, error) {
	rc, err := w.storage.Open(j.DemoPath)
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("open demo: %w", err)
	}
	defer rc.Close()

	// demoinfocs needs an io.ReadSeeker for CS2 demos; copy to a temp file
	// to give it one without buffering the whole demo in memory.
	tmp, err := os.CreateTemp("", "zv-demo-*.dem")
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, rc); err != nil {
		tmp.Close()
		return killplan.Plan{}, fmt.Errorf("write temp demo: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return killplan.Plan{}, err
	}

	p := demoinfocs.NewParser(tmp)
	defer p.Close()
	defer tmp.Close()

	meta := parser.PlanMeta{
		DemoPath: j.DemoPath,
		SHA256:   j.DemoSHA256,
	}
	plan, err := parser.Run(p, j.TargetSteamID, j.Rules, meta)
	if err != nil {
		if errors.Is(err, parser.ErrTargetNotFound) {
			return killplan.Plan{}, fmt.Errorf("target steamid %s not found in demo", j.TargetSteamID)
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/workers/...`
Expected: PASS (skips when no demo available).

- [ ] **Step 5: Commit**

```bash
git add internal/workers/
git commit -m "feat(workers): add Asynq parser worker"
```

---

## Task 11: `cmd/zv-orchestrator` — wire the binary

**Files:**
- Create: `cmd/zv-orchestrator/main.go`, `cmd/zv-orchestrator/config.go`

- [ ] **Step 1: Create the config loader**

`cmd/zv-orchestrator/config.go`:
```go
package main

import (
	"fmt"
	"os"
)

type config struct {
	HTTPAddr           string
	DatabaseURL        string
	RedisAddr          string
	DataDir            string
	WorkerConcurrency  int
}

func loadConfig() (config, error) {
	c := config{
		HTTPAddr:          envOr("ZV_HTTP_ADDR", ":8080"),
		DatabaseURL:       os.Getenv("ZV_DATABASE_URL"),
		RedisAddr:         envOr("ZV_REDIS_ADDR", "localhost:6379"),
		DataDir:           envOr("ZV_DATA_DIR", "./data"),
		WorkerConcurrency: 2,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 2: Create the entrypoint**

`cmd/zv-orchestrator/main.go`:
```go
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reche/zackvideo/internal/httpapi"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
	"github.com/reche/zackvideo/internal/workers"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	store, err := storage.NewLocal(cfg.DataDir)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	repo := job.NewRepository(pool)
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr}
	client := asynq.NewClient(redisOpt)
	defer client.Close()

	handlers := httpapi.NewHandlers(repo, store, client)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Routes(handlers),
		ReadHeaderTimeout: 10 * time.Second,
	}

	worker := workers.NewParserWorker(repo, store)
	asynqSrv := asynq.NewServer(redisOpt, asynq.Config{Concurrency: cfg.WorkerConcurrency})
	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeParseDemo, worker.HandleParseDemo)

	// Start HTTP
	go func() {
		log.Printf("http: listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	// Start Asynq (blocks until ctx is cancelled)
	go func() {
		log.Printf("asynq: starting worker (concurrency=%d)", cfg.WorkerConcurrency)
		if err := asynqSrv.Run(mux); err != nil {
			log.Printf("asynq: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutdown: received signal, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	asynqSrv.Shutdown()
	log.Print("shutdown: done")
}
```

- [ ] **Step 3: Build the binary**

Run: `go build -o bin/zv-orchestrator ./cmd/zv-orchestrator`
Expected: builds cleanly.

- [ ] **Step 4: Smoke-start the orchestrator**

Run: `make up && make migrate-up && ZV_DATABASE_URL=postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable ./bin/zv-orchestrator &`
Expected: logs "http: listening on :8080" and "asynq: starting worker".

Stop it: `pkill zv-orchestrator`.

- [ ] **Step 5: Commit**

```bash
git add cmd/zv-orchestrator/
git commit -m "feat(orchestrator): wire HTTP API + Asynq worker into a single binary"
```

---

## Task 12: End-to-end smoke test

**Files:**
- Create: `scripts/smoke.sh`

- [ ] **Step 1: Write the smoke script**

`scripts/smoke.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail

DEMO="${1:-testdata/lavked-vs-tnc-m2-nuke.dem}"
TARGET="${2:-76561198148986856}"
BASE="${ZV_BASE_URL:-http://localhost:8080}"

if [ ! -f "$DEMO" ]; then
  echo "demo not found: $DEMO" >&2
  exit 1
fi

echo "→ uploading $DEMO with target=$TARGET"
JOB=$(curl -fsS -X POST "$BASE/api/jobs" \
  -F "demo=@$DEMO" \
  -F "config={\"target_steamid\":\"$TARGET\"}" | tee /dev/stderr)
ID=$(echo "$JOB" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")

echo "→ job id = $ID; polling status…"
for i in $(seq 1 60); do
  STATUS=$(curl -fsS "$BASE/api/jobs/$ID" | python3 -c "import sys,json;print(json.load(sys.stdin)['status'])")
  echo "  [$i] status=$STATUS"
  case "$STATUS" in
    parsed) break ;;
    failed) echo "job failed" >&2; exit 2 ;;
  esac
  sleep 2
done

if [ "$STATUS" != "parsed" ]; then
  echo "timeout waiting for parse" >&2
  exit 3
fi

echo "→ fetching plan"
curl -fsS "$BASE/api/jobs/$ID/plan" | python3 -m json.tool | head -40
echo "✔ smoke test passed"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/smoke.sh`

- [ ] **Step 3: Run end-to-end**

Run:
```bash
make up && make migrate-up
ZV_DATABASE_URL=postgres://zackvideo:zackvideo@localhost:5432/zackvideo?sslmode=disable \
  ./bin/zv-orchestrator &
sleep 2
./scripts/smoke.sh
```
Expected: prints status transitions queued → parsing → parsed and the head of the kill plan JSON (must include `de_nuke`).

Clean up: `pkill zv-orchestrator && make down`.

- [ ] **Step 4: Commit**

```bash
git add scripts/smoke.sh
git commit -m "test: add end-to-end smoke script for orchestrator"
```

---

## Self-review

**Spec coverage:**
- POST /api/jobs — Task 7 ✓
- GET /api/jobs/{id} — Task 8 ✓
- GET /api/jobs/{id}/plan — Task 9 ✓
- Asynq parser worker — Task 10 ✓
- jobs table — Task 2 ✓
- Local filesystem storage — Task 5 ✓
- Domain types + repository — Tasks 3, 4 ✓
- Binary wiring — Task 11 ✓
- End-to-end validation — Task 12 ✓

**Placeholder scan:** none.

**Type consistency:** `JobRepository` interface is defined in both `httpapi` and `workers` packages with different method sets (each lists only what it needs) — intentional. `Enqueuer` interface only needs `Enqueue(...)` so `*asynq.Client` satisfies it.

**Independent execution windows for subagents:** Tasks 1 → 2 are sequential foundation. After Task 2, Tasks 3–6 can run in parallel (no cross-deps). After Tasks 3–6, Tasks 7–10 can run in parallel. Task 11 depends on all internal packages. Task 12 depends on Task 11.
