// Package tasks defines the Asynq task types and payloads shared between
// the orchestrator (producer) and the workers (consumer).
package tasks

import (
	"encoding/json"
	"fmt"
	"regexp"

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

	// TypeGenerateStreamCaptions produces durable subtitle candidates which a
	// human must review before a stream render may consume them.
	TypeGenerateStreamCaptions = "stream:captions"

	// TypeGenerateStreamKillfeed produces durable source-PTS event candidates.
	// Applying a ready generation is a separate, explicit plan mutation.
	TypeGenerateStreamKillfeed = "stream:killfeed"

	// TypeStreamAcquire is the Asynq task type for downloading a stream job's
	// source video from a URL (Twitch clip/VOD or any yt-dlp-supported site)
	// before it can be edited and rendered.
	TypeStreamAcquire = "stream:acquire"
)

var renderVariantPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const generateIntentHeader = "fragforge-generate-intent"
const streamCaptionGenerationHeader = "fragforge-stream-caption-generation"
const streamKillfeedGenerationHeader = "fragforge-stream-killfeed-generation"
const streamRenderIntentHeader = "fragforge-stream-render-intent"

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
	JobID                uuid.UUID `json:"job_id"`
	HUDMode              string    `json:"hud_mode,omitempty"`
	SegmentIDs           []string  `json:"segment_ids,omitempty"`
	PortraitSafeKillfeed bool      `json:"portrait_safe_killfeed,omitempty"`
}

// ComposeFinalPayload carries the job id for the composition worker.
type ComposeFinalPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

