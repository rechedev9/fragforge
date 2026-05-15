// Package tasks defines the Asynq task types and payloads shared between
// the orchestrator (producer) and the workers (consumer).
package tasks

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	// TypeParseDemo is the Asynq task type for parsing a demo into a kill plan.
	TypeParseDemo = "parse:demo"

	// TypeRecordDemo is the Asynq task type for running the Windows recorder.
	TypeRecordDemo = "record:demo"

	// TypeComposeFinal is the Asynq task type for building the first final MP4.
	TypeComposeFinal = "compose:final"
)

// ParseDemoPayload carries the inputs the worker needs to fetch from the DB.
type ParseDemoPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// RecordDemoPayload carries the job id for a Windows recording worker.
type RecordDemoPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// ComposeFinalPayload carries the job id for the composition worker.
type ComposeFinalPayload struct {
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

func NewRecordDemoTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(RecordDemoPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeRecordDemo, payload), nil
}

func NewComposeFinalTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(ComposeFinalPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeComposeFinal, payload), nil
}
