// Package streamclips defines local streamer-MP4 clip jobs and render plans.
package streamclips

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when no stream job has the requested id.
var ErrNotFound = errors.New("stream job not found")

const (
	VariantStreamerVerticalStack = "streamer-vertical-stack"

	StatusAcquiring Status = "acquiring"
	StatusUploaded  Status = "uploaded"
	StatusReady     Status = "ready"
	StatusRendering Status = "rendering"
	StatusRendered  Status = "rendered"
	StatusFailed    Status = "failed"
)

var clipIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// Twitch-compatible streamer names keep the value safe to embed directly in
// FFmpeg's drawtext filter while covering the handles the banner is designed
// for. Twitch usernames are at most 25 ASCII letters, digits, or underscores.
var streamerNickPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,25}$`)

type Status string

func ParseStatus(value string) (Status, error) {
	switch Status(value) {
	case StatusAcquiring, StatusUploaded, StatusReady, StatusRendering, StatusRendered, StatusFailed:
		return Status(value), nil
	default:
		return "", fmt.Errorf("unknown stream job status %q", value)
	}
}

type Job struct {
	ID            uuid.UUID `json:"id"`
	Status        Status    `json:"status"`
	FailureReason string    `json:"failure_reason,omitempty"`
	SourcePath    string    `json:"source_path"`
	SourceSHA256  string    `json:"source_sha256"`
	// SourceURL is set when the job was created from POST /api/stream-jobs
	// with a source_url instead of a multipart upload; the acquire worker
	// reads it to know what to download. Empty for uploaded jobs.
	SourceURL string          `json:"source_url,omitempty"`
	Title     string          `json:"title,omitempty"`
	Probe     SourceProbe     `json:"probe"`
	EditPlan  json.RawMessage `json:"edit_plan,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type SourceProbe struct {
	Width           int      `json:"width,omitempty"`
	Height          int      `json:"height,omitempty"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	VideoCodec      string   `json:"video_codec,omitempty"`
	AudioCodec      string   `json:"audio_codec,omitempty"`
	FrameRate       string   `json:"frame_rate,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

type CropRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type ClipRange struct {
	ID           string  `json:"id"`
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Title        string  `json:"title,omitempty"`
}

type EditPlan struct {
	SchemaVersion  string             `json:"schema_version"`
	Variant        string             `json:"variant"`
	FaceCrop       CropRect           `json:"face_crop"`
	GameplayCrop   CropRect           `json:"gameplay_crop"`
	Clips          []ClipRange        `json:"clips"`
	StreamerBanner StreamerBannerPlan `json:"streamer_banner,omitzero"`
	Captions       CaptionsPlan       `json:"captions,omitzero"`
	Music          MusicPlan          `json:"music,omitzero"`
	Effects        EffectsPlan        `json:"effects,omitzero"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// StreamerBannerPlan adds an optional branded separator to the rendered
// vertical clip. An empty Nick keeps the render visually unchanged.
type StreamerBannerPlan struct {
	Nick         string   `json:"nick,omitempty"`
	PositionY    *float64 `json:"position_y,omitempty"`
	SlideEnabled bool     `json:"slide_enabled,omitempty"`
}

// CaptionsPlan opts a stream render into a burned-in karaoke caption pass.
// Language is a whisper language code ("es", "en", ...); empty means "auto".
// Nothing is required when Enabled is false.
type CaptionsPlan struct {
	Enabled  bool   `json:"enabled"`
	Language string `json:"language,omitempty"`
}

// defaultMusicVolume is the music gain mixed under the clip's original audio
// when the plan selects a track without an explicit volume: loud enough to
// carry the edit, quiet enough that the streamer stays intelligible.
const defaultMusicVolume = 0.25

// musicKeyPattern matches a music catalog track id (same shape the songs API
// serves); it doubles as path-traversal defence since a valid key can never
// contain a separator or "..".
var musicKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// MusicPlan mixes a catalog track from the orchestrator's music dir under the
// clip's original audio. Key is the track id ("concrete-teeth"); empty means
// no music. Volume is the music gain in (0,1]; 0 means the default.
type MusicPlan struct {
	Key    string  `json:"key,omitempty"`
	Volume float64 `json:"volume,omitempty"`
}

// EffectsPlan opts a render into light, deterministic post effects. Grade
// applies the mild contrast/saturation lift used across FragForge's viral
// presets; heavier looks are deliberately not offered.
type EffectsPlan struct {
	Grade bool `json:"grade,omitempty"`
}

type RenderState struct {
	JobID       uuid.UUID    `json:"job_id"`
	Variant     string       `json:"variant"`
	Status      Status       `json:"status"`
	ResultKey   string       `json:"result_key"`
	GalleryKey  string       `json:"gallery_key"`
	ArtifactDir string       `json:"artifact_dir"`
	Warnings    []string     `json:"warnings,omitempty"`
	Error       string       `json:"error,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Videos      []VideoEntry `json:"videos,omitempty"`
}

type RenderResult struct {
	SchemaVersion string       `json:"schema_version"`
	JobID         uuid.UUID    `json:"job_id"`
	Variant       string       `json:"variant"`
	Clips         []VideoEntry `json:"clips"`
	Warnings      []string     `json:"warnings,omitempty"`
	Error         string       `json:"error,omitempty"`
	RenderedAt    time.Time    `json:"rendered_at"`
}

