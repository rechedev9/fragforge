package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
)

var benchmarkMetadataJob job.Job
var benchmarkJobStatus job.Status
var benchmarkJobList []job.Job

func BenchmarkSQLiteGetMetaWithLargePlan(b *testing.B) {
	repo, err := newSQLiteJobRepository(filepath.Join(b.TempDir(), "jobs.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repo.Close() })

	plan := killplan.NewPlan()
	plan.Segments = make([]killplan.Segment, 240)
	for i := range plan.Segments {
		plan.Segments[i].ID = killplan.FormatSegmentID(i + 1)
		plan.Segments[i].Kills = make([]killplan.Kill, 8)
		for k := range plan.Segments[i].Kills {
			plan.Segments[i].Kills[k] = killplan.Kill{
				Tick:   i*640 + k*64,
				Weapon: "ak47",
				Victim: killplan.Player{SteamID64: "76561198000000000", NameInDemo: "player", TeamAtKill: "CT"},
			}
		}
	}
	j := &job.Job{Status: job.StatusParsed, KillPlan: &plan}
	if err := repo.Create(context.Background(), j); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := repo.GetMeta(context.Background(), j.ID)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkMetadataJob = got
	}
}

func BenchmarkSQLiteGetStatusWithLargePlan(b *testing.B) {
	repo, err := newSQLiteJobRepository(filepath.Join(b.TempDir(), "jobs.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repo.Close() })

	plan := killplan.NewPlan()
	plan.Segments = make([]killplan.Segment, 240)
	for i := range plan.Segments {
		plan.Segments[i].ID = killplan.FormatSegmentID(i + 1)
		plan.Segments[i].Kills = make([]killplan.Kill, 8)
	}
	j := &job.Job{Status: job.StatusParsed, KillPlan: &plan}
	if err := repo.Create(context.Background(), j); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status, _, _, err := repo.GetStatus(context.Background(), j.ID)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkJobStatus = status
	}
}

func BenchmarkSQLiteListWithLargePlans(b *testing.B) {
	repo, err := newSQLiteJobRepository(filepath.Join(b.TempDir(), "jobs.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repo.Close() })

	plan := killplan.NewPlan()
	plan.Segments = make([]killplan.Segment, 240)
	for i := range plan.Segments {
		plan.Segments[i].ID = killplan.FormatSegmentID(i + 1)
		plan.Segments[i].Kills = make([]killplan.Kill, 8)
		for k := range plan.Segments[i].Kills {
			plan.Segments[i].Kills[k] = killplan.Kill{
				Tick:   i*640 + k*64,
				Weapon: "ak47",
				Victim: killplan.Player{SteamID64: "76561198000000000", NameInDemo: "player", TeamAtKill: "CT"},
			}
		}
	}
	for range 20 {
		j := &job.Job{Status: job.StatusParsed, KillPlan: &plan}
		if err := repo.Create(context.Background(), j); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobs, err := repo.List(context.Background(), 20)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkJobList = jobs
	}
}

func BenchmarkSQLiteUpdateStatusWithLargePlan(b *testing.B) {
	repo, err := newSQLiteJobRepository(filepath.Join(b.TempDir(), "jobs.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = repo.Close() })

	plan := killplan.NewPlan()
	plan.Segments = make([]killplan.Segment, 240)
	for i := range plan.Segments {
		plan.Segments[i].ID = killplan.FormatSegmentID(i + 1)
		plan.Segments[i].Kills = make([]killplan.Kill, 8)
		for k := range plan.Segments[i].Kills {
			plan.Segments[i].Kills[k] = killplan.Kill{
				Tick:   i*640 + k*64,
				Weapon: "ak47",
				Victim: killplan.Player{SteamID64: "76561198000000000", NameInDemo: "player", TeamAtKill: "CT"},
			}
		}
	}
	j := &job.Job{Status: job.StatusParsed, KillPlan: &plan}
	if err := repo.Create(context.Background(), j); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		status := job.StatusParsed
		reason := ""
		if i%2 == 0 {
			status = job.StatusFailed
			reason = "benchmark failure"
		}
		if err := repo.UpdateStatus(context.Background(), j.ID, status, reason); err != nil {
			b.Fatal(err)
		}
	}
}
