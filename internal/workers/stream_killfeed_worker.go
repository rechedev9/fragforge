package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/streamkillfeed"
	"github.com/rechedev9/fragforge/internal/tasks"
)

var errKillfeedAnalysisSuperseded = errors.New("killfeed analysis generation superseded")

var errStreamRenderSuperseded = errors.New("stream render killfeed analysis superseded")

// HandleGenerateStreamKillfeed scans every selected clip on the source media
// clock. The worker writes only its generation artifact: the HTTP admission
// boundary owns the active pointer, so an older task can never replace a newer
// crop or clip-range analysis.
func (w *StreamRenderWorker) HandleGenerateStreamKillfeed(ctx context.Context, t *asynq.Task) error {
	var payload tasks.GenerateStreamKillfeedPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	generationID, err := tasks.StreamKillfeedGenerationFromTask(t)
	if err != nil {
		return err
	}
	if payload.JobID == uuid.Nil {
		return fmt.Errorf("killfeed analysis job id is required")
	}

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load stream job %s: %w", payload.JobID, err)
	}
	state, owned, err := w.loadOwnedKillfeedAnalysis(j.ID, generationID)
	if err != nil {
		recordStreamKillfeedFailure(j.ID, err)
		return err
	}
	if !owned {
		return nil
	}
	if state.Status != streamclips.KillfeedAnalysisQueued &&
		state.Status != streamclips.KillfeedAnalysisAnalyzing {
		return nil
	}
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(j.ID, generationID)
	if err != nil {
		return err
	}
	previous, exists, err := readKillfeedAnalysisState(w.storage, generationKey)
	if err != nil {
		return fmt.Errorf("read killfeed analysis generation: %w", err)
	}
	if exists {
		if previous.JobID != j.ID || previous.GenerationID != generationID {
			return fmt.Errorf("killfeed analysis generation does not match its artifact key")
		}
		if err := previous.Validate(); err != nil {
			return fmt.Errorf("validate killfeed analysis generation: %w", err)
		}
		switch previous.Status {
		case streamclips.KillfeedAnalysisReady,
			streamclips.KillfeedAnalysisReviewRequired,
			streamclips.KillfeedAnalysisApplied,
			streamclips.KillfeedAnalysisFailed:
			return nil
		}
	}

	state.Status = streamclips.KillfeedAnalysisAnalyzing
	state.Error = ""
	state.UpdatedAt = time.Now().UTC()
	owned, err = w.writeKillfeedAnalysisStateIfOwned(state)
	if err != nil {
		return fmt.Errorf("write analyzing killfeed state: %w", err)
	}
	if !owned {
		return nil
	}

	state, err = w.generateStreamKillfeedAnalysis(ctx, j, state)
	if err != nil {
		if errors.Is(err, errKillfeedAnalysisSuperseded) {
			// A changed plan can invalidate this generation while it still owns the
			// active pointer. Close that lifecycle instead of leaving GET stuck on
			// "analyzing" forever. If a newer generation owns the pointer, the
			// conditional write is a no-op and cannot overwrite it.
			state.Status = streamclips.KillfeedAnalysisFailed
			state.Error = "killfeed analysis inputs changed; start a new analysis generation"
			state.UpdatedAt = time.Now().UTC()
			if _, stateErr := w.writeKillfeedAnalysisStateIfOwned(state); stateErr != nil {
				return fmt.Errorf("close superseded killfeed analysis: %w", stateErr)
			}
			return nil
		}
		state.Status = streamclips.KillfeedAnalysisFailed
		state.Error = singleLine(err)
		state.UpdatedAt = time.Now().UTC()
		owned, stateErr := w.writeKillfeedAnalysisStateIfOwned(state)
		if stateErr != nil {
			return errors.Join(err, fmt.Errorf("write failed killfeed state: %w", stateErr))
		}
		if owned {
			recordStreamKillfeedFailure(j.ID, err)
		}
		return err
	}
	owned, err = w.writeKillfeedAnalysisStateIfOwned(state)
	if err != nil {
		return fmt.Errorf("write completed killfeed state: %w", err)
	}
	if !owned {
		return nil
	}
	return nil
}

