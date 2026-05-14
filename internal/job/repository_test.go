package job

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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

	b, _ := json.Marshal(got.KillPlan)
	if !strings.Contains(string(b), "de_nuke") {
		t.Errorf("marshaled plan does not contain map name: %s", string(b))
	}
}
