package composition

import "github.com/google/uuid"

// ReadyArtifacts names the durable composition artifacts that prove a final
// composition can be reused without rerunning zv-composer.
type ReadyArtifacts struct {
	ResultKey    string
	RequiredKeys []string
}

// NewReadyArtifacts returns the durable artifacts required by a successful
// composition result.
func NewReadyArtifacts(jobID uuid.UUID, result Result) ReadyArtifacts {
	ready := ReadyArtifacts{
		ResultKey: ResultArtifactKey(jobID),
	}
	if result.Error != "" {
		return ready
	}
	ready.RequiredKeys = append(ready.RequiredKeys, FinalArtifactKey(jobID))
	return ready
}
