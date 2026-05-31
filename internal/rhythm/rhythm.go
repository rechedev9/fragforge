// Package rhythm analyzes music timing for deterministic kill-to-beat edits.
package rhythm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/recording"
)

const SchemaVersion = "1.0"

type Config struct {
	InputPath         string
	KillPlanPath      string
	FFmpegPath        string
	SampleRate        int
	MinBPM            float64
	MaxBPM            float64
	KillOffsetSeconds float64
	MaxBeats          int
	MaxOnsets         int
}

type Analysis struct {
	SchemaVersion     string        `json:"schema_version"`
	SourcePath        string        `json:"source_path,omitempty"`
	KillPlanPath      string        `json:"killplan,omitempty"`
	DurationSeconds   float64       `json:"duration_seconds"`
	SampleRate        int           `json:"sample_rate"`
	EstimatedBPM      float64       `json:"estimated_bpm"`
	BeatPeriodSeconds float64       `json:"beat_period_seconds"`
	BeatPhaseSeconds  float64       `json:"beat_phase_seconds"`
	KillOffsetSeconds float64       `json:"kill_offset_seconds"`
	BeatTimesSeconds  []float64     `json:"beat_times_seconds"`
	StrongOnsets      []Onset       `json:"strong_onsets,omitempty"`
	SegmentSync       []SegmentSync `json:"segment_sync,omitempty"`
	Warnings          []string      `json:"warnings,omitempty"`
}

type Onset struct {
	TimeSeconds float64 `json:"time_seconds"`
	Strength    float64 `json:"strength"`
}

type SegmentSync struct {
	SegmentID               string  `json:"segment_id"`
	Round                   int     `json:"round,omitempty"`
	FirstKillTimeSeconds    float64 `json:"first_kill_time_seconds"`
	DurationSeconds         float64 `json:"duration_seconds"`
	AssignedBeatSeconds     float64 `json:"assigned_beat_seconds"`
	TargetKillTimeSeconds   float64 `json:"target_kill_time_seconds"`
	TimelineStartSeconds    float64 `json:"timeline_start_seconds"`
	GapBeforeSeconds        float64 `json:"gap_before_seconds,omitempty"`
	DeltaToBeatMilliseconds int     `json:"delta_to_beat_ms"`
}

type samplesConfig struct {
	MinBPM            float64
	MaxBPM            float64
	KillOffsetSeconds float64
	MaxBeats          int
	MaxOnsets         int
}

// AnalyzeFile decodes audio through FFmpeg and returns a beat grid plus optional
// kill-plan sync suggestions.
func AnalyzeFile(ctx context.Context, cfg Config) (Analysis, error) {
	if strings.TrimSpace(cfg.InputPath) == "" {
		return Analysis{}, fmt.Errorf("input path is required")
	}
	inputPath, err := filepath.Abs(cfg.InputPath)
	if err != nil {
		return Analysis{}, fmt.Errorf("resolve input path: %w", err)
	}
	if _, err := os.Stat(inputPath); err != nil {
		return Analysis{}, fmt.Errorf("input not found: %w", err)
	}
	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 22050
	}
	samples, err := decodeMonoSamples(ctx, cfg.FFmpegPath, inputPath, sampleRate)
	if err != nil {
		return Analysis{}, err
	}
	analysis := AnalyzeSamples(samples, sampleRate, samplesConfig{
		MinBPM:            cfg.MinBPM,
		MaxBPM:            cfg.MaxBPM,
		KillOffsetSeconds: cfg.KillOffsetSeconds,
		MaxBeats:          cfg.MaxBeats,
		MaxOnsets:         cfg.MaxOnsets,
	})
	analysis.SourcePath = inputPath
	if strings.TrimSpace(cfg.KillPlanPath) != "" {
		planPath, err := filepath.Abs(cfg.KillPlanPath)
		if err != nil {
			return Analysis{}, fmt.Errorf("resolve killplan path: %w", err)
		}
		plan, err := readKillPlan(planPath)
		if err != nil {
			return Analysis{}, err
		}
		analysis.KillPlanPath = planPath
		analysis.SegmentSync = BuildSegmentSync(plan, analysis.BeatTimesSeconds, analysis.KillOffsetSeconds)
		if len(analysis.SegmentSync) == 0 {
			analysis.Warnings = append(analysis.Warnings, "killplan produced no segment sync entries")
		}
	}
	return analysis, nil
}

