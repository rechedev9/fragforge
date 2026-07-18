package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/generateintent"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestSQLiteRestartReconcilesPersistedInlineWork(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "jobs.db")
	storagePath := filepath.Join(dir, "artifacts")

	before, err := newSQLiteJobRepository(databasePath)
	if err != nil {
		t.Fatalf("newSQLiteJobRepository before restart: %v", err)
	}
	streamBefore, err := newSQLiteStreamJobRepository(before.db)
	if err != nil {
		_ = before.Close()
		t.Fatalf("newSQLiteStreamJobRepository before restart: %v", err)
	}
	storeBefore, err := storage.NewLocal(storagePath)
	if err != nil {
		_ = before.Close()
		t.Fatalf("storage.NewLocal before restart: %v", err)
	}

	queued := createRestartDemoJob(t, before, job.StatusQueued)
	rendering := createRestartDemoJob(t, before, job.StatusRecorded)
	generating := createRestartDemoJob(t, before, job.StatusParsed)

	loadout := renderplan.LoadoutCatalog()[0]
	demoState, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   rendering.ID,
		Loadout: loadout,
		Status:  renderplan.RenderVariantStatusRendering,
	})
	if err != nil {
		t.Fatalf("NewRenderVariantStateForLoadout: %v", err)
	}
	demoStateKey, err := renderplan.RenderVariantStateKey(rendering.ID, loadout.Variant)
	if err != nil {
		t.Fatalf("RenderVariantStateKey: %v", err)
	}
	putRestartJSON(t, storeBefore, demoStateKey, demoState)

	generateIntent := renderplan.GenerateIntent{
		Variant:     loadout.Variant,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
		AcceptedAt:  time.Now().UTC(),
	}
	putRestartJSON(t, storeBefore, artifacts.GenerateIntentKey(generating.ID), generateIntent)

	acquiring := createRestartStreamJob(t, streamBefore, streamclips.StatusAcquiring)
	streamRendering := createRestartStreamJob(t, streamBefore, streamclips.StatusReady)
	streamVariant := streamclips.DefaultVariant().Name
	streamVideoKey, err := streamclips.RenderVideoKey(streamRendering.ID, streamVariant, "clip-1")
	if err != nil {
		t.Fatalf("RenderVideoKey: %v", err)
	}
	streamState, err := streamclips.NewRenderState(
		streamRendering.ID,
		streamVariant,
		streamclips.StatusRendering,
		[]string{"preserve warning"},
		"",
		[]streamclips.VideoEntry{{ClipID: "clip-1", Key: streamVideoKey}},
	)
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	streamStateKey, err := streamclips.RenderStateKey(streamRendering.ID, streamVariant)
	if err != nil {
		t.Fatalf("RenderStateKey: %v", err)
	}
	putRestartJSON(t, storeBefore, streamStateKey, streamState)

	if err := before.Close(); err != nil {
		t.Fatalf("close SQLite before restart: %v", err)
	}

	after, err := newSQLiteJobRepository(databasePath)
	if err != nil {
		t.Fatalf("newSQLiteJobRepository after restart: %v", err)
	}
	t.Cleanup(func() { _ = after.Close() })
	streamAfter, err := newSQLiteStreamJobRepository(after.db)
	if err != nil {
		t.Fatalf("newSQLiteStreamJobRepository after restart: %v", err)
	}
	storeAfter, err := storage.NewLocal(storagePath)
	if err != nil {
		t.Fatalf("storage.NewLocal after restart: %v", err)
	}

	reconciled, err := reconcileInterruptedWork(ctx, after, streamAfter, storeAfter, nil)
	if err != nil {
		t.Fatalf("reconcileInterruptedWork after restart: %v", err)
	}
	wantReconciled := startupReconciliationResult{
		DemoJobs:           1,
		DemoRenders:        1,
		GenerateRuns:       1,
		StreamJobs:         1,
		StreamRenderStates: 1,
	}
	if reconciled != wantReconciled {
		t.Fatalf("reconciled = %+v, want %+v", reconciled, wantReconciled)
	}

	assertRestartDemoFailed(t, after, queued.ID, interruptedQueuedJobReason)
	assertRestartDemoFailed(t, after, generating.ID, interruptedGenerateReason)
	retryIntent := generateIntent
	retryIntent.ActiveRunID = uuid.New()
	if err := generateintent.New(storeAfter).Begin(generating.ID, retryIntent, nil); err != nil {
		t.Fatalf("begin generate retry after restart reconciliation: %v", err)
	}

	gotRendering, err := after.Get(ctx, rendering.ID)
	if err != nil {
		t.Fatalf("Get render parent: %v", err)
	}
	if gotRendering.Status != job.StatusRecorded {
		t.Fatalf("render parent status = %s, want recorded", gotRendering.Status)
	}
	var gotDemoState renderplan.RenderVariantState
	readRestartJSON(t, storeAfter, demoStateKey, &gotDemoState)
	if gotDemoState.Status != renderplan.RenderVariantStatusFailed || gotDemoState.Error != interruptedDemoRenderReason {
		t.Fatalf("demo render after restart = status %q error %q, want failed/%q", gotDemoState.Status, gotDemoState.Error, interruptedDemoRenderReason)
	}

	gotAcquiring, err := streamAfter.Get(ctx, acquiring.ID)
	if err != nil {
		t.Fatalf("Get acquiring stream: %v", err)
	}
	if gotAcquiring.Status != streamclips.StatusFailed || gotAcquiring.FailureReason != interruptedStreamAcquire {
		t.Fatalf("acquiring stream after restart = status %q reason %q, want failed/%q", gotAcquiring.Status, gotAcquiring.FailureReason, interruptedStreamAcquire)
	}
	gotStreamParent, err := streamAfter.Get(ctx, streamRendering.ID)
	if err != nil {
		t.Fatalf("Get stream render parent: %v", err)
	}
	if gotStreamParent.Status != streamclips.StatusReady {
		t.Fatalf("stream render parent status = %q, want ready", gotStreamParent.Status)
	}
	var gotStreamState streamclips.RenderState
	readRestartJSON(t, storeAfter, streamStateKey, &gotStreamState)
	if gotStreamState.Status != streamclips.StatusFailed || gotStreamState.Error != interruptedStreamRender {
		t.Fatalf("stream render after restart = status %q error %q, want failed/%q", gotStreamState.Status, gotStreamState.Error, interruptedStreamRender)
	}
	if len(gotStreamState.Warnings) != 1 || len(gotStreamState.Videos) != 1 || gotStreamState.Videos[0].Key != streamVideoKey {
		t.Fatalf("stream render artifact data was not preserved: %+v", gotStreamState)
	}
}