// RenderVariantPayload carries the job id and render variant requested by the
// orchestrator. MusicKey, when set, names a music track the render worker mixes
// into the reel (resolved from its music directory). MusicVolume is the music
// gain in (0,1]; 0 means the render default.
type RenderVariantPayload struct {
	JobID       uuid.UUID              `json:"job_id"`
	Variant     string                 `json:"variant"`
	MusicKey    string                 `json:"music_key,omitempty"`
	MusicVolume float64                `json:"music_volume,omitempty"`
	Edit        renderplan.EditRequest `json:"edit,omitempty"`
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

// StreamRenderIntent binds a queued Studio render to the exact edit plan and
// killfeed generation admitted by HTTP. It lives in a task header so Asynq's
// job+variant payload uniqueness remains stable while an older render is live.
type StreamRenderIntent struct {
	AttemptID           uuid.UUID `json:"attempt_id"`
	EditPlanFingerprint string    `json:"edit_plan_fingerprint"`
	KillfeedGeneration  uuid.UUID `json:"killfeed_generation_id,omitempty"`
	KillfeedFingerprint string    `json:"killfeed_fingerprint,omitempty"`
}

func (i StreamRenderIntent) Validate() error {
	if i.AttemptID == uuid.Nil {
		return fmt.Errorf("stream render attempt id is required")
	}
	if !sha256HexPattern.MatchString(i.EditPlanFingerprint) {
		return fmt.Errorf("stream render edit-plan fingerprint must be a sha256 hex digest")
	}
	hasGeneration := i.KillfeedGeneration != uuid.Nil
	hasFingerprint := i.KillfeedFingerprint != ""
	if hasGeneration != hasFingerprint {
		return fmt.Errorf("stream render killfeed generation and fingerprint must be provided together")
	}
	if hasFingerprint && !sha256HexPattern.MatchString(i.KillfeedFingerprint) {
		return fmt.Errorf("stream render killfeed fingerprint must be a sha256 hex digest")
	}
	return nil
}

type GenerateStreamCaptionsPayload struct {
	JobID uuid.UUID `json:"job_id"`
}

type GenerateStreamKillfeedPayload struct {
	JobID        uuid.UUID `json:"job_id"`
	GenerationID uuid.UUID `json:"generation_id"`
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
	return asynq.NewTask(TypeParseDemo, payload), nil
}

// NewScanRosterTask returns a task for the deterministic roster scan pass.
func NewScanRosterTask(id uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(ScanRosterPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeScanRoster, payload), nil
}

// NewRecordDemoTask returns an Asynq task for recording a job. hudMode is
// optional; when non-empty it must be one of the known HUD modes and overrides
// the recorder default for this capture. segmentIDs, when non-empty, scopes the
// capture to those kill-plan segments (the caller validates the ids against the
// job's kill plan); empty records every segment. Because the ids are part of the
// payload, asynq dedup treats a task for one segment as distinct from another.
func NewRecordDemoTask(id uuid.UUID, hudMode string, segmentIDs []string, portraitSafeKillfeed bool) (*asynq.Task, error) {
	return newRecordDemoTask(id, hudMode, segmentIDs, portraitSafeKillfeed, nil)
}

// NewGenerateRecordDemoTask returns a record task carrying the immutable render
// intent for that capture. The intent lives in task headers so uniqueness stays
// keyed by the capture payload (job, HUD, segments, and killfeed geometry).
func NewGenerateRecordDemoTask(id uuid.UUID, hudMode string, segmentIDs []string, portraitSafeKillfeed bool, intent renderplan.GenerateIntent) (*asynq.Task, error) {
	intent = intent.Normalize()
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	return newRecordDemoTask(id, hudMode, segmentIDs, portraitSafeKillfeed, map[string]string{
		generateIntentHeader: string(b),
	})
}

func newRecordDemoTask(id uuid.UUID, hudMode string, segmentIDs []string, portraitSafeKillfeed bool, headers map[string]string) (*asynq.Task, error) {
	switch hudMode {
	case "", "gameplay", "clean", "deathnotices":
	default:
		return nil, fmt.Errorf("invalid hud mode %q", hudMode)
	}
	payload, err := json.Marshal(RecordDemoPayload{
		JobID:                id,
		HUDMode:              hudMode,
		SegmentIDs:           segmentIDs,
		PortraitSafeKillfeed: portraitSafeKillfeed,
	})
	if err != nil {
		return nil, err
	}
	if len(headers) == 0 {
		return asynq.NewTask(TypeRecordDemo, payload), nil
	}
	return asynq.NewTaskWithHeaders(TypeRecordDemo, payload, headers), nil
}

// GenerateIntentFromTask returns the immutable one-click render intent carried
// by a generate record task. Plain record tasks return ok=false.
func GenerateIntentFromTask(task *asynq.Task) (intent renderplan.GenerateIntent, ok bool, err error) {
	if task == nil {
		return renderplan.GenerateIntent{}, false, fmt.Errorf("record task is nil")
	}
	raw, ok := task.Headers()[generateIntentHeader]
	if !ok || raw == "" {
		return renderplan.GenerateIntent{}, false, nil
	}
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return renderplan.GenerateIntent{}, false, fmt.Errorf("decode generate intent header: %w", err)
	}
	intent = intent.Normalize()
	if err := intent.Validate(); err != nil {
		return renderplan.GenerateIntent{}, false, fmt.Errorf("validate generate intent header: %w", err)
	}
	return intent, true, nil
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
// mixes the named track into the reel. musicVolume is the music gain in (0,1];
// 0 means the render default.
func NewRenderVariantTask(id uuid.UUID, variant, musicKey string, musicVolume float64, edit renderplan.EditRequest) (*asynq.Task, error) {
	if !renderVariantPattern.MatchString(variant) {
		return nil, fmt.Errorf("invalid render variant %q", variant)
	}
	if musicKey != "" && !renderVariantPattern.MatchString(musicKey) {
		return nil, fmt.Errorf("invalid music key %q", musicKey)
	}
	if musicVolume < 0 || musicVolume > 1 {
		return nil, fmt.Errorf("music volume must be between 0 and 1")
	}
	edit = renderplan.NormalizeEditRequest(edit)
	if err := edit.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(RenderVariantPayload{JobID: id, Variant: variant, MusicKey: musicKey, MusicVolume: musicVolume, Edit: edit})
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

// NewBoundRenderStreamClipTask returns the same job+variant payload as the
// legacy/CLI constructor while carrying Studio's immutable admitted intent in
// a header. Keeping the payload identical is important: task uniqueness must
// continue to serialize all render revisions for the same job and variant.
func NewBoundRenderStreamClipTask(id uuid.UUID, variant string, intent StreamRenderIntent) (*asynq.Task, error) {
	plain, err := NewRenderStreamClipTask(id, variant)
	if err != nil {
		return nil, err
	}
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeRenderStreamClip, plain.Payload(), map[string]string{
		streamRenderIntentHeader: string(b),
	}), nil
}

// StreamRenderIntentFromTask returns the immutable Studio admission intent.
// Plain CLI tasks intentionally return ok=false for backwards compatibility.
func StreamRenderIntentFromTask(task *asynq.Task) (intent StreamRenderIntent, ok bool, err error) {
	if task == nil {
		return StreamRenderIntent{}, false, fmt.Errorf("stream render task is nil")
	}
	raw, ok := task.Headers()[streamRenderIntentHeader]
	if !ok || raw == "" {
		return StreamRenderIntent{}, false, nil
	}
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return StreamRenderIntent{}, false, fmt.Errorf("decode stream render intent: %w", err)
	}
	if err := intent.Validate(); err != nil {
		return StreamRenderIntent{}, false, err
	}
	return intent, true, nil
}

