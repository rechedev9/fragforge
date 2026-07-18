package streamclips

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// KillfeedAnalysisStatus is the durable lifecycle of one temporal killfeed
// analysis generation. Candidate events are not renderable until Apply copies
// a ready generation into the edit plan.
type KillfeedAnalysisStatus string

const (
	KillfeedAnalysisNone           KillfeedAnalysisStatus = "none"
	KillfeedAnalysisQueued         KillfeedAnalysisStatus = "queued"
	KillfeedAnalysisAnalyzing      KillfeedAnalysisStatus = "analyzing"
	KillfeedAnalysisReviewRequired KillfeedAnalysisStatus = "review_required"
	KillfeedAnalysisReady          KillfeedAnalysisStatus = "ready"
	KillfeedAnalysisApplied        KillfeedAnalysisStatus = "applied"
	KillfeedAnalysisFailed         KillfeedAnalysisStatus = "failed"
)

// KillfeedEventMode records how precisely the source-video detector could
// locate an event. Empty Kills are valid for every exact mode: rendering uses
// the event's immutable captured row PNGs without requiring OCR enrichment.
type KillfeedEventMode string

const (
	KillfeedEventAlignedFrame KillfeedEventMode = "aligned_frame"
	KillfeedEventBurst        KillfeedEventMode = "burst"
	KillfeedEventUnresolved   KillfeedEventMode = "unresolved"
)

