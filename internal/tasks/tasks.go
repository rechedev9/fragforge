// Package tasks defines the Asynq task types and payloads shared between
// the orchestrator (producer) and the workers (consumer).
package tasks

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// TypeParseDemo is the Asynq task type for parsing a demo into a kill plan.
const TypeParseDemo = "parse:demo"

// ParseDemoPayload carries the inputs the worker needs to fetch from the DB.
type ParseDemoPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// NewParseDemoTask returns an Asynq task that, when consumed, processes the
// job identified by id.
func NewParseDemoTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(ParseDemoPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeParseDemo, payload), nil
}