type VideoEntry struct {
	ClipID          string  `json:"clip_id"`
	Title           string  `json:"title,omitempty"`
	Key             string  `json:"key"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

func NewVideoEntry(clip ClipRange, key string) VideoEntry {
	return VideoEntry{
		ClipID:          clip.ID,
		Title:           clip.Title,
		Key:             key,
		DurationSeconds: clip.EndSeconds - clip.StartSeconds,
	}
}

func NewRenderResult(id uuid.UUID, variant string, videos []VideoEntry, renderedAt time.Time) (RenderResult, error) {
	if _, err := RenderPrefix(id, variant); err != nil {
		return RenderResult{}, err
	}
	if renderedAt.IsZero() {
		renderedAt = time.Now()
	}
	return RenderResult{
		SchemaVersion: "1.0",
		JobID:         id,
		Variant:       variant,
		Clips:         append([]VideoEntry(nil), videos...),
		RenderedAt:    renderedAt.UTC(),
	}, nil
}

func NewRenderState(id uuid.UUID, variant string, status Status, warnings []string, errMsg string, videos []VideoEntry) (RenderState, error) {
	resultKey, err := RenderResultKey(id, variant)
	if err != nil {
		return RenderState{}, err
	}
	galleryKey, err := RenderGalleryKey(id, variant)
	if err != nil {
		return RenderState{}, err
	}
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return RenderState{}, err
	}
	return RenderState{
		JobID:       id,
		Variant:     variant,
		Status:      status,
		ResultKey:   resultKey,
		GalleryKey:  galleryKey,
		ArtifactDir: prefix,
		Warnings:    append([]string(nil), warnings...),
		Error:       errMsg,
		Videos:      append([]VideoEntry(nil), videos...),
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

func DefaultEditPlan() EditPlan {
	variant := DefaultVariant()
	return EditPlan{
		SchemaVersion: "1.0",
		Variant:       variant.Name,
		FaceCrop:      variant.DefaultFaceCrop,
		GameplayCrop:  variant.DefaultGameplayCrop,
		Clips:         []ClipRange{},
		UpdatedAt:     time.Now().UTC(),
	}
}

func (p EditPlan) Validate() error {
	if p.Variant == "" {
		return fmt.Errorf("variant is required")
	}
	layout, ok := VariantByName(p.Variant)
	if !ok {
		return unknownVariantError(p.Variant)
	}
	if !layout.FullFrame {
		if err := p.FaceCrop.Validate("face_crop"); err != nil {
			return err
		}
	}
	if err := p.GameplayCrop.Validate("gameplay_crop"); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, clip := range p.Clips {
		if err := clip.Validate(); err != nil {
			return err
		}
		if seen[clip.ID] {
			return fmt.Errorf("duplicate clip id %q", clip.ID)
		}
		seen[clip.ID] = true
	}
	if p.Music.Key != "" && !musicKeyPattern.MatchString(p.Music.Key) {
		return fmt.Errorf("invalid music key %q", p.Music.Key)
	}
	if p.Music.Volume < 0 || p.Music.Volume > 1 {
		return fmt.Errorf("music volume must be between 0 and 1")
	}
	if p.StreamerBanner.Nick != "" && !streamerNickPattern.MatchString(p.StreamerBanner.Nick) {
		return fmt.Errorf("streamer banner nick must use 1-25 letters, numbers, or underscores")
	}
	if positionY := p.StreamerBanner.PositionY; positionY != nil {
		if math.IsNaN(*positionY) || math.IsInf(*positionY, 0) || *positionY < 0.025 || *positionY > 0.975 {
			return fmt.Errorf("streamer banner position_y must be finite and between 0.025 and 0.975")
		}
	}
	return nil
}

func (c CropRect) Validate(label string) error {
	if c.X < 0 || c.Y < 0 || c.Width <= 0 || c.Height <= 0 {
		return fmt.Errorf("%s must use positive normalized coordinates", label)
	}
	if c.X+c.Width > 1 || c.Y+c.Height > 1 {
		return fmt.Errorf("%s must stay within the source frame", label)
	}
	return nil
}

func (c ClipRange) Validate() error {
	if !clipIDPattern.MatchString(c.ID) {
		return fmt.Errorf("invalid clip id %q", c.ID)
	}
	if c.StartSeconds < 0 {
		return fmt.Errorf("clip %s start_seconds must be >= 0", c.ID)
	}
	if c.EndSeconds <= c.StartSeconds {
		return fmt.Errorf("clip %s end_seconds must be greater than start_seconds", c.ID)
	}
	return nil
}

func NormalizeEditPlan(plan EditPlan) EditPlan {
	if plan.SchemaVersion == "" {
		plan.SchemaVersion = "1.0"
	}
	if plan.Variant == "" {
		plan.Variant = DefaultVariant().Name
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = time.Now().UTC()
	}
	for i := range plan.Clips {
		plan.Clips[i].ID = strings.TrimSpace(plan.Clips[i].ID)
	}
	plan.StreamerBanner.Nick = strings.TrimSpace(plan.StreamerBanner.Nick)
	plan.Music.Key = strings.TrimSpace(plan.Music.Key)
	if plan.Music.Key == "" {
		plan.Music.Volume = 0
	} else if plan.Music.Volume == 0 {
		plan.Music.Volume = defaultMusicVolume
	}
	return plan
}