func AnalyzeSamples(samples []float64, sampleRate int, cfg samplesConfig) Analysis {
	if sampleRate <= 0 || len(samples) == 0 {
		return Analysis{SchemaVersion: SchemaVersion}
	}
	minBPM := cfg.MinBPM
	if minBPM <= 0 {
		minBPM = 85
	}
	maxBPM := cfg.MaxBPM
	if maxBPM <= minBPM {
		maxBPM = 125
	}
	killOffset := cfg.KillOffsetSeconds
	if killOffset == 0 {
		killOffset = 0.10
	}
	maxBeats := cfg.MaxBeats
	if maxBeats <= 0 {
		maxBeats = 256
	}
	maxOnsets := cfg.MaxOnsets
	if maxOnsets <= 0 {
		maxOnsets = 32
	}

	frame := 1024
	hop := 256
	envelope := onsetEnvelope(samples, frame, hop)
	if len(envelope) == 0 {
		return Analysis{
			SchemaVersion:     SchemaVersion,
			DurationSeconds:   roundMillis(float64(len(samples)) / float64(sampleRate)),
			SampleRate:        sampleRate,
			KillOffsetSeconds: roundMillis(killOffset),
			BeatTimesSeconds:  nil,
			BeatPeriodSeconds: 0,
			BeatPhaseSeconds:  0,
			EstimatedBPM:      0,
			StrongOnsets:      nil,
		}
	}
	normalizeInPlace(envelope)
	lag := bestLag(envelope, sampleRate, hop, minBPM, maxBPM)
	period := float64(lag*hop) / float64(sampleRate)
	bpm := 60 / period
	phase := bestPhase(envelope, sampleRate, hop, period)
	duration := float64(len(samples)) / float64(sampleRate)
	beats := beatTimes(phase, period, duration, maxBeats)
	onsets := strongestOnsets(envelope, sampleRate, hop, maxOnsets)
	return Analysis{
		SchemaVersion:     SchemaVersion,
		DurationSeconds:   roundMillis(duration),
		SampleRate:        sampleRate,
		EstimatedBPM:      roundHundredths(bpm),
		BeatPeriodSeconds: roundMillis(period),
		BeatPhaseSeconds:  roundMillis(phase),
		KillOffsetSeconds: roundMillis(killOffset),
		BeatTimesSeconds:  beats,
		StrongOnsets:      onsets,
	}
}

func BuildSegmentSync(plan killplan.Plan, beats []float64, killOffset float64) []SegmentSync {
	if plan.Demo.Tickrate <= 0 || len(beats) == 0 {
		return nil
	}
	var out []SegmentSync
	cursor := 0.0
	for _, segment := range plan.Segments {
		if len(segment.Kills) == 0 {
			continue
		}
		recordingSegment := recording.RecordingSegment{
			ID:        segment.ID,
			Round:     segment.Round,
			TickStart: segment.TickStart,
			TickEnd:   segment.TickEnd,
			Kills:     segment.Kills,
		}
		recordStart := recording.EffectiveRecordStartTick(recordingSegment, plan.Demo.Tickrate)
		firstKill := firstKillTick(segment.Kills)
		if firstKill <= 0 || firstKill < recordStart {
			continue
		}
		firstKillSeconds := float64(firstKill-recordStart) / float64(plan.Demo.Tickrate)
		durationSeconds := float64(segment.TickEnd-recordStart) / float64(plan.Demo.Tickrate)
		if durationSeconds <= 0 {
			continue
		}
		beat := nextAssignableBeat(beats, cursor+firstKillSeconds-killOffset)
		targetKillTime := beat + killOffset
		start := targetKillTime - firstKillSeconds
		gap := 0.0
		if start < cursor {
			start = cursor
			targetKillTime = start + firstKillSeconds
			beat = nearestBeat(beats, targetKillTime-killOffset)
		} else if start > cursor {
			gap = start - cursor
		}
		out = append(out, SegmentSync{
			SegmentID:               segment.ID,
			Round:                   segment.Round,
			FirstKillTimeSeconds:    roundMillis(firstKillSeconds),
			DurationSeconds:         roundMillis(durationSeconds),
			AssignedBeatSeconds:     roundMillis(beat),
			TargetKillTimeSeconds:   roundMillis(targetKillTime),
			TimelineStartSeconds:    roundMillis(start),
			GapBeforeSeconds:        roundMillis(gap),
			DeltaToBeatMilliseconds: int(math.Round((targetKillTime - beat) * 1000)),
		})
		cursor = start + durationSeconds
	}
	return out
}