// KillfeedAnalysisState is the durable artifact for one generation. The
// fingerprint binds it to the source bytes, killfeed crop, and ordered clip
// bounds that the detector analyzed.
type KillfeedAnalysisState struct {
	JobID        uuid.UUID              `json:"job_id"`
	GenerationID uuid.UUID              `json:"generation_id"`
	Status       KillfeedAnalysisStatus `json:"status"`
	SourceSHA256 string                 `json:"source_sha256,omitempty"`
	KillfeedCrop CropRect               `json:"killfeed_crop,omitzero"`
	Fingerprint  string                 `json:"fingerprint,omitempty"`
	Clips        []KillfeedAnalysisClip `json:"clips"`
	Warnings     []string               `json:"warnings,omitempty"`
	Error        string                 `json:"error,omitempty"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type KillfeedAnalysisClip struct {
	ClipID       string                  `json:"clip_id"`
	StartSeconds float64                 `json:"start_seconds"`
	EndSeconds   float64                 `json:"end_seconds"`
	Events       []KillfeedAnalysisEvent `json:"events"`
	Warnings     []string                `json:"warnings,omitempty"`
	Error        string                  `json:"error,omitempty"`
}

// KillfeedAnalysisEvent keeps integer media timestamps as the source of truth.
// CueSeconds and SampleSeconds are derived compatibility fields used by the
// existing float-second edit plan and frame extractor.
type KillfeedAnalysisEvent struct {
	EventID       string                `json:"event_id"`
	SourcePTS     int64                 `json:"source_pts"`
	TimeBase      KillfeedTimeBase      `json:"time_base"`
	CueSeconds    float64               `json:"cue_seconds"`
	OnsetStartPTS int64                 `json:"onset_start_pts"`
	OnsetEndPTS   int64                 `json:"onset_end_pts"`
	SamplePTS     int64                 `json:"sample_pts"`
	SampleSeconds float64               `json:"sample_seconds"`
	Mode          KillfeedEventMode     `json:"mode"`
	Rows          []KillfeedRowEvidence `json:"rows"`
	Kills         []KillfeedKill        `json:"kills"`
	Warnings      []string              `json:"warnings,omitempty"`
	Error         string                `json:"error,omitempty"`
}

// KillfeedTimeBase and KillfeedRowEvidence intentionally mirror
// streamkillfeed.TimeBase and streamkillfeed.RowEvidence. streamkillfeed
// imports this package for plan/crop types, so a JSON-compatible boundary here
// avoids an import cycle while keeping the durable artifact lossless.
type KillfeedTimeBase struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

func (t KillfeedTimeBase) Validate() error {
	if t.Num <= 0 || t.Den <= 0 {
		return fmt.Errorf("time base must have positive numerator and denominator")
	}
	return nil
}

type KillfeedRowEvidence struct {
	OnsetRowIndex  int       `json:"onset_row_index"`
	SampleRowIndex int       `json:"sample_row_index"`
	Fingerprint    string    `json:"fingerprint"`
	OnsetBounds    NoticeRow `json:"onset_bounds"`
	SampleBounds   NoticeRow `json:"sample_bounds"`
}

// KillfeedAnalysisMetadata is server-owned edit-plan metadata proving which
// durable generation supplied the currently rendered killfeed events.
type KillfeedAnalysisMetadata struct {
	GenerationID uuid.UUID `json:"generation_id"`
	Fingerprint  string    `json:"fingerprint"`
	AppliedAt    time.Time `json:"applied_at"`
}

// KillfeedAnalysisFingerprint deterministically binds detector output to the
// source, crop, and ordered clip identity/bounds. Existing cue contents are
// deliberately excluded: they are the output being replaced by Apply.
func KillfeedAnalysisFingerprint(sourceSHA256 string, crop CropRect, clips []ClipRange) (string, error) {
	descriptors := make([]KillfeedAnalysisClip, len(clips))
	for i, clip := range clips {
		descriptors[i] = KillfeedAnalysisClip{
			ClipID:       clip.ID,
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
		}
	}
	return killfeedAnalysisFingerprint(sourceSHA256, crop, descriptors)
}

func killfeedAnalysisFingerprint(sourceSHA256 string, crop CropRect, clips []KillfeedAnalysisClip) (string, error) {
	if strings.TrimSpace(sourceSHA256) == "" {
		return "", fmt.Errorf("source sha256 is required")
	}
	if err := crop.Validate("killfeed_crop"); err != nil {
		return "", err
	}
	if len(clips) == 0 {
		return "", fmt.Errorf("at least one clip is required")
	}
	type clipDescriptor struct {
		ClipID       string  `json:"clip_id"`
		StartSeconds float64 `json:"start_seconds"`
		EndSeconds   float64 `json:"end_seconds"`
	}
	payload := struct {
		SourceSHA256 string           `json:"source_sha256"`
		KillfeedCrop CropRect         `json:"killfeed_crop"`
		Clips        []clipDescriptor `json:"clips"`
	}{
		SourceSHA256: strings.ToLower(strings.TrimSpace(sourceSHA256)),
		KillfeedCrop: crop,
		Clips:        make([]clipDescriptor, len(clips)),
	}
	for i, clip := range clips {
		if err := validateKillfeedAnalysisClipBounds(clip.ClipID, clip.StartSeconds, clip.EndSeconds); err != nil {
			return "", err
		}
		payload.Clips[i] = clipDescriptor{
			ClipID:       strings.TrimSpace(clip.ClipID),
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
		}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode killfeed analysis fingerprint: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s KillfeedAnalysisState) Validate() error {
	switch s.Status {
	case KillfeedAnalysisNone:
		if s.GenerationID != uuid.Nil || s.Fingerprint != "" {
			return fmt.Errorf("none killfeed analysis must not identify a generation")
		}
		return nil
	case KillfeedAnalysisQueued, KillfeedAnalysisAnalyzing,
		KillfeedAnalysisReviewRequired, KillfeedAnalysisReady,
		KillfeedAnalysisApplied, KillfeedAnalysisFailed:
	default:
		return fmt.Errorf("unknown killfeed analysis status %q", s.Status)
	}
	if s.JobID == uuid.Nil {
		return fmt.Errorf("killfeed analysis job id is required")
	}
	if s.GenerationID == uuid.Nil {
		return fmt.Errorf("killfeed analysis generation id is required")
	}
	if s.UpdatedAt.IsZero() {
		return fmt.Errorf("killfeed analysis updated_at is required")
	}
	if s.Clips == nil {
		return fmt.Errorf("killfeed analysis clips must be an array")
	}
	wantFingerprint, err := killfeedAnalysisFingerprint(s.SourceSHA256, s.KillfeedCrop, s.Clips)
	if err != nil {
		return fmt.Errorf("validate killfeed analysis inputs: %w", err)
	}
	if s.Fingerprint != wantFingerprint {
		return fmt.Errorf("killfeed analysis fingerprint does not match source, crop, and clip bounds")
	}
	seenClips := make(map[string]struct{}, len(s.Clips))
	seenEvents := make(map[string]struct{})
	for _, clip := range s.Clips {
		clipID := strings.TrimSpace(clip.ClipID)
		if _, duplicate := seenClips[clipID]; duplicate {
			return fmt.Errorf("duplicate killfeed analysis clip %q", clip.ClipID)
		}
		seenClips[clipID] = struct{}{}
		if clip.Events == nil {
			return fmt.Errorf("clip %s killfeed events must be an array", clip.ClipID)
		}
		lastCue := math.Inf(-1)
		for _, event := range clip.Events {
			if err := event.validate(clip); err != nil {
				return err
			}
			if _, duplicate := seenEvents[event.EventID]; duplicate {
				return fmt.Errorf("duplicate killfeed event id %q", event.EventID)
			}
			seenEvents[event.EventID] = struct{}{}
			if event.CueSeconds <= lastCue {
				return fmt.Errorf("clip %s killfeed events must use strictly increasing cue_seconds", clip.ClipID)
			}
			lastCue = event.CueSeconds
		}
	}
	return nil
}

func (m KillfeedAnalysisMetadata) Validate() error {
	if m.GenerationID == uuid.Nil {
		return fmt.Errorf("killfeed analysis metadata generation id is required")
	}
	if len(m.Fingerprint) != sha256.Size*2 {
		return fmt.Errorf("killfeed analysis metadata fingerprint must be a sha256 hex digest")
	}
	if _, err := hex.DecodeString(m.Fingerprint); err != nil {
		return fmt.Errorf("killfeed analysis metadata fingerprint must be a sha256 hex digest")
	}
	if m.AppliedAt.IsZero() {
		return fmt.Errorf("killfeed analysis metadata applied_at is required")
	}
	return nil
}

func validateKillfeedAnalysisClipBounds(id string, start, end float64) error {
	id = strings.TrimSpace(id)
	if !clipIDPattern.MatchString(id) {
		return fmt.Errorf("invalid killfeed analysis clip id %q", id)
	}
	if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 {
		return fmt.Errorf("killfeed analysis clip %s start_seconds must be finite and >= 0", id)
	}
	if math.IsNaN(end) || math.IsInf(end, 0) || end <= start {
		return fmt.Errorf("killfeed analysis clip %s end_seconds must be finite and greater than start_seconds", id)
	}
	return nil
}

func (e KillfeedAnalysisEvent) validate(clip KillfeedAnalysisClip) error {
	e.EventID = strings.TrimSpace(e.EventID)
	if e.EventID == "" {
		return fmt.Errorf("clip %s killfeed event id is required", clip.ClipID)
	}
	if err := e.TimeBase.Validate(); err != nil {
		return fmt.Errorf("clip %s killfeed event %s: %w", clip.ClipID, e.EventID, err)
	}
	if e.OnsetEndPTS < e.OnsetStartPTS || e.OnsetEndPTS != e.SourcePTS {
		return fmt.Errorf("clip %s killfeed event %s source_pts must equal onset_end_pts", clip.ClipID, e.EventID)
	}
	if e.SamplePTS < e.SourcePTS {
		return fmt.Errorf("clip %s killfeed event %s sample_pts must be >= source_pts", clip.ClipID, e.EventID)
	}
	if math.IsNaN(e.CueSeconds) || math.IsInf(e.CueSeconds, 0) ||
		e.CueSeconds < clip.StartSeconds || e.CueSeconds >= clip.EndSeconds {
		return fmt.Errorf("clip %s killfeed event %s cue_seconds must fall inside the clip", clip.ClipID, e.EventID)
	}
	if math.IsNaN(e.SampleSeconds) || math.IsInf(e.SampleSeconds, 0) ||
		e.SampleSeconds < e.CueSeconds || e.SampleSeconds >= clip.EndSeconds {
		return fmt.Errorf("clip %s killfeed event %s sample_seconds must be >= cue_seconds and inside the clip", clip.ClipID, e.EventID)
	}
	switch e.Mode {
	case KillfeedEventAlignedFrame, KillfeedEventBurst, KillfeedEventUnresolved:
	default:
		return fmt.Errorf("clip %s killfeed event %s has unknown mode %q", clip.ClipID, e.EventID, e.Mode)
	}
	if len(e.Rows) == 0 {
		return fmt.Errorf("clip %s killfeed event %s rows are required", clip.ClipID, e.EventID)
	}
	if e.Kills == nil {
		return fmt.Errorf("clip %s killfeed event %s kills must be an array", clip.ClipID, e.EventID)
	}
	for _, row := range e.Rows {
		if row.OnsetRowIndex < 0 || row.SampleRowIndex < 0 || strings.TrimSpace(row.Fingerprint) == "" {
			return fmt.Errorf("clip %s killfeed event %s has invalid row evidence", clip.ClipID, e.EventID)
		}
		if row.OnsetBounds.Width <= 0 || row.OnsetBounds.Height <= 0 ||
			row.SampleBounds.Width <= 0 || row.SampleBounds.Height <= 0 {
			return fmt.Errorf("clip %s killfeed event %s row bounds must be positive", clip.ClipID, e.EventID)
		}
	}
	for _, kill := range e.Kills {
		if err := kill.validate(clip.ClipID); err != nil {
			return err
		}
	}
	return nil
}