func createRestartDemoJob(t *testing.T, repo *sqliteJobRepository, status job.Status) job.Job {
	t.Helper()
	j := job.Job{
		Status:        status,
		DemoPath:      "demos/" + status.String() + ".dem",
		DemoSHA256:    "sha-" + status.String(),
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	if err := repo.Create(context.Background(), &j); err != nil {
		t.Fatalf("Create demo job %s: %v", status, err)
	}
	return j
}

func createRestartStreamJob(t *testing.T, repo *sqliteStreamJobRepository, status streamclips.Status) streamclips.Job {
	t.Helper()
	j := streamclips.Job{
		Status:       status,
		SourcePath:   "streams/" + string(status) + ".mp4",
		SourceSHA256: "sha-" + string(status),
		Probe:        streamclips.SourceProbe{Width: 1920, Height: 1080},
	}
	if err := repo.Create(context.Background(), &j); err != nil {
		t.Fatalf("Create stream job %s: %v", status, err)
	}
	return j
}

func putRestartJSON(t *testing.T, store storage.Storage, key string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal %s: %v", key, err)
	}
	if err := store.Put(key, bytes.NewReader(b)); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
}

func readRestartJSON(t *testing.T, store storage.Storage, key string, dst any) {
	t.Helper()
	rc, err := store.Open(key)
	if err != nil {
		t.Fatalf("Open %s: %v", key, err)
	}
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(dst); err != nil {
		t.Fatalf("Decode %s: %v", key, err)
	}
}

func assertRestartDemoFailed(t *testing.T, repo *sqliteJobRepository, id uuid.UUID, reason string) {
	t.Helper()
	got, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get demo %s: %v", id, err)
	}
	if got.Status != job.StatusFailed || got.FailureReason != reason {
		t.Fatalf("demo %s after restart = status %s reason %q, want failed/%q", id, got.Status, got.FailureReason, reason)
	}
}