func decodeMonoSamples(ctx context.Context, ffmpegPath, inputPath string, sampleRate int) ([]float64, error) {
	if strings.TrimSpace(ffmpegPath) == "" {
		ffmpegPath = "ffmpeg"
	}
	args := []string{
		"-v", "error",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"pipe:1",
	}
	// #nosec G204 -- ffmpegPath and inputPath are explicit local CLI inputs.
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("decode audio with ffmpeg: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("decode audio with ffmpeg: %w", err)
	}
	if len(out)%2 != 0 {
		return nil, fmt.Errorf("decoded audio has odd byte count")
	}
	samples := make([]float64, len(out)/2)
	for i := range samples {
		v := int16(binary.LittleEndian.Uint16(out[i*2 : i*2+2]))
		samples[i] = float64(v) / 32768
	}
	return samples, nil
}

func onsetEnvelope(samples []float64, frame, hop int) []float64 {
	if len(samples) < frame || frame <= 0 || hop <= 0 {
		return nil
	}
	frames := 1 + (len(samples)-frame)/hop
	rms := make([]float64, frames)
	for i := 0; i < frames; i++ {
		start := i * hop
		sum := 0.0
		for j := 0; j < frame; j++ {
			v := samples[start+j]
			sum += v * v
		}
		rms[i] = math.Sqrt(sum / float64(frame))
	}
	env := make([]float64, frames)
	for i := 1; i < frames; i++ {
		if d := rms[i] - rms[i-1]; d > 0 {
			env[i] = d
		}
	}
	return smooth(env, 5)
}

func smooth(values []float64, width int) []float64 {
	if width <= 1 || len(values) == 0 {
		return append([]float64(nil), values...)
	}
	out := make([]float64, len(values))
	half := width / 2
	for i := range values {
		sum := 0.0
		count := 0
		for j := i - half; j <= i+half; j++ {
			if j < 0 || j >= len(values) {
				continue
			}
			sum += values[j]
			count++
		}
		out[i] = sum / float64(count)
	}
	return out
}