func NewGenerateStreamCaptionsTask(id, generationID uuid.UUID) (*asynq.Task, error) {
	if generationID == uuid.Nil {
		return nil, fmt.Errorf("caption generation id is required")
	}
	payload, err := json.Marshal(GenerateStreamCaptionsPayload{JobID: id})
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeGenerateStreamCaptions, payload, map[string]string{
		streamCaptionGenerationHeader: generationID.String(),
	}), nil
}

func StreamCaptionGenerationFromTask(task *asynq.Task) (uuid.UUID, error) {
	if task == nil {
		return uuid.Nil, fmt.Errorf("caption generation task is nil")
	}
	raw := task.Headers()[streamCaptionGenerationHeader]
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid caption generation id: %w", err)
	}
	return id, nil
}

// NewGenerateStreamKillfeedTask carries the generation in both payload and
// header. Unlike captions, killfeed uniqueness must include the generation so
// a crop/bounds change can enqueue immediately while older work is still live.
func NewGenerateStreamKillfeedTask(id, generationID uuid.UUID) (*asynq.Task, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("killfeed analysis job id is required")
	}
	if generationID == uuid.Nil {
		return nil, fmt.Errorf("killfeed analysis generation id is required")
	}
	payload, err := json.Marshal(GenerateStreamKillfeedPayload{
		JobID:        id,
		GenerationID: generationID,
	})
	if err != nil {
		return nil, err
	}
	return asynq.NewTaskWithHeaders(TypeGenerateStreamKillfeed, payload, map[string]string{
		streamKillfeedGenerationHeader: generationID.String(),
	}), nil
}

func StreamKillfeedGenerationFromTask(task *asynq.Task) (uuid.UUID, error) {
	if task == nil {
		return uuid.Nil, fmt.Errorf("killfeed analysis task is nil")
	}
	raw := task.Headers()[streamKillfeedGenerationHeader]
	headerID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid killfeed analysis generation id: %w", err)
	}
	var payload GenerateStreamKillfeedPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return uuid.Nil, fmt.Errorf("decode killfeed analysis payload: %w", err)
	}
	if payload.GenerationID != headerID {
		return uuid.Nil, fmt.Errorf("killfeed analysis generation header does not match payload")
	}
	return headerID, nil
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