func (w *StreamRenderWorker) generateStreamKillfeedAnalysis(
	ctx context.Context,
	j streamclips.Job,
	state streamclips.KillfeedAnalysisState,
) (streamclips.KillfeedAnalysisState, error) {
	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return state, err
	}
	if w.killfeedScanner == nil {
		return state, fmt.Errorf("killfeed scanner is not configured")
	}
	if len(j.EditPlan) == 0 {
		return state, fmt.Errorf("stream job %s has no edit plan", j.ID)
	}
	var plan streamclips.EditPlan
	if err := json.Unmarshal(j.EditPlan, &plan); err != nil {
		return state, fmt.Errorf("decode edit plan: %w", err)
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if migrated, changed := streamclips.MigrateLegacySourceDuration(plan, j.Probe.DurationSeconds); changed {
		plan = migrated
	}
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		return state, err
	}
	if plan.KillfeedCrop == nil {
		return state, fmt.Errorf("edit plan has no killfeed crop")
	}
	if len(plan.Clips) == 0 {
		return state, fmt.Errorf("edit plan has no clips")
	}
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		j.SourceSHA256, *plan.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		return state, fmt.Errorf("fingerprint killfeed analysis inputs: %w", err)
	}
	if fingerprint != state.Fingerprint || state.SourceSHA256 != j.SourceSHA256 ||
		state.KillfeedCrop != *plan.KillfeedCrop {
		return state, errKillfeedAnalysisSuperseded
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "stream-killfeed")
	if err != nil {
		return state, err
	}
	defer cleanup()
	sourcePath := filepath.Join(workDir, "source.mp4")
	if err := copyStorageToFile(w.storage, j.SourcePath, sourcePath); err != nil {
		return state, fmt.Errorf("materialize stream source: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.timeoutDuration())
	defer cancel()
	state.Clips = make([]streamclips.KillfeedAnalysisClip, len(plan.Clips))
	for i, clip := range plan.Clips {
		state.Clips[i] = streamclips.KillfeedAnalysisClip{
			ClipID:       clip.ID,
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
			Events:       []streamclips.KillfeedAnalysisEvent{},
		}
	}
	for clipIndex, clip := range plan.Clips {
		events, scanErr := w.killfeedScanner.Scan(
			runCtx, sourcePath, j.Probe, *plan.KillfeedCrop, clip,
		)
		if scanErr != nil {
			return state, fmt.Errorf("scan killfeed clip %s: %w", clip.ID, scanErr)
		}
		if _, owned, err := w.loadOwnedKillfeedAnalysis(j.ID, state.GenerationID); err != nil {
			return state, err
		} else if !owned {
			return state, errKillfeedAnalysisSuperseded
		}
		candidate := streamclips.KillfeedAnalysisClip{
			ClipID:       clip.ID,
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
			Events:       make([]streamclips.KillfeedAnalysisEvent, 0, len(events)),
		}
		for _, event := range events {
			if err := event.Validate(); err != nil {
				return state, fmt.Errorf("validate killfeed event %s: %w", event.EventID, err)
			}
			if event.Mode == streamkillfeed.ModeUnresolved {
				warning := fmt.Sprintf(
					"omitted left-censored killfeed event %s at %.9fs because no preceding source frame proves its onset",
					event.EventID, event.CueSeconds,
				)
				candidate.Warnings = append(candidate.Warnings, warning)
				state.Warnings = append(state.Warnings, "clip "+clip.ID+": "+warning)
				continue
			}
			if w.extractKillfeedRows == nil {
				return state, fmt.Errorf("killfeed row extractor is not configured")
			}
			rows, err := w.extractKillfeedRows(runCtx, sourcePath, j.Probe, event)
			if err != nil {
				return state, fmt.Errorf("extract killfeed event %s rows: %w", event.EventID, err)
			}
			if len(rows) != len(event.Rows) {
				return state, fmt.Errorf(
					"extract killfeed event %s rows: got %d PNGs, want %d",
					event.EventID, len(rows), len(event.Rows),
				)
			}
			for rowIndex, encoded := range rows {
				if !bytes.HasPrefix(encoded, []byte("\x89PNG\r\n\x1a\n")) {
					return state, fmt.Errorf(
						"extract killfeed event %s row %d: result is not a PNG",
						event.EventID, rowIndex,
					)
				}
				key, err := streamclips.KillfeedEventRowKey(
					j.ID, state.GenerationID, clip.ID, event.EventID, rowIndex,
				)
				if err != nil {
					return state, err
				}
				if err := w.storage.Put(key, bytes.NewReader(encoded)); err != nil {
					return state, fmt.Errorf(
						"persist killfeed event %s row %d: %w",
						event.EventID, rowIndex, err,
					)
				}
			}
			candidate.Events = append(candidate.Events, durableKillfeedEvent(event))
		}
		nextClips := append([]streamclips.KillfeedAnalysisClip(nil), state.Clips...)
		nextClips[clipIndex] = candidate
		nextState := state
		nextState.Clips = nextClips
		nextState.UpdatedAt = time.Now().UTC()
		if err := nextState.Validate(); err != nil {
			return state, fmt.Errorf("validate killfeed clip %s analysis: %w", clip.ID, err)
		}
		state = nextState
		owned, err := w.writeKillfeedAnalysisStateIfOwned(state)
		if err != nil {
			return state, err
		}
		if !owned {
			return state, errKillfeedAnalysisSuperseded
		}
	}
	state.Status = streamclips.KillfeedAnalysisReady
	state.UpdatedAt = time.Now().UTC()
	return state, nil
}

func durableKillfeedEvent(event streamkillfeed.Event) streamclips.KillfeedAnalysisEvent {
	rows := make([]streamclips.KillfeedRowEvidence, len(event.Rows))
	for i, row := range event.Rows {
		rows[i] = streamclips.KillfeedRowEvidence{
			OnsetRowIndex:  row.OnsetRowIndex,
			SampleRowIndex: row.SampleRowIndex,
			Fingerprint:    row.Fingerprint,
			OnsetBounds:    row.OnsetBounds,
			SampleBounds:   row.SampleBounds,
		}
	}
	return streamclips.KillfeedAnalysisEvent{
		EventID:       event.EventID,
		SourcePTS:     event.SourcePTS,
		TimeBase:      streamclips.KillfeedTimeBase{Num: event.TimeBase.Num, Den: event.TimeBase.Den},
		CueSeconds:    event.CueSeconds,
		OnsetStartPTS: event.OnsetStartPTS,
		OnsetEndPTS:   event.OnsetEndPTS,
		SamplePTS:     event.SamplePTS,
		SampleSeconds: event.SampleSeconds,
		Mode:          streamclips.KillfeedEventMode(event.Mode),
		Rows:          rows,
		Kills:         []streamclips.KillfeedKill{},
	}
}

func (w *StreamRenderWorker) loadOwnedKillfeedAnalysis(
	jobID, generationID uuid.UUID,
) (streamclips.KillfeedAnalysisState, bool, error) {
	state, exists, err := readKillfeedAnalysisState(w.storage, streamclips.KillfeedAnalysisKey(jobID))
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("read active killfeed analysis: %w", err)
	}
	if !exists || state.GenerationID != generationID {
		return streamclips.KillfeedAnalysisState{}, false, nil
	}
	if state.JobID != jobID {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("active killfeed analysis job id does not match its key")
	}
	if err := state.Validate(); err != nil {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("validate active killfeed analysis: %w", err)
	}
	return state, true, nil
}

