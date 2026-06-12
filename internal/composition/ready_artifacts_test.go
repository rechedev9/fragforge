package composition

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewReadyArtifactsRequiresFinalForSuccessfulResult(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got := NewReadyArtifacts(id, Result{Output: "final.mp4"})

	if got.ResultKey != "jobs/11111111-1111-1111-1111-111111111111/composition/composition-result.json" {
		t.Fatalf("result key = %q", got.ResultKey)
	}
	wantRequired := []string{"jobs/11111111-1111-1111-1111-111111111111/composition/final.mp4"}
	if len(got.RequiredKeys) != len(wantRequired) {
		t.Fatalf("required keys len = %d, want %d: %#v", len(got.RequiredKeys), len(wantRequired), got.RequiredKeys)
	}
	for i := range wantRequired {
		if got.RequiredKeys[i] != wantRequired[i] {
			t.Fatalf("required key[%d] = %q, want %q", i, got.RequiredKeys[i], wantRequired[i])
		}
	}
}

func TestNewReadyArtifactsSkipsFinalForFailedResult(t *testing.T) {
	got := NewReadyArtifacts(uuid.New(), Result{Error: "compose failed"})

	if got.ResultKey == "" {
		t.Fatal("result key is empty")
	}
	if len(got.RequiredKeys) != 0 {
		t.Fatalf("required keys = %#v, want none for failed result", got.RequiredKeys)
	}
}
