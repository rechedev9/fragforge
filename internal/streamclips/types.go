// Package streamclips defines local streamer-MP4 clip jobs and render plans.
package streamclips

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
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
	ID              string           `json:"id"`
	StartSeconds    float64          `json:"start_seconds"`
	EndSeconds      float64          `json:"end_seconds"`
	KillfeedSeconds []float64        `json:"killfeed_seconds,omitempty"`
	KillfeedKills   [][]KillfeedKill `json:"killfeed_kills,omitempty"`
	Title           string           `json:"title,omitempty"`
}

// KillfeedKill is one confirmed CS2 kill notice, either read from the cue
// frame by the xAI vision reader or entered by the user in the web editor.
// It mirrors the community killfeed CSV schema so imports stay trivial.
// Weapon is a key from WeaponKeys (the embedded notice icon catalog).
type KillfeedKill struct {
	AttackerSide string `json:"attacker_side"` // "CT" or "T"
	AttackerName string `json:"attacker_name"`
	VictimSide   string `json:"victim_side"` // "CT" or "T"
	VictimName   string `json:"victim_name"`
	AssisterSide string `json:"assister_side,omitempty"`
	AssisterName string `json:"assister_name,omitempty"`
	Weapon       string `json:"weapon"`
	Headshot     bool   `json:"headshot,omitempty"`
	Wallbang     bool   `json:"wallbang,omitempty"`
	Noscope      bool   `json:"noscope,omitempty"`
	Smoke        bool   `json:"smoke,omitempty"`
	Blind        bool   `json:"blind,omitempty"`
	InAir        bool   `json:"in_air,omitempty"`
	FlashAssist  bool   `json:"flash_assist,omitempty"`
}