func (w *StreamRenderWorker) writeKillfeedAnalysisStateIfOwned(
	state streamclips.KillfeedAnalysisState,
) (bool, error) {
	current, owned, err := w.loadOwnedKillfeedAnalysis(state.JobID, state.GenerationID)
	if err != nil || !owned {
		return false, err
	}
	if current.Fingerprint != state.Fingerprint {
		return false, fmt.Errorf("active killfeed analysis fingerprint changed within generation")
	}
	if err := state.Validate(); err != nil {
		return false, fmt.Errorf("validate killfeed analysis state: %w", err)
	}
	key, err := streamclips.KillfeedAnalysisGenerationKey(state.JobID, state.GenerationID)
	if err != nil {
		return false, err
	}
	// Do not update KillfeedAnalysisKey here. A replacement HTTP request may
	// race after the ownership read; writing only the immutable generation key
	// makes that race incapable of replacing the newer active pointer.
	if err := putJSONToStorage(w.storage, key, state); err != nil {
		return false, err
	}
	return true, nil
}

// appliedKillfeedAnalysis is the worker-side half of the render gate.
// It runs before status mutation, source materialization, or FFmpeg, so a task
// enqueued outside the HTTP handler cannot render stale detector output. A
// legacy/manual plan without analysis metadata remains compatible unless the
// caller explicitly enables RequireAppliedKillfeedAnalysis. Explicit automatic
// provenance always requires its exact durable captures.
func (w *StreamRenderWorker) appliedKillfeedAnalysis(
	j streamclips.Job,
	plan streamclips.EditPlan,
) (streamclips.KillfeedAnalysisState, error) {
	if plan.KillfeedCrop == nil {
		return streamclips.KillfeedAnalysisState{}, nil
	}
	if plan.KillfeedAnalysis == nil {
		if plan.Variant != streamclips.VariantStreamerLandscape16x9 && hasAutomaticKillfeedCue(plan) {
			return streamclips.KillfeedAnalysisState{}, fmt.Errorf("automatic killfeed cues require exact applied analysis before rendering")
		}
		if w.cfg.RequireAppliedKillfeedAnalysis {
			return streamclips.KillfeedAnalysisState{}, fmt.Errorf("killfeed analysis must be applied before rendering")
		}
		return streamclips.KillfeedAnalysisState{}, nil
	}
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		j.SourceSHA256, *plan.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf("validate applied killfeed analysis inputs: %w", err)
	}
	if fingerprint != plan.KillfeedAnalysis.Fingerprint {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf(
			"%w: metadata is stale for the current source, crop, or clip bounds",
			errStreamRenderSuperseded,
		)
	}

	active, exists, err := readKillfeedAnalysisState(w.storage, streamclips.KillfeedAnalysisKey(j.ID))
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf("read applied killfeed analysis pointer: %w", err)
	}
	// The active document is only the generation selector. Apply persists the
	// authoritative generation before refreshing this pointer, so a failed
	// second Put may leave the pointer's embedded status/fingerprint stale even
	// though it selects a complete applied generation.
	if !exists || active.JobID != j.ID ||
		active.GenerationID != plan.KillfeedAnalysis.GenerationID {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf(
			"%w: active generation is missing or obsolete",
			errStreamRenderSuperseded,
		)
	}
	key, err := streamclips.KillfeedAnalysisGenerationKey(j.ID, active.GenerationID)
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, err
	}
	generation, exists, err := readKillfeedAnalysisState(w.storage, key)
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf("read applied killfeed analysis generation: %w", err)
	}
	if !exists || generation.JobID != active.JobID ||
		generation.GenerationID != active.GenerationID ||
		generation.Fingerprint != fingerprint ||
		generation.Status != streamclips.KillfeedAnalysisApplied {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf(
			"%w: durable generation is missing or obsolete",
			errStreamRenderSuperseded,
		)
	}
	if err := generation.Validate(); err != nil {
		return streamclips.KillfeedAnalysisState{}, fmt.Errorf("validate applied killfeed analysis generation: %w", err)
	}
	return generation, nil
}