func normalizeInPlace(values []float64) {
	if len(values) == 0 {
		return
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	var variance float64
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	std := math.Sqrt(variance / float64(len(values)))
	if std == 0 {
		return
	}
	for i := range values {
		values[i] = (values[i] - mean) / std
	}
}

func bestLag(envelope []float64, sampleRate, hop int, minBPM, maxBPM float64) int {
	minLag := int((60 / maxBPM) * float64(sampleRate) / float64(hop))
	maxLag := int((60 / minBPM) * float64(sampleRate) / float64(hop))
	if minLag < 1 {
		minLag = 1
	}
	if maxLag >= len(envelope) {
		maxLag = len(envelope) - 1
	}
	best := minLag
	bestScore := math.Inf(-1)
	for lag := minLag; lag <= maxLag; lag++ {
		score := 0.0
		for i := 0; i+lag < len(envelope); i++ {
			score += envelope[i] * envelope[i+lag]
		}
		if score > bestScore {
			bestScore = score
			best = lag
		}
	}
	return best
}

func bestPhase(envelope []float64, sampleRate, hop int, period float64) float64 {
	if period <= 0 {
		return 0
	}
	bestPhase := 0.0
	bestScore := math.Inf(-1)
	for i := 0; i < 200; i++ {
		phase := period * float64(i) / 200
		score := 0.0
		count := 0
		for t := phase; ; t += period {
			idx := int(math.Round(t * float64(sampleRate) / float64(hop)))
			if idx < 0 || idx >= len(envelope) {
				break
			}
			score += envelope[idx]
			count++
		}
		if count > 0 {
			score /= float64(count)
		}
		if score > bestScore {
			bestScore = score
			bestPhase = phase
		}
	}
	return bestPhase
}

func beatTimes(phase, period, duration float64, maxBeats int) []float64 {
	var beats []float64
	for t := phase; t <= duration && len(beats) < maxBeats; t += period {
		if t >= 0 {
			beats = append(beats, roundMillis(t))
		}
	}
	return beats
}

func strongestOnsets(envelope []float64, sampleRate, hop, maxOnsets int) []Onset {
	minDistance := int(0.22 * float64(sampleRate) / float64(hop))
	if minDistance < 1 {
		minDistance = 1
	}
	var peaks []Onset
	for i := 1; i+1 < len(envelope); i++ {
		if envelope[i] <= 1.25 || envelope[i] < envelope[i-1] || envelope[i] < envelope[i+1] {
			continue
		}
		if len(peaks) > 0 {
			lastFrame := int(math.Round(peaks[len(peaks)-1].TimeSeconds * float64(sampleRate) / float64(hop)))
			if i-lastFrame < minDistance {
				if envelope[i] > peaks[len(peaks)-1].Strength {
					peaks[len(peaks)-1] = Onset{TimeSeconds: frameTime(i, sampleRate, hop), Strength: envelope[i]}
				}
				continue
			}
		}
		peaks = append(peaks, Onset{TimeSeconds: frameTime(i, sampleRate, hop), Strength: envelope[i]})
	}
	sort.Slice(peaks, func(i, j int) bool {
		return peaks[i].Strength > peaks[j].Strength
	})
	if len(peaks) > maxOnsets {
		peaks = peaks[:maxOnsets]
	}
	sort.Slice(peaks, func(i, j int) bool {
		return peaks[i].TimeSeconds < peaks[j].TimeSeconds
	})
	for i := range peaks {
		peaks[i].TimeSeconds = roundMillis(peaks[i].TimeSeconds)
		peaks[i].Strength = roundHundredths(peaks[i].Strength)
	}
	return peaks
}

func readKillPlan(path string) (killplan.Plan, error) {
	// #nosec G304 -- path is an explicit local CLI input.
	b, err := os.ReadFile(path)
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("read killplan: %w", err)
	}
	var plan killplan.Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		return killplan.Plan{}, fmt.Errorf("decode killplan: %w", err)
	}
	return plan, nil
}

func firstKillTick(kills []killplan.Kill) int {
	out := 0
	for _, kill := range kills {
		if kill.Tick <= 0 {
			continue
		}
		if out == 0 || kill.Tick < out {
			out = kill.Tick
		}
	}
	return out
}

func nextAssignableBeat(beats []float64, minTime float64) float64 {
	for _, beat := range beats {
		if beat >= minTime {
			return beat
		}
	}
	return beats[len(beats)-1]
}

func nearestBeat(beats []float64, target float64) float64 {
	best := beats[0]
	bestDelta := math.Abs(target - best)
	for _, beat := range beats[1:] {
		if delta := math.Abs(target - beat); delta < bestDelta {
			best = beat
			bestDelta = delta
		}
	}
	return best
}

func frameTime(frame, sampleRate, hop int) float64 {
	return float64(frame*hop) / float64(sampleRate)
}

func roundMillis(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func roundHundredths(v float64) float64 {
	return math.Round(v*100) / 100
}
