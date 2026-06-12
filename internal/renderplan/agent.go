package renderplan

import (
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
)

const AgentSchemaVersion = "1.0"
const AgentKindCaptionCandidates = "caption-candidates"

type AgentContext struct {
	SchemaVersion   string    `json:"schema_version"`
	JobID           uuid.UUID `json:"job_id"`
	Variant         string    `json:"variant"`
	Kind            string    `json:"kind"`
	MomentsKey      string    `json:"moments_key"`
	PackManifestKey string    `json:"pack_manifest_key"`
	Moments         any       `json:"moments"`
	PackManifest    any       `json:"pack_manifest"`
	CreatedAt       time.Time `json:"created_at"`
}

type AgentResult struct {
	SchemaVersion string          `json:"schema_version"`
	JobID         uuid.UUID       `json:"job_id"`
	Variant       string          `json:"variant"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	Titles        []string        `json:"titles,omitempty"`
	Captions      []string        `json:"captions,omitempty"`
	Hashtags      []string        `json:"hashtags,omitempty"`
	Notes         []string        `json:"notes,omitempty"`
	Raw           string          `json:"raw,omitempty"`
	Error         string          `json:"error,omitempty"`
	Artifacts     AgentResultKeys `json:"artifacts"`
	GeneratedAt   time.Time       `json:"generated_at"`
}

type AgentResultKeys struct {
	Context string `json:"context"`
	Result  string `json:"result"`
}

// AgentArtifacts names the durable inputs and outputs for one render-variant
// editorial agent run.
type AgentArtifacts struct {
	ContextKey      string
	ResultKey       string
	MomentsKey      string
	PackManifestKey string
}

// NewAgentArtifacts derives the durable storage keys used by one agent run.
func NewAgentArtifacts(jobID uuid.UUID, variant, kind string) (AgentArtifacts, error) {
	contextKey, err := artifacts.RenderVariantAgentContextKey(jobID, variant, kind)
	if err != nil {
		return AgentArtifacts{}, err
	}
	resultKey, err := artifacts.RenderVariantAgentResultKey(jobID, variant, kind)
	if err != nil {
		return AgentArtifacts{}, err
	}
	packKey, err := artifacts.RenderVariantPackManifestKey(jobID, variant)
	if err != nil {
		return AgentArtifacts{}, err
	}
	return AgentArtifacts{
		ContextKey:      contextKey,
		ResultKey:       resultKey,
		MomentsKey:      artifacts.MomentsKey(jobID),
		PackManifestKey: packKey,
	}, nil
}

func NewAgentContext(jobID uuid.UUID, variant, kind string, agentArtifacts AgentArtifacts, moments, packManifest any) AgentContext {
	return AgentContext{
		SchemaVersion:   AgentSchemaVersion,
		JobID:           jobID,
		Variant:         variant,
		Kind:            kind,
		MomentsKey:      agentArtifacts.MomentsKey,
		PackManifestKey: agentArtifacts.PackManifestKey,
		Moments:         moments,
		PackManifest:    packManifest,
		CreatedAt:       time.Now().UTC(),
	}
}

func NewAgentResult(jobID uuid.UUID, variant, kind, status string, agentArtifacts AgentArtifacts) AgentResult {
	return AgentResult{
		SchemaVersion: AgentSchemaVersion,
		JobID:         jobID,
		Variant:       variant,
		Kind:          kind,
		Status:        status,
		Artifacts: AgentResultKeys{
			Context: agentArtifacts.ContextKey,
			Result:  agentArtifacts.ResultKey,
		},
		GeneratedAt: time.Now().UTC(),
	}
}
