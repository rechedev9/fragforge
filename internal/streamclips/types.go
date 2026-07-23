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

const (
	// RenderErrorCodeKillfeedArtifactsStale marks a recoverable render failure:
	// the exact source-backed row captures are missing or corrupt, so the user
	// can remain in the editor and regenerate killfeed analysis.
	RenderErrorCodeKillfeedArtifactsStale = "killfeed_artifacts_stale"
	// RenderErrorCodeSuperseded marks an admitted render whose immutable plan
	// or analysis generation changed before it could commit. The job remains
	// editable and can be rendered again.
	RenderErrorCodeSuperseded = "render_superseded"
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
	// SourceURL is the private, short-lived acquisition URL used by the worker.
	// It may contain provider query material and is never serialized. Durable
	// repositories clear it on acquisition success or terminal failure.
	SourceURL string `json:"-"`
	// PublicSourceURL is the credential-free provider URL returned by APIs and
	// retained after the private acquisition URL has been cleared.
	PublicSourceURL string          `json:"source_url,omitempty"`
	Title           string          `json:"title,omitempty"`
	Probe           SourceProbe     `json:"probe"`
	EditPlan        json.RawMessage `json:"edit_plan,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type SourceProbe struct {
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	VideoCodec      string  `json:"video_codec,omitempty"`
	AudioCodec      string  `json:"audio_codec,omitempty"`
	FrameRate       string  `json:"frame_rate,omitempty"`
	// VideoTimeBase is the selected video stream's ffprobe time_base (for
	// example "1/30000"). StartTimeSeconds is format.start_time: the container
	// timestamp represented by source/UI time zero. Frame-aware analyzers map a
	// video PTS to that timeline as PTS*time_base-StartTimeSeconds.
	//
	// VideoStartTimeSeconds is the selected video stream's stream.start_time.
	// It can be later than StartTimeSeconds when, for example, audio begins
	// before the first video frame; it is retained as source metadata and must
	// not replace the container timeline origin.
	VideoTimeBase         string   `json:"video_time_base,omitempty"`
	StartTimeSeconds      float64  `json:"start_time_seconds,omitempty"`
	VideoStartTimeSeconds float64  `json:"video_start_time_seconds,omitempty"`
	Warnings              []string `json:"warnings,omitempty"`
}

type CropRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// KillfeedCueOrigin records whether a cue came from source-frame analysis or
// was authored manually. Rendering uses this durable provenance instead of
// inferring origin from whether OCR/reviewed kills happen to be present.
type KillfeedCueOrigin string

const (
	KillfeedCueAutomatic KillfeedCueOrigin = "automatic"
	KillfeedCueManual    KillfeedCueOrigin = "manual"
)

// KillfeedCueProvenance is keyed by CueSeconds rather than slice position so
// clients can sort, add, or remove cues without silently reassigning origin.
// EventID is filled once an automatic cue is bound to exact captured evidence;
// CLI detection may leave it empty until synchronous render analysis runs.
type KillfeedCueProvenance struct {
	CueSeconds float64           `json:"cue_seconds"`
	Origin     KillfeedCueOrigin `json:"origin"`
	EventID    string            `json:"event_id,omitempty"`
}

type ClipRange struct {
	ID              string    `json:"id"`
	StartSeconds    float64   `json:"start_seconds"`
	EndSeconds      float64   `json:"end_seconds"`
	KillfeedSeconds []float64 `json:"killfeed_seconds,omitempty"`
	// KillfeedKills is index-aligned with KillfeedSeconds. Each inner slice
	// contains only the notices born at that cue, not a cumulative snapshot.
	KillfeedKills [][]KillfeedKill `json:"killfeed_kills,omitempty"`
	// KillfeedCueProvenance independently identifies automatic versus manual
	// cues. It is intentionally not index-aligned: CueSeconds is the key.
	KillfeedCueProvenance []KillfeedCueProvenance `json:"killfeed_cue_provenance,omitempty"`
	// CaptionWords are reviewed Spanish word cues relative to this clip's
	// source range. When present they are burned directly and no cloud
	// transcription key is required.
	CaptionWords []CaptionWord `json:"caption_words,omitempty"`
	// CaptionReviewed distinguishes an intentionally empty, human-reviewed
	// no-speech clip from one that still needs transcription.
	CaptionReviewed bool      `json:"caption_reviewed,omitempty"`
	Title           string    `json:"title,omitempty"`
	Edit            *ClipEdit `json:"edit,omitempty"`
}

// Clip edit limits. Speed stays within what chained atempo filters reproduce
// faithfully; fades and overlay text are bounded so a plan can never produce a
// degenerate render.
const (
	minClipSpeed           = 0.25
	maxClipSpeed           = 3
	maxSourceVolume        = 2
	maxClipFadeSeconds     = 5
	maxTextOverlaysPerClip = 4
	maxTextOverlayRunes    = 120
	minOverlayFontSize     = 24
	maxOverlayFontSize     = 120
	defaultOverlayFontSize = 64
	// Vertical center bounds shared by the streamer banner and text overlays:
	// the drag-handle margin that keeps either strip fully inside the frame.
	minVerticalPositionY = 0.025
	maxVerticalPositionY = 0.975
)

// ClipEdit carries the optional per-clip edit options: playback speed, the
// original-audio gain, fades, and burned-in text overlays. A nil or zero value
// renders the clip exactly as before the edit options existed.
type ClipEdit struct {
	// Speed is the playback rate in [0.25, 3]; 0 means unchanged (1x).
	Speed float64 `json:"speed,omitempty"`
	// SourceVolume scales the clip's original audio in [0, 2]; nil means
	// unchanged and 0 mutes the source (music, if any, still plays).
	SourceVolume *float64 `json:"source_volume,omitempty"`
	// FadeInSeconds / FadeOutSeconds fade video and audio at the clip
	// boundaries, measured in output (post-speed) seconds.
	FadeInSeconds  float64       `json:"fade_in_seconds,omitempty"`
	FadeOutSeconds float64       `json:"fade_out_seconds,omitempty"`
	TextOverlays   []TextOverlay `json:"text_overlays,omitempty"`
}

// TextOverlay burns a centered text line into the rendered clip. Times are
// relative to the clip start in source seconds (the same timeline the web
// preview scrubs); nil bounds extend to the clip edge.
type TextOverlay struct {
	Text string `json:"text"`
	// PositionY is the normalized vertical center in [0.025, 0.975].
	PositionY    float64  `json:"position_y"`
	StartSeconds *float64 `json:"start_seconds,omitempty"`
	EndSeconds   *float64 `json:"end_seconds,omitempty"`
	// FontSize in output pixels, [24, 120]; 0 means the default 64.
	FontSize int `json:"font_size,omitempty"`
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

// CaptionWord is one reviewed Spanish word on the clip-relative source
// timeline. It mirrors captions.WordCue without coupling saved edit plans to a
// renderer package.
type CaptionWord struct {
	Word         string  `json:"word"`
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
}

type EditPlan struct {
	SchemaVersion    string                    `json:"schema_version"`
	Variant          string                    `json:"variant"`
	FaceCrop         CropRect                  `json:"face_crop"`
	FaceCropReviewed bool                      `json:"face_crop_reviewed,omitempty"`
	GameplayCrop     CropRect                  `json:"gameplay_crop"`
	KillfeedCrop     *CropRect                 `json:"killfeed_crop,omitempty"`
	KillfeedAnalysis *KillfeedAnalysisMetadata `json:"killfeed_analysis,omitempty"`
	Clips            []ClipRange               `json:"clips"`
	StreamerBanner   StreamerBannerPlan        `json:"streamer_banner,omitzero"`
	Captions         CaptionsPlan              `json:"captions,omitzero"`
	Music            MusicPlan                 `json:"music,omitzero"`
	Effects          EffectsPlan               `json:"effects,omitzero"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

const EditPlanSchemaVersion = "1.1"

// StreamerBannerPlan adds an optional branded separator to the rendered
// vertical clip. An empty Nick keeps the render visually unchanged.
type StreamerBannerPlan struct {
	Nick         string   `json:"nick,omitempty"`
	PositionY    *float64 `json:"position_y,omitempty"`
	SlideEnabled bool     `json:"slide_enabled,omitempty"`
}

// CaptionsPlan opts a stream render into a burned-in karaoke caption pass.
// Subtitle output is always Spanish: Spanish speech is preserved and other
// recognized speech is translated. Language remains in the wire shape for
// saved-plan compatibility; new plans write "es" and rendering ignores older
// source-language values. Nothing is required when Enabled is false.
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
	JobID   uuid.UUID `json:"job_id"`
	Variant string    `json:"variant"`
	// AttemptID owns the mutable attempt status. It prevents an older queued
	// task from completing or failing a newer attempt for the same variant.
	AttemptID uuid.UUID `json:"attempt_id,omitempty"`
	Status    Status    `json:"status"`
	// Published means ResultKey, GalleryKey, ArtifactDir, and Videos identify
	// the last fully committed render. Those pointers remain valid while a
	// newer attempt is rendering or has failed.
	Published   bool            `json:"published,omitempty"`
	ResultKey   string          `json:"result_key"`
	GalleryKey  string          `json:"gallery_key"`
	ArtifactDir string          `json:"artifact_dir"`
	Warnings    []string        `json:"warnings,omitempty"`
	Error       string          `json:"error,omitempty"`
	ErrorCode   string          `json:"error_code,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Videos      []VideoEntry    `json:"videos,omitempty"`
	Delivery    []DeliveryEntry `json:"delivery,omitempty"`
}

// DeliveryEntry is one upload-ready asset inside shortslistosparasubir.
type DeliveryEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Key  string `json:"key"`
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
		DurationSeconds: clip.OutputDurationSeconds(),
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
		Published:   status == StatusRendered,
		ResultKey:   resultKey,
		GalleryKey:  galleryKey,
		ArtifactDir: prefix,
		Warnings:    append([]string(nil), warnings...),
		Error:       errMsg,
		Videos:      append([]VideoEntry(nil), videos...),
		UpdatedAt:   time.Now().UTC(),
	}, nil
}

// HasPublishedRender reports whether the state carries an active completed
// render. StatusRendered is accepted for compatibility with states written
// before Published was added.
func (s RenderState) HasPublishedRender() bool {
	return s.Published || s.Status == StatusRendered
}

// PreservePublishedRender copies only the immutable active-revision pointer
// from previous. Attempt status, warnings, and errors stay owned by the new
// state.
func (s *RenderState) PreservePublishedRender(previous RenderState) {
	if s == nil || !previous.HasPublishedRender() {
		return
	}
	s.Published = true
	s.ResultKey = previous.ResultKey
	s.GalleryKey = previous.GalleryKey
	s.ArtifactDir = previous.ArtifactDir
	s.Videos = append([]VideoEntry(nil), previous.Videos...)
	s.Delivery = append([]DeliveryEntry(nil), previous.Delivery...)
}

func DefaultEditPlan() EditPlan {
	variant := DefaultVariant()
	return EditPlan{
		SchemaVersion: EditPlanSchemaVersion,
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
	if p.KillfeedAnalysis != nil {
		if p.KillfeedCrop == nil {
			return fmt.Errorf("killfeed_analysis requires killfeed_crop")
		}
		if err := p.KillfeedAnalysis.Validate(); err != nil {
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
		if math.IsNaN(*positionY) || math.IsInf(*positionY, 0) || *positionY < minVerticalPositionY || *positionY > maxVerticalPositionY {
			return fmt.Errorf("streamer banner position_y must be finite and between 0.025 and 0.975")
		}
	}
	return nil
}

// ValidateForSourceDuration validates the edit plan and also proves every clip
// range fits inside the probed source media. A zero duration means the source
// has not been probed and preserves the structural-only validation behavior.
func (p EditPlan) ValidateForSourceDuration(durationSeconds float64) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds < 0 {
		return fmt.Errorf("source duration must be finite and >= 0")
	}
	if durationSeconds == 0 {
		return nil
	}
	const durationToleranceSeconds = 0.001
	for _, clip := range p.Clips {
		if clip.EndSeconds > durationSeconds+durationToleranceSeconds {
			return fmt.Errorf(
				"clip %s end_seconds %.3f exceeds source duration %.3f",
				clip.ID, clip.EndSeconds, durationSeconds,
			)
		}
	}
	return nil
}

const legacyInitialClipEndSeconds = 20.0

// MigrateLegacySourceDuration fits only the historical fixed 20-second clip
// endpoint to a shorter probed source. Older Studio versions persisted that
// default before media duration was loaded, so rejecting it during render
// would strand otherwise valid jobs after an upgrade. Other overruns remain
// untouched and fail ValidateForSourceDuration, preserving strict validation
// for newly submitted or genuinely invalid plans.
func MigrateLegacySourceDuration(plan EditPlan, durationSeconds float64) (EditPlan, bool) {
	const tolerance = 0.001
	if durationSeconds <= 0 || durationSeconds >= legacyInitialClipEndSeconds-tolerance ||
		math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) {
		return plan, false
	}

	plan = NormalizeEditPlan(plan)
	clips := make([]ClipRange, 0, len(plan.Clips))
	changed := false
	for _, clip := range plan.Clips {
		if math.Abs(clip.EndSeconds-legacyInitialClipEndSeconds) > tolerance || clip.EndSeconds <= durationSeconds+tolerance {
			clips = append(clips, clip)
			continue
		}
		changed = true
		if clip.StartSeconds >= durationSeconds {
			continue
		}
		clip.EndSeconds = durationSeconds
		if len(clip.KillfeedSeconds) > 0 {
			cues := make([]float64, 0, len(clip.KillfeedSeconds))
			alignedKills := clip.KillfeedKills != nil && len(clip.KillfeedKills) == len(clip.KillfeedSeconds)
			kills := clip.KillfeedKills
			if alignedKills {
				kills = make([][]KillfeedKill, 0, len(clip.KillfeedKills))
			}
			for i, cue := range clip.KillfeedSeconds {
				if cue < clip.StartSeconds || cue >= clip.EndSeconds {
					continue
				}
				cues = append(cues, cue)
				if alignedKills {
					kills = append(kills, clip.KillfeedKills[i])
				}
			}
			clip.KillfeedSeconds = cues
			clip.KillfeedKills = kills
		}
		clips = append(clips, clip)
	}
	if !changed {
		return plan, false
	}
	plan.Clips = clips
	return NormalizeEditPlan(plan), true
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
	seenProvenance := make(map[float64]struct{}, len(c.KillfeedCueProvenance))
	for _, provenance := range c.KillfeedCueProvenance {
		if math.IsNaN(provenance.CueSeconds) || math.IsInf(provenance.CueSeconds, 0) {
			return fmt.Errorf("clip %s killfeed cue provenance must use a finite cue_seconds", c.ID)
		}
		if _, duplicate := seenProvenance[provenance.CueSeconds]; duplicate {
			return fmt.Errorf("clip %s has duplicate killfeed cue provenance at %g", c.ID, provenance.CueSeconds)
		}
		seenProvenance[provenance.CueSeconds] = struct{}{}
		matchedCue := false
		for _, cue := range c.KillfeedSeconds {
			if cue == provenance.CueSeconds {
				matchedCue = true
				break
			}
		}
		if !matchedCue {
			return fmt.Errorf(
				"clip %s killfeed cue provenance %g has no matching killfeed_seconds entry",
				c.ID, provenance.CueSeconds,
			)
		}
		switch provenance.Origin {
		case KillfeedCueAutomatic:
		case KillfeedCueManual:
			if provenance.EventID != "" {
				return fmt.Errorf("clip %s manual killfeed cue %g must not identify an automatic event", c.ID, provenance.CueSeconds)
			}
		default:
			return fmt.Errorf("clip %s killfeed cue %g has unknown origin %q", c.ID, provenance.CueSeconds, provenance.Origin)
		}
	}
	lastEnd := 0.0
	for i, cue := range c.CaptionWords {
		word := strings.TrimSpace(cue.Word)
		if word == "" {
			return fmt.Errorf("clip %s caption word %d is blank", c.ID, i)
		}
		if len([]rune(word)) > 80 {
			return fmt.Errorf("clip %s caption word %d exceeds 80 characters", c.ID, i)
		}
		if strings.ContainsAny(word, "\r\n") {
			return fmt.Errorf("clip %s caption word %d contains a line break", c.ID, i)
		}
		if math.IsNaN(cue.StartSeconds) || math.IsInf(cue.StartSeconds, 0) || cue.StartSeconds < 0 {
			return fmt.Errorf("clip %s caption word %d start_seconds must be finite and >= 0", c.ID, i)
		}
		if math.IsNaN(cue.EndSeconds) || math.IsInf(cue.EndSeconds, 0) || cue.EndSeconds <= cue.StartSeconds {
			return fmt.Errorf("clip %s caption word %d end_seconds must be finite and greater than start_seconds", c.ID, i)
		}
		if cue.EndSeconds-cue.StartSeconds > 2.5 {
			return fmt.Errorf("clip %s caption word %d lasts more than 2.5 seconds", c.ID, i)
		}
		if cue.EndSeconds > c.EndSeconds-c.StartSeconds+0.001 {
			return fmt.Errorf("clip %s caption word %d exceeds the clip duration", c.ID, i)
		}
		if i > 0 && cue.StartSeconds < lastEnd {
			return fmt.Errorf("clip %s caption words overlap or are unsorted at index %d", c.ID, i)
		}
		lastEnd = cue.EndSeconds
	}
	if err := c.Edit.validate(c.ID, c.EndSeconds-c.StartSeconds); err != nil {
		return err
	}
	return nil
}

// speed returns the effective playback rate, treating nil and 0 as 1x.
func (e *ClipEdit) speed() float64 {
	if e == nil || e.Speed == 0 {
		return 1
	}
	return e.Speed
}

// OutputDurationSeconds is the rendered clip length after the speed edit.
func (c ClipRange) OutputDurationSeconds() float64 {
	return (c.EndSeconds - c.StartSeconds) / c.Edit.speed()
}

// EffectiveSpeed is the clip's playback rate with the unset default applied,
// so callers can map source-time positions onto the rendered output timeline.
func (c ClipRange) EffectiveSpeed() float64 {
	return c.Edit.speed()
}

// SourceAudioMuted reports whether the clip edit silences the original audio,
// which also means transcribing the source would caption inaudible speech.
func (c ClipRange) SourceAudioMuted() bool {
	return c.Edit != nil && c.Edit.SourceVolume != nil && *c.Edit.SourceVolume == 0
}

// HasTextOverlays reports whether any clip burns text overlays, which decides
// whether the render worker must resolve a font file up front.
func (p EditPlan) HasTextOverlays() bool {
	for _, clip := range p.Clips {
		if clip.Edit != nil && len(clip.Edit.TextOverlays) > 0 {
			return true
		}
	}
	return false
}

func (e *ClipEdit) validate(clipID string, sourceDuration float64) error {
	if e == nil {
		return nil
	}
	if e.Speed != 0 && (math.IsNaN(e.Speed) || e.Speed < minClipSpeed || e.Speed > maxClipSpeed) {
		return fmt.Errorf("clip %s speed must be between 0.25 and 3", clipID)
	}
	if v := e.SourceVolume; v != nil && (math.IsNaN(*v) || *v < 0 || *v > maxSourceVolume) {
		return fmt.Errorf("clip %s source_volume must be between 0 and 2", clipID)
	}
	if math.IsNaN(e.FadeInSeconds) || e.FadeInSeconds < 0 || e.FadeInSeconds > maxClipFadeSeconds {
		return fmt.Errorf("clip %s fade_in_seconds must be between 0 and 5", clipID)
	}
	if math.IsNaN(e.FadeOutSeconds) || e.FadeOutSeconds < 0 || e.FadeOutSeconds > maxClipFadeSeconds {
		return fmt.Errorf("clip %s fade_out_seconds must be between 0 and 5", clipID)
	}
	// Fades run in output time, so they must fit the sped-up duration.
	if e.FadeInSeconds+e.FadeOutSeconds > sourceDuration/e.speed() {
		return fmt.Errorf("clip %s fades must fit within the clip's output duration", clipID)
	}
	if len(e.TextOverlays) > maxTextOverlaysPerClip {
		return fmt.Errorf("clip %s has at most 4 text overlays", clipID)
	}
	for _, overlay := range e.TextOverlays {
		if err := overlay.validate(clipID, sourceDuration); err != nil {
			return err
		}
	}
	return nil
}

func (o TextOverlay) validate(clipID string, clipDuration float64) error {
	text := strings.TrimSpace(o.Text)
	if text == "" {
		return fmt.Errorf("clip %s text overlay text is required", clipID)
	}
	if len([]rune(text)) > maxTextOverlayRunes {
		return fmt.Errorf("clip %s text overlay text must be at most 120 characters", clipID)
	}
	for _, r := range text {
		// The render reads the text from a file with expansion=none, so every
		// printable character is safe; only control characters break layout.
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("clip %s text overlay text must not contain control characters", clipID)
		}
	}
	if math.IsNaN(o.PositionY) || math.IsInf(o.PositionY, 0) || o.PositionY < minVerticalPositionY || o.PositionY > maxVerticalPositionY {
		return fmt.Errorf("clip %s text overlay position_y must be finite and between 0.025 and 0.975", clipID)
	}
	if o.FontSize != 0 && (o.FontSize < minOverlayFontSize || o.FontSize > maxOverlayFontSize) {
		return fmt.Errorf("clip %s text overlay font_size must be between 24 and 120", clipID)
	}
	if s := o.StartSeconds; s != nil && (math.IsNaN(*s) || *s < 0 || *s >= clipDuration) {
		return fmt.Errorf("clip %s text overlay start_seconds must be inside the clip", clipID)
	}
	if e := o.EndSeconds; e != nil && (math.IsNaN(*e) || *e <= 0 || *e > clipDuration) {
		return fmt.Errorf("clip %s text overlay end_seconds must be inside the clip", clipID)
	}
	if o.StartSeconds != nil && o.EndSeconds != nil && *o.EndSeconds <= *o.StartSeconds {
		return fmt.Errorf("clip %s text overlay end_seconds must be greater than start_seconds", clipID)
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
	legacyKillfeedSnapshots := plan.SchemaVersion == "" || plan.SchemaVersion == "1.0"
	if legacyKillfeedSnapshots {
		plan.SchemaVersion = EditPlanSchemaVersion
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
	if plan.KillfeedAnalysis != nil {
		metadata := *plan.KillfeedAnalysis
		plan.KillfeedAnalysis = &metadata
	}
	for i := range plan.Clips {
		plan.Clips[i] = normalizeClipRange(plan.Clips[i])
		if legacyKillfeedSnapshots {
			plan.Clips[i].KillfeedKills = killfeedSnapshotsToEvents(plan.Clips[i].KillfeedKills)
		}
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

// killfeedSnapshotsToEvents migrates the cumulative snapshots stored by edit
// plan schema 1.0 into the birth-only event deltas consumed by schema 1.1. An
// empty entry means an unresolved frozen-crop cue, not an observed empty
// snapshot, so it does not reset the previous observed snapshot.
func killfeedSnapshotsToEvents(snapshots [][]KillfeedKill) [][]KillfeedKill {
	if len(snapshots) == 0 {
		return snapshots
	}
	events := make([][]KillfeedKill, len(snapshots))
	var previous map[KillfeedKill]struct{}
	for i, snapshot := range snapshots {
		if len(snapshot) == 0 {
			continue
		}
		current := make(map[KillfeedKill]struct{}, len(snapshot))
		for _, kill := range snapshot {
			if _, duplicate := current[kill]; duplicate {
				continue
			}
			current[kill] = struct{}{}
			if _, existed := previous[kill]; !existed {
				events[i] = append(events[i], kill)
			}
		}
		previous = current
	}
	return events
}

func normalizeClipRange(clip ClipRange) ClipRange {
	clip.KillfeedSeconds, clip.KillfeedKills = normalizeKillfeedPlanEntries(
		clip.KillfeedSeconds,
		clip.KillfeedKills,
	)
	clip.KillfeedCueProvenance = normalizeKillfeedCueProvenance(
		clip.KillfeedCueProvenance,
		clip.KillfeedSeconds,
	)
	clip.Edit = normalizeClipEdit(clip.Edit)
	if len(clip.CaptionWords) > 0 {
		clip.CaptionWords = append([]CaptionWord(nil), clip.CaptionWords...)
		for i := range clip.CaptionWords {
			clip.CaptionWords[i].Word = strings.TrimSpace(clip.CaptionWords[i].Word)
		}
	}
	return clip
}

// KillfeedProvenanceAt returns explicit durable provenance for one exact cue.
func (c ClipRange) KillfeedProvenanceAt(cue float64) (KillfeedCueProvenance, bool) {
	for _, provenance := range c.KillfeedCueProvenance {
		if provenance.CueSeconds == cue {
			return provenance, true
		}
	}
	return KillfeedCueProvenance{}, false
}

// CaptionsNeedBackend reports whether at least one audible clip still needs
// transcription because it has neither reviewed Spanish words nor a reviewed
// no-speech decision. Muted clips do not need captions because their source
// speech is not present in the output.
func (p EditPlan) CaptionsNeedBackend() bool {
	if !p.Captions.Enabled {
		return false
	}
	for _, clip := range p.Clips {
		if !clip.SourceAudioMuted() && !clip.CaptionReviewed {
			return true
		}
	}
	return false
}

// normalizeClipEdit trims overlay text and collapses an all-defaults edit back
// to nil so untouched clips keep their pre-edit plan shape. It deep-copies so
// the caller's overlays are never mutated.
func normalizeClipEdit(edit *ClipEdit) *ClipEdit {
	if edit == nil {
		return nil
	}
	normalized := *edit
	if len(edit.TextOverlays) > 0 {
		normalized.TextOverlays = make([]TextOverlay, len(edit.TextOverlays))
		for i, overlay := range edit.TextOverlays {
			overlay.Text = strings.TrimSpace(overlay.Text)
			normalized.TextOverlays[i] = overlay
		}
	}
	// Identity values render exactly like unset ones, so collapse them too;
	// this keeps plans saved through any surface shape-identical.
	if normalized.Speed == 1 {
		normalized.Speed = 0
	}
	if normalized.SourceVolume != nil && *normalized.SourceVolume == 1 {
		normalized.SourceVolume = nil
	}
	if normalized.Speed == 0 && normalized.SourceVolume == nil &&
		normalized.FadeInSeconds == 0 && normalized.FadeOutSeconds == 0 &&
		len(normalized.TextOverlays) == 0 {
		return nil
	}
	return &normalized
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

// normalizeKillfeedPlanEntries sorts and deduplicates cues without breaking
// the index alignment of their kill events. When kills are omitted (or the
// input is already invalid because lengths differ), cue-only normalization is
// retained and Validate reports any remaining length mismatch.
func normalizeKillfeedPlanEntries(cues []float64, kills [][]KillfeedKill) ([]float64, [][]KillfeedKill) {
	normalizedKills := normalizeKillfeedKills(kills)
	if len(cues) == 0 || len(normalizedKills) == 0 || len(normalizedKills) != len(cues) {
		return normalizeKillfeedSeconds(cues), normalizedKills
	}

	type entry struct {
		cue   float64
		kills []KillfeedKill
	}
	entries := make([]entry, len(cues))
	for i, cue := range cues {
		entries[i] = entry{cue: cue, kills: normalizedKills[i]}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].cue < entries[j].cue
	})

	deduped := entries[:0]
	for _, current := range entries {
		if len(deduped) > 0 && deduped[len(deduped)-1].cue == current.cue {
			deduped[len(deduped)-1].kills = mergeKillfeedKills(deduped[len(deduped)-1].kills, current.kills)
			continue
		}
		deduped = append(deduped, current)
	}

	normalizedCues := make([]float64, len(deduped))
	normalizedKills = make([][]KillfeedKill, len(deduped))
	for i, current := range deduped {
		normalizedCues[i] = current.cue
		normalizedKills[i] = current.kills
	}
	return normalizedCues, normalizedKills
}

func normalizeKillfeedCueProvenance(
	provenance []KillfeedCueProvenance,
	cues []float64,
) []KillfeedCueProvenance {
	if len(provenance) == 0 || len(cues) == 0 {
		return nil
	}
	wanted := make(map[float64]struct{}, len(cues))
	for _, cue := range cues {
		wanted[cue] = struct{}{}
	}
	normalized := make([]KillfeedCueProvenance, 0, len(provenance))
	for _, current := range provenance {
		if _, keep := wanted[current.CueSeconds]; !keep {
			continue
		}
		current.Origin = KillfeedCueOrigin(strings.ToLower(strings.TrimSpace(string(current.Origin))))
		current.EventID = strings.TrimSpace(current.EventID)
		normalized = append(normalized, current)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].CueSeconds < normalized[j].CueSeconds
	})
	// Do not guess when duplicate metadata disagrees about origin or event
	// identity. ClipRange.Validate reports the ambiguity before render.
	return normalized
}

func mergeKillfeedKills(existing, incoming []KillfeedKill) []KillfeedKill {
	merged := make([]KillfeedKill, 0, len(existing)+len(incoming))
	seen := make(map[KillfeedKill]struct{}, len(existing)+len(incoming))
	for _, kills := range [][]KillfeedKill{existing, incoming} {
		for _, kill := range kills {
			if _, ok := seen[kill]; ok {
				continue
			}
			seen[kill] = struct{}{}
			merged = append(merged, kill)
		}
	}
	return merged
}
