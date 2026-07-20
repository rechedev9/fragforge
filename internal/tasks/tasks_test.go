package tasks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/renderplan"
)

const testRenderVariant = "viral-60-clean"

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

func TestGenerateStreamCaptionsTaskCarriesGenerationOutsideUniquePayload(t *testing.T) {
	jobID := uuid.New()
	firstGeneration := uuid.New()
	secondGeneration := uuid.New()
	first, err := NewGenerateStreamCaptionsTask(jobID, firstGeneration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGenerateStreamCaptionsTask(jobID, secondGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Payload()) != string(second.Payload()) {
		t.Fatalf("payloads differ; asynq Unique must dedupe by job: %s != %s", first.Payload(), second.Payload())
	}
	if got, err := StreamCaptionGenerationFromTask(first); err != nil || got != firstGeneration {
		t.Fatalf("first generation = %s, %v; want %s", got, err, firstGeneration)
	}
	if got, err := StreamCaptionGenerationFromTask(second); err != nil || got != secondGeneration {
		t.Fatalf("second generation = %s, %v; want %s", got, err, secondGeneration)
	}
}

func TestGenerateStreamKillfeedTaskCarriesGenerationInPayloadAndHeader(t *testing.T) {
	jobID := uuid.New()
	firstGeneration := uuid.New()
	secondGeneration := uuid.New()
	first, err := NewGenerateStreamKillfeedTask(jobID, firstGeneration)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGenerateStreamKillfeedTask(jobID, secondGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Payload()) == string(second.Payload()) {
		t.Fatalf("payloads are equal; asynq Unique would suppress a fresh generation: %s", first.Payload())
	}
	var payload GenerateStreamKillfeedPayload
	if err := json.Unmarshal(first.Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.JobID != jobID || payload.GenerationID != firstGeneration {
		t.Fatalf("payload = %+v, want job %s generation %s", payload, jobID, firstGeneration)
	}
	if got, err := StreamKillfeedGenerationFromTask(first); err != nil || got != firstGeneration {
		t.Fatalf("generation = %s, %v; want %s", got, err, firstGeneration)
	}
	if got, err := StreamKillfeedGenerationFromTask(second); err != nil || got != secondGeneration {
		t.Fatalf("generation = %s, %v; want %s", got, err, secondGeneration)
	}
}

func TestBoundStreamRenderTaskKeepsUniquePayloadAndCarriesIntent(t *testing.T) {
	jobID := uuid.New()
	plain, err := NewRenderStreamClipTask(jobID, "streamer-40-60")
	if err != nil {
		t.Fatal(err)
	}
	intent := StreamRenderIntent{
		AttemptID:           uuid.New(),
		EditPlanFingerprint: strings.Repeat("a", 64),
		KillfeedGeneration:  uuid.New(),
		KillfeedFingerprint: strings.Repeat("b", 64),
	}
	bound, err := NewBoundRenderStreamClipTask(jobID, "streamer-40-60", intent)
	if err != nil {
		t.Fatal(err)
	}
	if string(bound.Payload()) != string(plain.Payload()) {
		t.Fatalf("bound payload = %s, want stable unique payload %s", bound.Payload(), plain.Payload())
	}
	got, ok, err := StreamRenderIntentFromTask(bound)
	if err != nil || !ok || got != intent {
		t.Fatalf("StreamRenderIntentFromTask = (%+v, %v, %v), want (%+v, true, nil)", got, ok, err, intent)
	}
	if _, ok, err := StreamRenderIntentFromTask(plain); err != nil || ok {
		t.Fatalf("plain intent = (_, %v, %v), want (_, false, nil)", ok, err)
	}
}

func TestNewScanRosterTaskRoundtrip(t *testing.T) {
	id := uuid.New()
	tk, err := NewScanRosterTask(id)
	if err != nil {
		t.Fatalf("NewScanRosterTask error = %v", err)
	}
	if tk.Type() != TypeScanRoster {
		t.Errorf("Type() = %q, want %q", tk.Type(), TypeScanRoster)
	}

	var payload ScanRosterPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.JobID != id {
		t.Errorf("JobID = %v, want %v", payload.JobID, id)
	}
}

func TestNewRecordDemoTaskRoundtrip(t *testing.T) {
	id := uuid.New()
	tk, err := NewRecordDemoTask(id, "", nil, false)
	if err != nil {
		t.Fatalf("NewRecordDemoTask error = %v", err)
	}
	if tk.Type() != TypeRecordDemo {
		t.Errorf("Type() = %q, want %q", tk.Type(), TypeRecordDemo)
	}

	var payload RecordDemoPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.JobID != id {
		t.Errorf("JobID = %v, want %v", payload.JobID, id)
	}
	if payload.SegmentIDs != nil {
		t.Errorf("SegmentIDs = %v, want nil", payload.SegmentIDs)
	}
}

func TestNewRecordDemoTaskRoundtripWithSegmentIDs(t *testing.T) {
	id := uuid.New()
	want := []string{"seg-001", "seg-002"}
	tk, err := NewRecordDemoTask(id, "clean", want, true)
	if err != nil {
		t.Fatalf("NewRecordDemoTask error = %v", err)
	}

	// The wire field is snake_case so the proxy and handler agree on the name.
	if !strings.Contains(string(tk.Payload()), `"segment_ids"`) {
		t.Errorf("payload missing segment_ids field: %s", tk.Payload())
	}

	var payload RecordDemoPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.HUDMode != "clean" {
		t.Errorf("HUDMode = %q, want clean", payload.HUDMode)
	}
	if !payload.PortraitSafeKillfeed {
		t.Error("PortraitSafeKillfeed = false, want true")
	}
	if len(payload.SegmentIDs) != len(want) {
		t.Fatalf("SegmentIDs = %v, want %v", payload.SegmentIDs, want)
	}
	for i := range want {
		if payload.SegmentIDs[i] != want[i] {
			t.Errorf("SegmentIDs[%d] = %q, want %q", i, payload.SegmentIDs[i], want[i])
		}
	}
}

func TestNewGenerateRecordDemoTaskCarriesIntentOutsideUniquePayload(t *testing.T) {
	id := uuid.New()
	want := renderplan.GenerateIntent{
		Variant:  testRenderVariant,
		MusicKey: "track-01",
		Edit:     renderplan.DefaultEditRequest(),
	}
	task, err := NewGenerateRecordDemoTask(id, "deathnotices", []string{"seg-001"}, true, want)
	if err != nil {
		t.Fatalf("NewGenerateRecordDemoTask error = %v", err)
	}
	got, ok, err := GenerateIntentFromTask(task)
	if err != nil {
		t.Fatalf("GenerateIntentFromTask error = %v", err)
	}
	if !ok || got != want {
		t.Fatalf("GenerateIntentFromTask = (%#v, %v), want (%#v, true)", got, ok, want)
	}

	plain, err := NewRecordDemoTask(id, "deathnotices", []string{"seg-001"}, true)
	if err != nil {
		t.Fatalf("NewRecordDemoTask error = %v", err)
	}
	if string(task.Payload()) != string(plain.Payload()) {
		t.Fatalf("generate payload = %s, want capture-only payload %s", task.Payload(), plain.Payload())
	}
	if _, ok, err := GenerateIntentFromTask(plain); err != nil || ok {
		t.Fatalf("plain GenerateIntentFromTask = (_, %v, %v), want (_, false, nil)", ok, err)
	}
}

func TestGenerateIntentFromTaskRejectsInvalidHeader(t *testing.T) {
	task := asynq.NewTaskWithHeaders(TypeRecordDemo, nil, map[string]string{
		generateIntentHeader: `{"variant":"missing"}`,
	})
	if _, _, err := GenerateIntentFromTask(task); err == nil {
		t.Fatal("GenerateIntentFromTask error = nil, want invalid intent error")
	}
}

func TestNewComposeFinalTaskRoundtrip(t *testing.T) {
	id := uuid.New()
	tk, err := NewComposeFinalTask(id)
	if err != nil {
		t.Fatalf("NewComposeFinalTask error = %v", err)
	}
	if tk.Type() != TypeComposeFinal {
		t.Errorf("Type() = %q, want %q", tk.Type(), TypeComposeFinal)
	}

	var payload ComposeFinalPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.JobID != id {
		t.Errorf("JobID = %v, want %v", payload.JobID, id)
	}
}

func TestNewRenderVariantTaskRoundtrip(t *testing.T) {
	id := uuid.New()
	edit := renderplan.EditRequest{Format: renderplan.FormatLandscape16x9, KillEffect: renderplan.KillEffectVelocity, Transition: renderplan.TransitionWhip, Intro: true, HookText: true, KillCounter: true, CoverStrategy: renderplan.CoverStrategyNone}
	tk, err := NewRenderVariantTask(id, testRenderVariant, "concrete-teeth", 0.35, edit)
	if err != nil {
		t.Fatalf("NewRenderVariantTask error = %v", err)
	}
	if tk.Type() != TypeRenderVariant {
		t.Errorf("Type() = %q, want %q", tk.Type(), TypeRenderVariant)
	}

	var payload RenderVariantPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.JobID != id {
		t.Errorf("JobID = %v, want %v", payload.JobID, id)
	}
	if payload.Variant != testRenderVariant {
		t.Errorf("Variant = %q, want %q", payload.Variant, testRenderVariant)
	}
	if payload.MusicKey != "concrete-teeth" || payload.MusicVolume != 0.35 {
		t.Errorf("music = %q/%v, want concrete-teeth/0.35", payload.MusicKey, payload.MusicVolume)
	}
	if payload.Edit != edit {
		t.Errorf("Edit = %#v, want %#v", payload.Edit, edit)
	}
	if !strings.Contains(string(tk.Payload()), `"hook_text":true`) || !strings.Contains(string(tk.Payload()), `"kill_counter":true`) {
		t.Errorf("payload missing automatic text fields: %s", tk.Payload())
	}
	if !strings.Contains(string(tk.Payload()), `"cover_strategy":"no-cover"`) {
		t.Errorf("payload missing cover strategy: %s", tk.Payload())
	}
}

func TestNewRenderVariantTaskRejectsUnsafeVariant(t *testing.T) {
	id := uuid.New()
	for _, variant := range []string{"", "../x", "x/y", `x\y`, "-bad", "x.mp4"} {
		if _, err := NewRenderVariantTask(id, variant, "", 0, renderplan.EditRequest{}); err == nil {
			t.Fatalf("NewRenderVariantTask(%q) error = nil, want error", variant)
		}
	}
}

func TestNewRenderVariantTaskRejectsOutOfRangeMusicVolume(t *testing.T) {
	id := uuid.New()
	for _, volume := range []float64{-0.1, 1.5} {
		if _, err := NewRenderVariantTask(id, testRenderVariant, "", volume, renderplan.EditRequest{}); err == nil {
			t.Fatalf("NewRenderVariantTask(volume=%v) error = nil, want error", volume)
		}
	}
}

func TestNewCodexAgentTaskRoundtrip(t *testing.T) {
	id := uuid.New()
	tk, err := NewCodexAgentTask(id, testRenderVariant, "caption-candidates")
	if err != nil {
		t.Fatalf("NewCodexAgentTask error = %v", err)
	}
	if tk.Type() != TypeCodexAgent {
		t.Errorf("Type() = %q, want %q", tk.Type(), TypeCodexAgent)
	}
	var payload CodexAgentPayload
	if err := json.Unmarshal(tk.Payload(), &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.JobID != id || payload.Variant != testRenderVariant || payload.Kind != "caption-candidates" {
		t.Fatalf("payload = %#v", payload)
	}
}
