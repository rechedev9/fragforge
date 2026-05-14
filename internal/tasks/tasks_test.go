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
