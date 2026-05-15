// Package recording defines the local recording contract consumed by
// zv-recorder. It intentionally does not know about HTTP, Asynq, or Postgres.
package recording

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/reche/zackvideo/internal/killplan"
)

const steamID64AccountIDBase uint64 = 76561197960265728

// StreamMode names the HLAE output strategy.
type StreamMode string

const (
	// StreamModeFFmpegDirect uses the candidate direct ffmpeg mirv_streams
	// command being validated by the HLAE prototype.
	StreamModeFFmpegDirect StreamMode = "ffmpeg_direct"

	// StreamModeTGASequence is the fallback raw-frame path.
	StreamModeTGASequence StreamMode = "tga_sequence"
)

// HUDMode controls whether HLAE records a clean stream or the in-game HUD.
type HUDMode string

const (
	// HUDModeGameplay keeps the CS2 HUD, crosshair, killfeed, weapon, ammo,
	// and health visible for a normal gameplay-looking recording.
	HUDModeGameplay HUDMode = "gameplay"

	// HUDModeClean hides the HUD for cinematic/effects-friendly captures.
	HUDModeClean HUDMode = "clean"
)

// StreamConfig describes how HLAE should emit raw recordings.
type StreamConfig struct {
	Mode    StreamMode `json:"mode"`
	HUDMode HUDMode    `json:"hud_mode,omitempty"`
	FPS     int        `json:"fps"`
	Width   int        `json:"width"`
	Height  int        `json:"height"`
	CRF     int        `json:"crf,omitempty"`
}

// RuntimeConfig captures HLAE runtime toggles that affect timing.
type RuntimeConfig struct {
	HostTimescale float64 `json:"host_timescale,omitempty"`
	QuitTickPad   int     `json:"quit_tick_pad,omitempty"`
}

// RecordingPlan is the lowest-level input to script generation.
type RecordingPlan struct {
	DemoPath        string             `json:"demo_path"`
	OutputDir       string             `json:"output_dir"`
	TargetSteamID64 string             `json:"target_steamid64"`
	TargetAccountID uint32             `json:"target_account_id"`
	Tickrate        int                `json:"tickrate"`
	Segments        []RecordingSegment `json:"segments"`
	Stream          StreamConfig       `json:"stream"`
	Runtime         RuntimeConfig      `json:"runtime"`
}

// RecordingSegment is one HLAE record window.
type RecordingSegment struct {
	ID        string          `json:"id"`
	Round     int             `json:"round,omitempty"`
	TickStart int             `json:"tick_start"`
	TickEnd   int             `json:"tick_end"`
	Kills     []killplan.Kill `json:"kills,omitempty"`
}

// RecordingArtifact is one file discovered after recording.
type RecordingArtifact struct {
	SegmentID       string  `json:"segment_id,omitempty"`
	TakeID          string  `json:"take_id,omitempty"`
	Type            string  `json:"type,omitempty"`
	Role            string  `json:"role,omitempty"`
	Path            string  `json:"path"`
	SizeBytes       int64   `json:"size_bytes"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	FrameCount      int64   `json:"frame_count,omitempty"`
	FrameRate       string  `json:"frame_rate,omitempty"`
	Codec           string  `json:"codec,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	SampleRate      int     `json:"sample_rate,omitempty"`
	Channels        int     `json:"channels,omitempty"`
	ProbeError      string  `json:"probe_error,omitempty"`
}

// RecordingResult is the file emitted by zv-recorder after a run.
type RecordingResult struct {
	Plan      RecordingPlan       `json:"plan"`
	Script    string              `json:"script"`
	Artifacts []RecordingArtifact `json:"artifacts"`
	Warnings  []string            `json:"warnings,omitempty"`
	Error     string              `json:"error,omitempty"`
}

// DefaultStreamConfig returns the current V1 target recording format.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		Mode:    StreamModeFFmpegDirect,
		HUDMode: HUDModeGameplay,
		FPS:     60,
		Width:   1920,
		Height:  1080,
		CRF:     18,
	}
}

