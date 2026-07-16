// Package job defines the orchestrator's core domain type and helpers.
package job

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

// ErrNotFound is returned by Get when no job has the requested id.
var ErrNotFound = errors.New("job not found")

// ErrConflict is returned when an operation is rejected because the job is not
// in a state that allows it (e.g. a parse request for a job that was never
// scanned). It maps to HTTP 409 at the API boundary.
var ErrConflict = errors.New("job state conflict")

// Status is the lifecycle state of a Job.
type Status int

const (
	StatusQueued Status = iota
	StatusParsing
	StatusParsed
	StatusRecording
	StatusRecorded
	StatusComposing
	StatusComposed
	StatusDone
	StatusFailed
	// Appended after StatusFailed so the existing integer values are unchanged.
	StatusScanning // queued→scanning while the roster scan runs
	StatusScanned  // roster ready; awaiting the user's target pick
)

var statusNames = [...]string{
	"queued",
	"parsing",
	"parsed",
	"recording",
	"recorded",
	"composing",
	"composed",
	"done",
	"failed",
	"scanning",
	"scanned",
}

// String returns the canonical lowercase representation used in JSON and DB.
func (s Status) String() string {
	if int(s) < 0 || int(s) >= len(statusNames) {
		return "unknown"
	}
	return statusNames[s]
}

// MarshalJSON renders Status as the canonical string.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON parses a JSON string into a Status.
func (s *Status) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	parsed, err := ParseStatus(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ParseStatus converts a canonical name into a Status.
func ParseStatus(name string) (Status, error) {
	for i, n := range statusNames {
		if n == name {
			return Status(i), nil
		}
	}
	return 0, fmt.Errorf("unknown job status %q", name)
}

// Job is the canonical domain model used by the API, the worker, and the DB.
type Job struct {
	ID            uuid.UUID `json:"id"`
	Status        Status    `json:"status"`
	FailureReason string    `json:"failure_reason,omitempty"`
	// SeriesID groups the jobs of one uploaded bo3/bo5 series. It is a
	// client-minted UUID shared by every demo in the series; empty for a
	// standalone single-demo upload.
	SeriesID string `json:"series_id,omitempty"`
	// DemoFileName is the sanitized original file name of the uploaded demo,
	// kept only for display; empty when the upload carried no usable name.
	DemoFileName  string         `json:"demo_file_name,omitempty"`
	DemoPath      string         `json:"demo_path"`
	DemoSHA256    string         `json:"demo_sha256"`
	TargetSteamID string         `json:"target_steamid"`
	Rules         rules.Rules    `json:"rules"`
	KillPlan      *killplan.Plan `json:"kill_plan,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