func hasAutomaticKillfeedCue(plan streamclips.EditPlan) bool {
	for _, clip := range plan.Clips {
		for _, cue := range clip.KillfeedSeconds {
			provenance, ok := clip.KillfeedProvenanceAt(cue)
			if ok && provenance.Origin == streamclips.KillfeedCueAutomatic {
				return true
			}
		}
	}
	return false
}

func (w *StreamRenderWorker) materializeAnalyzedKillfeedNotices(
	workDir string,
	jobID uuid.UUID,
	analysis streamclips.KillfeedAnalysisState,
	clip streamclips.ClipRange,
) ([][]string, error) {
	paths, err := renderClipKillfeedNotices(workDir, clip)
	if err != nil {
		return nil, err
	}
	if len(clip.KillfeedSeconds) == 0 {
		return paths, nil
	}
	if len(paths) == 0 {
		paths = make([][]string, len(clip.KillfeedSeconds))
	}

	var analyzedClip *streamclips.KillfeedAnalysisClip
	for i := range analysis.Clips {
		if analysis.Clips[i].ClipID == clip.ID {
			analyzedClip = &analysis.Clips[i]
			break
		}
	}
	if analyzedClip == nil {
		return nil, fmt.Errorf("applied killfeed analysis has no clip %s", clip.ID)
	}
	usedEvents := make(map[int]struct{}, len(analyzedClip.Events))
	for cueIndex, cue := range clip.KillfeedSeconds {
		provenance, explicitProvenance := clip.KillfeedProvenanceAt(cue)
		reviewedManualNotice := cueIndex < len(clip.KillfeedKills) && len(clip.KillfeedKills[cueIndex]) > 0
		if explicitProvenance && provenance.Origin == streamclips.KillfeedCueManual {
			if reviewedManualNotice {
				// Only an explicitly manual cue may render its reviewed synthetic
				// notice instead of an immutable source-row capture.
				continue
			}
			return nil, fmt.Errorf(
				"clip %s manual cue %.9f needs reviewed kills before rendering",
				clip.ID, cue,
			)
		}
		eventIndex := -1
		for i, event := range analyzedClip.Events {
			if _, used := usedEvents[i]; used || event.CueSeconds != cue {
				continue
			}
			if explicitProvenance && provenance.Origin == streamclips.KillfeedCueAutomatic &&
				provenance.EventID != "" && provenance.EventID != event.EventID {
				continue
			}
			eventIndex = i
			break
		}
		if eventIndex < 0 {
			if !explicitProvenance && reviewedManualNotice {
				// A reviewed manual cue has no source-backed capture, so its
				// structured notice is the safe legacy-compatible rendering.
				continue
			}
			return nil, fmt.Errorf(
				"clip %s cue %.9f has no exact captured killfeed event; rerun analysis or provide reviewed kills",
				clip.ID, cue,
			)
		}
		usedEvents[eventIndex] = struct{}{}
		event := analyzedClip.Events[eventIndex]
		// Captured rows remain the visual source of truth for automatic events,
		// even after OCR enrichment. A burst can contain several rows while OCR
		// resolves only some of them; replacing the capture with the partial
		// structured result would silently drop visible kills.
		cuePaths := make([]string, len(event.Rows))
		for rowIndex := range event.Rows {
			key, err := streamclips.KillfeedEventRowKey(
				jobID, analysis.GenerationID, clip.ID, event.EventID, rowIndex,
			)
			if err != nil {
				return nil, err
			}
			localPath := filepath.Join(
				workDir, "killfeed-captured", clip.ID, event.EventID,
				fmt.Sprintf("row-%03d.png", rowIndex),
			)
			if err := copyStorageToFile(w.storage, key, localPath); err != nil {
				return nil, errors.Join(
					errStreamKillfeedArtifactsStale,
					fmt.Errorf(
						"materialize exact killfeed artifact for clip %s event %s row %d: %w; rerun killfeed analysis",
						clip.ID, event.EventID, rowIndex, err,
					),
				)
			}
			if err := validateExactKillfeedPNG(localPath, event.Rows[rowIndex].SampleBounds); err != nil {
				return nil, errors.Join(
					errStreamKillfeedArtifactsStale,
					fmt.Errorf(
						"validate exact killfeed artifact for clip %s event %s row %d: %w; rerun killfeed analysis",
						clip.ID, event.EventID, rowIndex, err,
					),
				)
			}
			cuePaths[rowIndex] = localPath
		}
		paths[cueIndex] = cuePaths
	}
	return paths, nil
}

