// Package streamkillfeed detects the source-frame birth of CS2 killfeed rows.
//
// The package deliberately keeps integer source PTS as its canonical clock.
// CueSeconds exists for edit-plan compatibility and is always derived from PTS,
// the frame time base, and SourceProbe.StartTimeSeconds.
package streamkillfeed

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const (
	// CoarseFPS is used only to locate short native-frame refinement windows.
	// Final events never inherit this synthetic eight-fps clock.
	CoarseFPS = 8
	// LookbackSeconds seeds row identity before a clip starts, preventing a
	// notice already on screen at the boundary from becoming a fabricated kill.
	LookbackSeconds = 8
	// SampleDelaySeconds separates the exact display cue from the later frame
	// whose settled row is suitable for OCR.
	SampleDelaySeconds = streamclips.KillfeedSampleDelaySeconds
)

// Mode describes how strongly video evidence locates an event.
type Mode string

const (
	ModeAlignedFrame Mode = "aligned_frame"
	ModeBurst        Mode = "burst"
	ModeUnresolved   Mode = "unresolved"
)

// TimeBase is an FFmpeg rational. A PTS of p represents p*Num/Den seconds.
type TimeBase struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

func ParseTimeBase(value string) (TimeBase, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return TimeBase{}, fmt.Errorf("invalid time base %q", value)
	}
	num, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return TimeBase{}, fmt.Errorf("invalid time base numerator %q: %w", parts[0], err)
	}
	den, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return TimeBase{}, fmt.Errorf("invalid time base denominator %q: %w", parts[1], err)
	}
	tb := TimeBase{Num: num, Den: den}
	if err := tb.Validate(); err != nil {
		return TimeBase{}, err
	}
	return tb, nil
}

func (t TimeBase) Validate() error {
	if t.Num <= 0 || t.Den <= 0 {
		return fmt.Errorf("time base must have positive numerator and denominator")
	}
	return nil
}

func (t TimeBase) Seconds(pts int64) float64 {
	return float64(pts) * float64(t.Num) / float64(t.Den)
}

func (t TimeBase) String() string {
	return strconv.FormatInt(t.Num, 10) + "/" + strconv.FormatInt(t.Den, 10)
}

// RowEvidence identifies one row born in an event and where to crop it from the
// later SamplePTS frame for OCR. Fingerprint is deterministic evidence, not an
// OCR result.
type RowEvidence struct {
	OnsetRowIndex  int                   `json:"onset_row_index"`
	SampleRowIndex int                   `json:"sample_row_index"`
	Fingerprint    string                `json:"fingerprint"`
	OnsetBounds    streamclips.NoticeRow `json:"onset_bounds"`
	SampleBounds   streamclips.NoticeRow `json:"sample_bounds"`
}

// Event is one or more row births on exactly the same native source PTS. Events
// on different frames are never merged, regardless of how close they are.
type Event struct {
	EventID       string        `json:"event_id"`
	SourcePTS     int64         `json:"source_pts"`
	TimeBase      TimeBase      `json:"time_base"`
	CueSeconds    float64       `json:"cue_seconds"`
	OnsetStartPTS int64         `json:"onset_start_pts"`
	OnsetEndPTS   int64         `json:"onset_end_pts"`
	SamplePTS     int64         `json:"sample_pts"`
	SampleSeconds float64       `json:"sample_seconds"`
	Mode          Mode          `json:"mode"`
	Rows          []RowEvidence `json:"rows"`
}

func (e Event) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("event id is required")
	}
	if err := e.TimeBase.Validate(); err != nil {
		return err
	}
	if math.IsNaN(e.CueSeconds) || math.IsInf(e.CueSeconds, 0) || e.CueSeconds < 0 {
		return fmt.Errorf("cue seconds must be finite and >= 0")
	}
	if math.IsNaN(e.SampleSeconds) || math.IsInf(e.SampleSeconds, 0) || e.SampleSeconds < e.CueSeconds {
		return fmt.Errorf("sample seconds must be finite and >= cue seconds")
	}
	if e.OnsetStartPTS > e.OnsetEndPTS || e.OnsetEndPTS != e.SourcePTS {
		return fmt.Errorf("invalid onset PTS interval")
	}
	if e.SamplePTS < e.SourcePTS {
		return fmt.Errorf("sample PTS must not precede source PTS")
	}
	switch e.Mode {
	case ModeAlignedFrame, ModeBurst, ModeUnresolved:
	default:
		return fmt.Errorf("invalid event mode %q", e.Mode)
	}
	if len(e.Rows) == 0 {
		return fmt.Errorf("event rows are required")
	}
	return nil
}

// Analyzer scans one selected clip from a local source video.
type Analyzer struct {
	FFmpegPath string
}