// NewPlanFromKillPlan converts parser output into the local recorder contract.
func NewPlanFromKillPlan(plan killplan.Plan, demoPath, outputDir string, stream StreamConfig) (RecordingPlan, error) {
	accountID, err := AccountIDFromSteamID64(plan.Target.SteamID64)
	if err != nil {
		return RecordingPlan{}, err
	}
	stream = normalizeStreamConfig(stream)
	out := RecordingPlan{
		DemoPath:        demoPath,
		OutputDir:       outputDir,
		TargetSteamID64: plan.Target.SteamID64,
		TargetAccountID: accountID,
		Tickrate:        plan.Demo.Tickrate,
		Stream:          stream,
		Runtime: RuntimeConfig{
			QuitTickPad: 200,
		},
	}
	for _, s := range plan.Segments {
		out.Segments = append(out.Segments, RecordingSegment{
			ID:        s.ID,
			Round:     s.Round,
			TickStart: s.TickStart,
			TickEnd:   s.TickEnd,
			Kills:     s.Kills,
		})
	}
	if err := out.Validate(); err != nil {
		return RecordingPlan{}, err
	}
	return out, nil
}

// AccountIDFromSteamID64 converts a SteamID64 to the 32-bit account id used
// by CS2's spec_player_by_accountid command.
func AccountIDFromSteamID64(raw string) (uint32, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse steamid64: %w", err)
	}
	if id < steamID64AccountIDBase || id-steamID64AccountIDBase > uint64(^uint32(0)) {
		return 0, fmt.Errorf("steamid64 %q is outside account-id range", raw)
	}
	return uint32(id - steamID64AccountIDBase), nil
}

// Validate rejects plans that would generate ambiguous or unsafe scripts.
func (p RecordingPlan) Validate() error {
	if p.DemoPath == "" {
		return fmt.Errorf("demo_path is required")
	}
	if p.OutputDir == "" {
		return fmt.Errorf("output_dir is required")
	}
	if p.TargetAccountID == 0 {
		return fmt.Errorf("target_account_id is required")
	}
	if p.Tickrate <= 0 {
		return fmt.Errorf("tickrate must be positive")
	}
	if len(p.Segments) == 0 {
		return fmt.Errorf("at least one segment is required")
	}
	if p.Stream.Mode == "" {
		return fmt.Errorf("stream mode is required")
	}
	if p.Stream.HUDMode != "" && p.Stream.HUDMode != HUDModeGameplay && p.Stream.HUDMode != HUDModeClean {
		return fmt.Errorf("stream hud_mode must be %q or %q", HUDModeGameplay, HUDModeClean)
	}
	if p.Stream.FPS <= 0 || p.Stream.Width <= 0 || p.Stream.Height <= 0 {
		return fmt.Errorf("stream fps, width, and height must be positive")
	}
	seen := map[string]bool{}
	for i, s := range p.Segments {
		if s.ID == "" {
			return fmt.Errorf("segments[%d].id is required", i)
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate segment id %q", s.ID)
		}
		seen[s.ID] = true
		if s.TickStart <= 0 {
			return fmt.Errorf("segment %s tick_start must be positive", s.ID)
		}
		if s.TickEnd <= s.TickStart {
			return fmt.Errorf("segment %s tick_end must be greater than tick_start", s.ID)
		}
	}
	return nil
}

func normalizeStreamConfig(stream StreamConfig) StreamConfig {
	defaults := DefaultStreamConfig()
	if stream.Mode == "" {
		return defaults
	}
	if stream.HUDMode == "" {
		stream.HUDMode = defaults.HUDMode
	}
	if stream.FPS == 0 {
		stream.FPS = defaults.FPS
	}
	if stream.Width == 0 {
		stream.Width = defaults.Width
	}
	if stream.Height == 0 {
		stream.Height = defaults.Height
	}
	if stream.CRF == 0 {
		stream.CRF = defaults.CRF
	}
	return stream
}

// SegmentOutputPath returns the preferred output file path for a segment.
func (p RecordingPlan) SegmentOutputPath(segmentID string) string {
	ext := ".mp4"
	if p.Stream.Mode == StreamModeTGASequence {
		ext = ""
	}
	return filepath.Join(p.OutputDir, segmentID+ext)
}