func validateExactKillfeedPNG(path string, sampleBounds streamclips.NoticeRow) error {
	if sampleBounds.Width <= 0 || sampleBounds.Height <= 0 {
		return fmt.Errorf("sample row bounds must be positive")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	decoded, decodeErr := png.Decode(f)
	closeErr := f.Close()
	if decodeErr != nil {
		return errors.Join(decodeErr, closeErr)
	}
	expectedHeight := streamclips.KillfeedNoticeHeight
	expectedWidth := (sampleBounds.Width*expectedHeight + sampleBounds.Height/2) / sampleBounds.Height
	if expectedWidth < 1 {
		expectedWidth = 1
	}
	gotWidth := decoded.Bounds().Dx()
	gotHeight := decoded.Bounds().Dy()
	if gotWidth != expectedWidth || gotHeight != expectedHeight {
		decodeErr = fmt.Errorf(
			"png dimensions are %dx%d, want %dx%d",
			gotWidth, gotHeight, expectedWidth, expectedHeight,
		)
	}
	return errors.Join(decodeErr, closeErr)
}

func readKillfeedAnalysisState(
	store storage.Storage,
	key string,
) (streamclips.KillfeedAnalysisState, bool, error) {
	rc, err := store.Open(key)
	if err != nil {
		if storage.IsNotExist(err) {
			return streamclips.KillfeedAnalysisState{}, false, nil
		}
		return streamclips.KillfeedAnalysisState{}, false, err
	}
	var state streamclips.KillfeedAnalysisState
	decodeErr := json.NewDecoder(rc).Decode(&state)
	closeErr := rc.Close()
	if decodeErr != nil {
		return streamclips.KillfeedAnalysisState{}, false, decodeErr
	}
	if closeErr != nil {
		return streamclips.KillfeedAnalysisState{}, false, closeErr
	}
	return state, true, nil
}

func recordStreamKillfeedFailure(id uuid.UUID, err error) {
	recorder := obs.Default()
	if recorder == nil {
		return
	}
	_ = recorder.RecordError(obs.Event{
		Stage:   obs.StageStreamKillfeed,
		Class:   "analysis_failed",
		Message: id.String() + ": " + err.Error(),
	})
}
