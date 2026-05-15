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
