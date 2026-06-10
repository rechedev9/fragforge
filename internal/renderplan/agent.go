package renderplan

import (
	"time"

	"github.com/google/uuid"
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

func NewAgentContext(jobID uuid.UUID, variant, kind, momentsKey, packManifestKey string, moments, packManifest any) AgentContext {
	return AgentContext{
		SchemaVersion:   AgentSchemaVersion,
		JobID:           jobID,
		Variant:         variant,
		Kind:            kind,
		MomentsKey:      momentsKey,
		PackManifestKey: packManifestKey,
		Moments:         moments,
		PackManifest:    packManifest,
		CreatedAt:       time.Now().UTC(),
	}
}

func NewAgentResult(jobID uuid.UUID, variant, kind, status, contextKey, resultKey string) AgentResult {
	return AgentResult{
		SchemaVersion: AgentSchemaVersion,
		JobID:         jobID,
		Variant:       variant,
		Kind:          kind,
		Status:        status,
		Artifacts: AgentResultKeys{
			Context: contextKey,
			Result:  resultKey,
		},
		GeneratedAt: time.Now().UTC(),
	}
}