type EditPlan struct {
	SchemaVersion  string             `json:"schema_version"`
	Variant        string             `json:"variant"`
	FaceCrop       CropRect           `json:"face_crop"`
	GameplayCrop   CropRect           `json:"gameplay_crop"`
	KillfeedCrop   *CropRect          `json:"killfeed_crop,omitempty"`
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
	if p.KillfeedCrop != nil {
		if err := p.KillfeedCrop.Validate("killfeed_crop"); err != nil {
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
		// Kills are index-aligned with killfeed_seconds (enforced in
		// ClipRange.Validate), so a clip with kills always has cues and this
		// single check covers both.
		if p.KillfeedCrop == nil && len(clip.KillfeedSeconds) > 0 {
			return fmt.Errorf("clip %s has killfeed_seconds but killfeed_crop is not configured", clip.ID)
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
	if math.IsNaN(c.X) || math.IsInf(c.X, 0) ||
		math.IsNaN(c.Y) || math.IsInf(c.Y, 0) ||
		math.IsNaN(c.Width) || math.IsInf(c.Width, 0) ||
		math.IsNaN(c.Height) || math.IsInf(c.Height, 0) {
		return fmt.Errorf("%s must use finite normalized coordinates", label)
	}
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
	if math.IsNaN(c.StartSeconds) || math.IsInf(c.StartSeconds, 0) {
		return fmt.Errorf("clip %s start_seconds must be finite", c.ID)
	}
	if c.StartSeconds < 0 {
		return fmt.Errorf("clip %s start_seconds must be >= 0", c.ID)
	}
	if math.IsNaN(c.EndSeconds) || math.IsInf(c.EndSeconds, 0) {
		return fmt.Errorf("clip %s end_seconds must be finite", c.ID)
	}
	if c.EndSeconds <= c.StartSeconds {
		return fmt.Errorf("clip %s end_seconds must be greater than start_seconds", c.ID)
	}
	for _, cue := range c.KillfeedSeconds {
		if math.IsNaN(cue) || math.IsInf(cue, 0) {
			return fmt.Errorf("clip %s killfeed_seconds must contain only finite values", c.ID)
		}
		if cue < c.StartSeconds || cue >= c.EndSeconds {
			return fmt.Errorf(
				"clip %s killfeed cue %g must satisfy start_seconds <= cue < end_seconds",
				c.ID, cue,
			)
		}
	}
	if c.KillfeedKills != nil && len(c.KillfeedKills) != len(c.KillfeedSeconds) {
		return fmt.Errorf(
			"clip %s killfeed_kills length %d must match %d killfeed_seconds",
			c.ID, len(c.KillfeedKills), len(c.KillfeedSeconds),
		)
	}
	for _, cue := range c.KillfeedKills {
		for _, kill := range cue {
			if err := kill.validate(c.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// validate checks that a kill notice carries the names, team sides, and weapon
// key the synthetic notice renderer needs. It is tolerant of un-normalized case
// and whitespace so a plan validates the same before and after normalization.
func (k KillfeedKill) validate(clipID string) error {
	if strings.TrimSpace(k.AttackerName) == "" {
		return fmt.Errorf("clip %s killfeed kill attacker_name is required", clipID)
	}
	if strings.TrimSpace(k.VictimName) == "" {
		return fmt.Errorf("clip %s killfeed kill victim_name is required", clipID)
	}
	if !validKillSide(k.AttackerSide) {
		return fmt.Errorf("clip %s killfeed kill attacker_side %q must be CT or T", clipID, k.AttackerSide)
	}
	if !validKillSide(k.VictimSide) {
		return fmt.Errorf("clip %s killfeed kill victim_side %q must be CT or T", clipID, k.VictimSide)
	}
	if strings.TrimSpace(k.AssisterName) != "" && !validKillSide(k.AssisterSide) {
		return fmt.Errorf("clip %s killfeed kill assister_side %q must be CT or T", clipID, k.AssisterSide)
	}
	if !ValidWeaponKey(strings.ToLower(strings.TrimSpace(k.Weapon))) {
		return fmt.Errorf("clip %s killfeed kill weapon %q is not a known weapon", clipID, k.Weapon)
	}
	return nil
}

func validKillSide(side string) bool {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "CT", "T":
		return true
	default:
		return false
	}
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
	if len(plan.Clips) > 0 {
		plan.Clips = append([]ClipRange(nil), plan.Clips...)
	}
	for i := range plan.Clips {
		plan.Clips[i].ID = strings.TrimSpace(plan.Clips[i].ID)
		plan.Clips[i].KillfeedSeconds = normalizeKillfeedSeconds(plan.Clips[i].KillfeedSeconds)
		plan.Clips[i].KillfeedKills = normalizeKillfeedKills(plan.Clips[i].KillfeedKills)
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
func normalizeClipRange(clip ClipRange) ClipRange {
	clip.KillfeedSeconds = normalizeKillfeedSeconds(clip.KillfeedSeconds)
	clip.KillfeedKills = normalizeKillfeedKills(clip.KillfeedKills)
	return clip
}

// normalizeKillfeedKills trims and case-folds every kill's names, team sides,
// and weapon key. It deep-copies so the caller's slices are never mutated, and
// preserves nil cue entries so the result stays index-aligned with the cues.
func normalizeKillfeedKills(kills [][]KillfeedKill) [][]KillfeedKill {
	if len(kills) == 0 {
		return kills
	}
	out := make([][]KillfeedKill, len(kills))
	for i, cue := range kills {
		if cue == nil {
			continue
		}
		normalized := make([]KillfeedKill, len(cue))
		for j, kill := range cue {
			normalized[j] = normalizeKill(kill)
		}
		out[i] = normalized
	}
	return out
}

func normalizeKill(k KillfeedKill) KillfeedKill {
	k.AttackerSide = strings.ToUpper(strings.TrimSpace(k.AttackerSide))
	k.VictimSide = strings.ToUpper(strings.TrimSpace(k.VictimSide))
	k.AssisterSide = strings.ToUpper(strings.TrimSpace(k.AssisterSide))
	k.AttackerName = strings.TrimSpace(k.AttackerName)
	k.VictimName = strings.TrimSpace(k.VictimName)
	k.AssisterName = strings.TrimSpace(k.AssisterName)
	k.Weapon = strings.ToLower(strings.TrimSpace(k.Weapon))
	return k
}

func normalizeKillfeedSeconds(cues []float64) []float64 {
	if len(cues) == 0 {
		return cues
	}
	normalized := append([]float64(nil), cues...)
	sort.Float64s(normalized)
	writeIndex := 1
	for _, cue := range normalized[1:] {
		if cue == normalized[writeIndex-1] {
			continue
		}
		normalized[writeIndex] = cue
		writeIndex++
	}
	return normalized[:writeIndex]
}
