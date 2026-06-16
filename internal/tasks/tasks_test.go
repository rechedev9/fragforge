package tasks

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
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
	tk, err := NewRecordDemoTask(id)
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
	tk, err := NewRenderVariantTask(id, testRenderVariant, "")
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
}

func TestNewRenderVariantTaskRejectsUnsafeVariant(t *testing.T) {
	id := uuid.New()
	for _, variant := range []string{"", "../x", "x/y", `x\y`, "-bad", "x.mp4"} {
		if _, err := NewRenderVariantTask(id, variant, ""); err == nil {
			t.Fatalf("NewRenderVariantTask(%q) error = nil, want error", variant)
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
