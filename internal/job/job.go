// Package job defines the orchestrator's core domain type and helpers.
package job

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

// Status is the lifecycle state of a Job.
type Status int

const (
	StatusQueued Status = iota
	StatusParsing
	StatusParsed
	StatusFailed
)

var statusNames = [...]string{"queued", "parsing", "parsed", "failed"}

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
	ID            uuid.UUID      `json:"id"`
	Status        Status         `json:"status"`
	FailureReason string         `json:"failure_reason,omitempty"`
	DemoPath      string         `json:"demo_path"`
	DemoSHA256    string         `json:"demo_sha256"`
	TargetSteamID string         `json:"target_steamid"`
	Rules         rules.Rules    `json:"rules"`
	KillPlan      *killplan.Plan `json:"kill_plan,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
