// Package tasks defines the Asynq task types and payloads shared between
// the orchestrator (producer) and the workers (consumer).
package tasks

import (
	"encoding/json"
	"time"

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

const (
	// parseDemoTimeout bounds how long a single demo parse may run before Asynq
	// cancels the task context. Parsing a legitimate CS2 demo finishes in
	// seconds; this generous ceiling stops a corrupt or pathological demo from
	// pinning a worker slot indefinitely. The parser worker threads this context
	// into demoinfocs via parser.RunWithContext, so an exceeded deadline aborts
	// ParseToEnd instead of running forever.
	parseDemoTimeout = 15 * time.Minute

	// parseDemoMaxRetry caps retries for a parse task. Parsing is deterministic:
	// a demo that fails (corrupt, target-not-found, or timed out) fails the same
	// way every time, so the default 25 retries only waste worker slots. One
	// retry still absorbs a transient infrastructure blip (Redis/temp-file).
	parseDemoMaxRetry = 1
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
	return asynq.NewTask(TypeParseDemo, payload,
		asynq.Timeout(parseDemoTimeout),
		asynq.MaxRetry(parseDemoMaxRetry),
	), nil
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
