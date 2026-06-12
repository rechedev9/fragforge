package renderplan

import (
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestNewAgentArtifactsDerivesKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got, err := NewAgentArtifacts(id, editor.PresetViral60Clean, AgentKindCaptionCandidates)
	if err != nil {
		t.Fatalf("NewAgentArtifacts error = %v", err)
	}

	prefix := "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean"
	cases := map[string]string{
		got.ContextKey:      prefix + "/agents/caption-candidates/context.json",
		got.ResultKey:       prefix + "/agents/caption-candidates/result.json",
		got.MomentsKey:      "jobs/11111111-1111-1111-1111-111111111111/moments/moments.json",
		got.PackManifestKey: prefix + "/pack-manifest.json",
	}
	for gotKey, want := range cases {
		if gotKey != want {
			t.Fatalf("key = %q, want %q", gotKey, want)
		}
	}
}

func TestNewAgentContextAndResultUseArtifacts(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	agentArtifacts, err := NewAgentArtifacts(id, editor.PresetViral60Clean, AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}

	context := NewAgentContext(id, editor.PresetViral60Clean, AgentKindCaptionCandidates, agentArtifacts, "moments", "pack")
	if context.MomentsKey != agentArtifacts.MomentsKey {
		t.Fatalf("context moments key = %q, want %q", context.MomentsKey, agentArtifacts.MomentsKey)
	}
	if context.PackManifestKey != agentArtifacts.PackManifestKey {
		t.Fatalf("context pack key = %q, want %q", context.PackManifestKey, agentArtifacts.PackManifestKey)
	}

	result := NewAgentResult(id, editor.PresetViral60Clean, AgentKindCaptionCandidates, "ready", agentArtifacts)
	if result.Artifacts.Context != agentArtifacts.ContextKey {
		t.Fatalf("result context key = %q, want %q", result.Artifacts.Context, agentArtifacts.ContextKey)
	}
	if result.Artifacts.Result != agentArtifacts.ResultKey {
		t.Fatalf("result key = %q, want %q", result.Artifacts.Result, agentArtifacts.ResultKey)
	}
}
