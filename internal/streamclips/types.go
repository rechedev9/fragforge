// Package streamclips defines local streamer-MP4 clip jobs and render plans.
package streamclips

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	VariantStreamerVerticalStack = "streamer-vertical-stack"

	StatusUploaded  Status = "uploaded"
	StatusReady     Status = "ready"
	StatusRendering Status = "rendering"
	StatusRendered  Status = "rendered"
	StatusFailed    Status = "failed"
)

var clipIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Status string

func ParseStatus(value string) (Status, error) {
	switch Status(value) {
	case StatusUploaded, StatusReady, StatusRendering, StatusRendered, StatusFailed:
		return Status(value), nil
	default:
		return "", fmt.Errorf("unknown stream job status %q", value)
	}
}

type Job struct {
	ID            uuid.UUID       `json:"id"`
	Status        Status          `json:"status"`
	FailureReason string          `json:"failure_reason,omitempty"`
	SourcePath    string          `json:"source_path"`
	SourceSHA256  string          `json:"source_sha256"`
	Title         string          `json:"title,omitempty"`
	Probe         SourceProbe     `json:"probe"`
	EditPlan      json.RawMessage `json:"edit_plan,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
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
	SchemaVersion string      `json:"schema_version"`
	Variant       string      `json:"variant"`
	FaceCrop      CropRect    `json:"face_crop"`
	GameplayCrop  CropRect    `json:"gameplay_crop"`
	Clips         []ClipRange `json:"clips"`
	UpdatedAt     time.Time   `json:"updated_at"`
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
	return EditPlan{
		SchemaVersion: "1.0",
		Variant:       VariantStreamerVerticalStack,
		FaceCrop:      CropRect{X: 0, Y: 0, Width: 1, Height: 0.35},
		GameplayCrop:  CropRect{X: 0, Y: 0.35, Width: 1, Height: 0.65},
		Clips:         []ClipRange{},
		UpdatedAt:     time.Now().UTC(),
	}
}

func (p EditPlan) Validate() error {
	if p.Variant == "" {
		return fmt.Errorf("variant is required")
	}
	if p.Variant != VariantStreamerVerticalStack {
		return fmt.Errorf("unsupported stream render variant %q", p.Variant)
	}
	if err := p.FaceCrop.Validate("face_crop"); err != nil {
		return err
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
		plan.Variant = VariantStreamerVerticalStack
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = time.Now().UTC()
	}
	for i := range plan.Clips {
		plan.Clips[i].ID = strings.TrimSpace(plan.Clips[i].ID)
	}
	return plan
}
