package main

import (
	"context"
	"testing"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

func TestMemoryJobRepositoryStoresJobLifecycle(t *testing.T) {
	repo := newMemoryJobRepository()
	ctx := context.Background()
	j := &job.Job{
		Status:        job.StatusQueued,
		DemoPath:      "demos/demo.dem",
		DemoSHA256:    "abc123",
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}

	if err := repo.Create(ctx, j); err != nil {
		t.Fatalf("Create error = %v", err)
	}
	if j.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("Create left nil job id")
	}

	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 64, TickEnd: 128}}
	if err := repo.SetKillPlan(ctx, j.ID, plan); err != nil {
		t.Fatalf("SetKillPlan error = %v", err)
	}
	if err := repo.UpdateStatus(ctx, j.ID, job.StatusParsed, ""); err != nil {
		t.Fatalf("UpdateStatus error = %v", err)
	}

	got, err := repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.Status != job.StatusParsed || got.KillPlan == nil || len(got.KillPlan.Segments) != 1 {
		t.Fatalf("Get = %#v, want parsed job with one segment", got)
	}

	meta, err := repo.GetMeta(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetMeta error = %v", err)
	}
	if meta.KillPlan != nil {
		t.Fatal("GetMeta returned kill plan, want nil")
	}

	jobs, err := repo.List(ctx, 10)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != j.ID || jobs[0].KillPlan != nil {
		t.Fatalf("List = %#v, want one metadata-only job", jobs)
	}
}
