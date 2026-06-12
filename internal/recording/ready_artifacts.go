package recording

import "github.com/google/uuid"

// ReadyArtifacts names the durable recording artifacts that prove a recording
// run can be reused without rerunning zv-recorder.
type ReadyArtifacts struct {
	ResultKey    string
	RequiredKeys []string
	SegmentCount int
}

// NewReadyArtifacts returns the durable artifacts required by a successful
// recorder result.
func NewReadyArtifacts(jobID uuid.UUID, result RecordingResult) (ReadyArtifacts, error) {
	ready := ReadyArtifacts{
		ResultKey: ResultArtifactKey(jobID),
	}
	if result.Error != "" {
		return ready, nil
	}
	ready.RequiredKeys = append(ready.RequiredKeys, ScriptArtifactKey(jobID))
	for _, artifact := range result.Artifacts {
		if artifact.Role != "segment" || artifact.Type != "video" || artifact.SegmentID == "" {
			continue
		}
		key, err := SegmentClipArtifactKey(jobID, artifact.SegmentID)
		if err != nil {
			return ReadyArtifacts{}, err
		}
		ready.RequiredKeys = append(ready.RequiredKeys, key)
		ready.SegmentCount++
	}
	return ready, nil
}
