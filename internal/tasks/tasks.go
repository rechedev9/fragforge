// Package tasks defines the Asynq task types and payloads shared between
// the orchestrator (producer) and the workers (consumer).
package tasks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/renderplan"
)

const (
	// TypeParseDemo is the Asynq task type for parsing a demo into a kill plan.
	TypeParseDemo = "parse:demo"

	// TypeScanRoster is the Asynq task type for scanning a demo's roster before
	// the user picks a target to clip.
	TypeScanRoster = "scan:roster"

	// TypeRecordDemo is the Asynq task type for running the Windows recorder.
	TypeRecordDemo = "record:demo"

	// TypeComposeFinal is the Asynq task type for building the first final MP4.
	TypeComposeFinal = "compose:final"

	// TypeRenderVariant is the Asynq task type for rendering a named output
	// variant from an existing recording result.
	TypeRenderVariant = "render:variant"

	// TypeCodexAgent is the Asynq task type for local Codex CLI editorial
	// assistance over already materialized render artifacts.
	TypeCodexAgent = "agent:codex"

	// TypeRenderStreamClip is the Asynq task type for rendering manually
	// selected clips from a streamer MP4 upload.
	TypeRenderStreamClip = "render:stream-clip"

	// TypeStreamAcquire is the Asynq task type for downloading a stream job's
	// source video from a URL (Twitch clip/VOD or any yt-dlp-supported site)
	// before it can be edited and rendered.
	TypeStreamAcquire = "stream:acquire"
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

var renderVariantPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ParseDemoPayload carries the inputs the worker needs to fetch from the DB.
type ParseDemoPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// ScanRosterPayload carries the job id for a roster scan worker.
type ScanRosterPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// RecordDemoPayload carries the job id for a Windows recording worker.
// HUDMode, when set, overrides the recorder's default in-game HUD for this
// capture (one of "gameplay", "clean", "deathnotices"); empty keeps the default.
// SegmentIDs, when non-empty, scopes the capture to those kill-plan segments so
// a reel records only the user-selected clip instead of the whole demo; empty
// records every segment (the CLI all-kills default).
type RecordDemoPayload struct {
	JobID      uuid.UUID `json:"job_id"`
	HUDMode    string    `json:"hud_mode,omitempty"`
	SegmentIDs []string  `json:"segment_ids,omitempty"`
}

// ComposeFinalPayload carries the job id for the composition worker.
type ComposeFinalPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// RenderVariantPayload carries the job id and render variant requested by the
// orchestrator. MusicKey, when set, names a music track the render worker mixes
// into the reel (resolved from its music directory).
type RenderVariantPayload struct {
	JobID    uuid.UUID              `json:"job_id"`
	Variant  string                 `json:"variant"`
	MusicKey string                 `json:"music_key,omitempty"`
	Edit     renderplan.EditRequest `json:"edit,omitempty"`
}

type CodexAgentPayload struct {
	JobID   uuid.UUID `json:"job_id"`
	Variant string    `json:"variant"`
	Kind    string    `json:"kind"`
}

type RenderStreamClipPayload struct {
	JobID   uuid.UUID `json:"job_id"`
	Variant string    `json:"variant"`
}

// StreamAcquirePayload carries the job id for the acquire-by-URL worker. The
// source URL itself lives on the stream job row (StreamJob.SourceURL), not
// the payload, so a retried/redriven task always re-reads the current state
// instead of a possibly-stale copy.
type StreamAcquirePayload struct {
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

// NewScanRosterTask mirrors NewParseDemoTask: a roster scan is a single
// deterministic pass over the same demo, so it reuses the parse timeout and
// max-retry ceiling.
func NewScanRosterTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(ScanRosterPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeScanRoster, payload,
		asynq.Timeout(parseDemoTimeout),
		asynq.MaxRetry(parseDemoMaxRetry),
	), nil
}

// NewRecordDemoTask returns an Asynq task for recording a job. hudMode is
// optional; when non-empty it must be one of the known HUD modes and overrides
// the recorder default for this capture. segmentIDs, when non-empty, scopes the
// capture to those kill-plan segments (the caller validates the ids against the
// job's kill plan); empty records every segment. Because the ids are part of the
// payload, asynq dedup treats a task for one segment as distinct from another.
func NewRecordDemoTask(id uuid.UUID, hudMode string, segmentIDs []string) (*asynq.Task, error) {
	switch hudMode {
	case "", "gameplay", "clean", "deathnotices":
	default:
		return nil, fmt.Errorf("invalid hud mode %q", hudMode)
	}
	payload, err := json.Marshal(RecordDemoPayload{JobID: id, HUDMode: hudMode, SegmentIDs: segmentIDs})
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

// NewRenderVariantTask returns an Asynq task for rendering one named output
// variant for a job. musicKey is optional; when non-empty the render worker
// mixes the named track into the reel.
func NewRenderVariantTask(id uuid.UUID, variant, musicKey string, edit renderplan.EditRequest) (*asynq.Task, error) {
	if !renderVariantPattern.MatchString(variant) {
		return nil, fmt.Errorf("invalid render variant %q", variant)
	}
	if musicKey != "" && !renderVariantPattern.MatchString(musicKey) {
		return nil, fmt.Errorf("invalid music key %q", musicKey)
	}
	edit = renderplan.NormalizeEditRequest(edit)
	if err := edit.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(RenderVariantPayload{JobID: id, Variant: variant, MusicKey: musicKey, Edit: edit})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeRenderVariant, payload), nil
}

func NewCodexAgentTask(id uuid.UUID, variant, kind string) (*asynq.Task, error) {
	if !renderVariantPattern.MatchString(variant) {
		return nil, fmt.Errorf("invalid render variant %q", variant)
	}
	if !renderVariantPattern.MatchString(kind) {
		return nil, fmt.Errorf("invalid agent kind %q", kind)
	}
	payload, err := json.Marshal(CodexAgentPayload{JobID: id, Variant: variant, Kind: kind})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeCodexAgent, payload), nil
}

func NewRenderStreamClipTask(id uuid.UUID, variant string) (*asynq.Task, error) {
	if !renderVariantPattern.MatchString(variant) {
		return nil, fmt.Errorf("invalid stream render variant %q", variant)
	}
	payload, err := json.Marshal(RenderStreamClipPayload{JobID: id, Variant: variant})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeRenderStreamClip, payload), nil
}

// NewStreamAcquireTask returns an Asynq task that downloads a stream job's
// source video. Callers enqueue it with asynq.MaxRetry(0): acquisition is an
// expensive network step, so a failure is terminal for the user to retry
// explicitly rather than something Asynq should retry automatically.
func NewStreamAcquireTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(StreamAcquirePayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeStreamAcquire, payload), nil
}
