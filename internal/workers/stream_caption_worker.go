package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/captions"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const captionSTTEndpoint = "/v1/stt"

var errCaptionGenerationSuperseded = errors.New("caption generation superseded")

// HandleGenerateStreamCaptions produces durable candidates only. Rendering is
// intentionally a separate operation and refuses to consume these words until
// the review endpoint copies them into the edit plan.
func (w *StreamRenderWorker) HandleGenerateStreamCaptions(ctx context.Context, t *asynq.Task) error {
	var payload tasks.GenerateStreamCaptionsPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	generationID, err := tasks.StreamCaptionGenerationFromTask(t)
	if err != nil {
		return err
	}
	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load stream job %s: %w", payload.JobID, err)
	}
	state := streamclips.CaptionCandidateState{
		JobID:        j.ID,
		GenerationID: generationID,
		Status:       streamclips.CaptionCandidatesGenerating,
		Clips:        []streamclips.CaptionCandidateClip{},
		UpdatedAt:    time.Now().UTC(),
	}
	owned, err := w.writeCaptionCandidateStateIfOwned(state)
	if err != nil {
		return fmt.Errorf("write generating caption state: %w", err)
	}
	if !owned {
		return nil
	}
	state, err = w.generateStreamCaptionCandidates(ctx, j, state)
	if err != nil {
		if errors.Is(err, errCaptionGenerationSuperseded) {
			return nil
		}
		state.Status = streamclips.CaptionCandidatesFailed
		state.Error = singleLine(err)
		state.UpdatedAt = time.Now().UTC()
		owned, stateErr := w.writeCaptionCandidateStateIfOwned(state)
		if stateErr != nil {
			return errors.Join(err, fmt.Errorf("write failed caption state: %w", stateErr))
		}
		if owned {
			recordStreamCaptionFailure(j.ID, err)
		}
		return err
	}
	owned, err = w.writeCaptionCandidateStateIfOwned(state)
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}
	return nil
}

func (w *StreamRenderWorker) generateStreamCaptionCandidates(ctx context.Context, j streamclips.Job, state streamclips.CaptionCandidateState) (streamclips.CaptionCandidateState, error) {
	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return state, err
	}
	if !cfg.xaiConfigured() {
		return state, fmt.Errorf("xai is required to generate stream caption candidates")
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
	if !plan.Captions.Enabled {
		return state, fmt.Errorf("captions are not enabled in the edit plan")
	}
	if len(plan.Clips) == 0 {
		return state, fmt.Errorf("edit plan has no clips")
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "stream-captions")
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

	state.Clips = make([]streamclips.CaptionCandidateClip, 0, len(plan.Clips))
	hasReview, hasFailure := false, false
	for _, clip := range plan.Clips {
		candidate, candidateErr := w.generateClipCaptionCandidate(runCtx, cfg, workDir, sourcePath, j, clip)
		state.Clips = append(state.Clips, candidate)
		switch candidate.Status {
		case streamclips.CaptionClipReviewRequired, streamclips.CaptionClipNoSpeech:
			hasReview = true
		case streamclips.CaptionClipFailed:
			hasFailure = true
		}
		if candidateErr != nil {
			state.Warnings = append(state.Warnings, fmt.Sprintf("clip %s: %s", clip.ID, singleLine(candidateErr)))
		}
		state.UpdatedAt = time.Now().UTC()
		owned, err := w.writeCaptionCandidateStateIfOwned(state)
		if err != nil {
			return state, err
		}
		if !owned {
			return state, errCaptionGenerationSuperseded
		}
	}
	switch {
	case hasFailure:
		state.Status = streamclips.CaptionCandidatesFailed
		state.Error = "one or more clips failed caption generation"
	case hasReview:
		state.Status = streamclips.CaptionCandidatesReviewRequired
	default:
		state.Status = streamclips.CaptionCandidatesReady
	}
	state.UpdatedAt = time.Now().UTC()
	if hasFailure {
		return state, errors.New(state.Error)
	}
	return state, nil
}

func (w *StreamRenderWorker) generateClipCaptionCandidate(ctx context.Context, cfg StreamRenderWorkerConfig, workDir, sourcePath string, j streamclips.Job, clip streamclips.ClipRange) (streamclips.CaptionCandidateClip, error) {
	fingerprint, err := streamclips.CaptionClipFingerprint(j.SourceSHA256, clip)
	if err != nil {
		return streamclips.CaptionCandidateClip{ClipID: clip.ID, Status: streamclips.CaptionClipFailed, Error: singleLine(err)}, err
	}
	result := streamclips.CaptionCandidateClip{
		ClipID:           clip.ID,
		StartSeconds:     clip.StartSeconds,
		EndSeconds:       clip.EndSeconds,
		Fingerprint:      fingerprint,
		Provider:         "xai",
		STTEndpoint:      captionSTTEndpoint,
		TranslationModel: captions.DefaultSpanishModel,
	}
	if clip.SourceAudioMuted() || j.Probe.AudioCodec == "" || clip.CaptionReviewed {
		result.Status = streamclips.CaptionClipReady
		result.CandidateWords = append([]streamclips.CaptionWord(nil), clip.CaptionWords...)
		return result, nil
	}
	transcriptionPath, err := w.extractCaptionSourceAudio(ctx, cfg, workDir, sourcePath, clip)
	if err != nil {
		return failedCaptionCandidate(result, err)
	}
	recoverTranscription := func() ([]captions.WordCue, error) {
		return w.recoverCaptionTranscript(ctx, cfg, workDir, sourcePath, transcriptionPath, clip, captionSourceLanguage)
	}
	sourceCues, err := w.transcribeCaptionCues(ctx, transcriptionPath, workDir, captionSourceLanguage, clip.EndSeconds-clip.StartSeconds, recoverTranscription)
	if err != nil {
		if errors.Is(err, captions.ErrUnusableTranscript) {
			result.Status = streamclips.CaptionClipNoSpeech
			return result, nil
		}
		return failedCaptionCandidate(result, fmt.Errorf("transcribe: %w", err))
	}
	result.SourceWords = captionWords(sourceCues)
	translated, err := w.translateToSpanish(ctx, sourceCues)
	if err != nil {
		return failedCaptionCandidate(result, fmt.Errorf("translate to spanish: %w", err))
	}
	if err := captions.ValidateTranscript(translated); err != nil {
		return failedCaptionCandidate(result, fmt.Errorf("validate translated captions: %w", err))
	}
	result.CandidateWords = captionWords(translated)
	reviewedClip := clip
	reviewedClip.CaptionWords = append([]streamclips.CaptionWord(nil), result.CandidateWords...)
	reviewedClip.CaptionReviewed = true
	if err := reviewedClip.Validate(); err != nil {
		return failedCaptionCandidate(result, fmt.Errorf("validate caption candidate: %w", err))
	}
	result.Status = streamclips.CaptionClipReviewRequired
	return result, nil
}

func failedCaptionCandidate(candidate streamclips.CaptionCandidateClip, err error) (streamclips.CaptionCandidateClip, error) {
	candidate.Status = streamclips.CaptionClipFailed
	candidate.Error = singleLine(err)
	return candidate, err
}

func captionWords(cues []captions.WordCue) []streamclips.CaptionWord {
	words := make([]streamclips.CaptionWord, len(cues))
	for i, cue := range cues {
		words[i] = streamclips.CaptionWord{
			Word:         cue.Word,
			StartSeconds: cue.StartSeconds,
			EndSeconds:   cue.EndSeconds,
		}
	}
	return words
}

func (w *StreamRenderWorker) writeCaptionCandidateStateIfOwned(state streamclips.CaptionCandidateState) (bool, error) {
	rc, err := w.storage.Open(streamclips.CaptionCandidatesKey(state.JobID))
	if err != nil {
		if storage.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var current streamclips.CaptionCandidateState
	decodeErr := json.NewDecoder(rc).Decode(&current)
	closeErr := rc.Close()
	if decodeErr != nil {
		return false, decodeErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if current.GenerationID != state.GenerationID {
		return false, nil
	}
	generationKey, err := streamclips.CaptionCandidateGenerationKey(state.JobID, state.GenerationID)
	if err != nil {
		return false, err
	}
	// Workers only write their generation-specific artifact. The active pointer
	// is owned by the HTTP admission boundary, so a superseded worker can never
	// replace a newer generation even if it races after this ownership check.
	if err := putJSONToStorage(w.storage, generationKey, state); err != nil {
		return false, err
	}
	return true, nil
}

func recordStreamCaptionFailure(id uuid.UUID, err error) {
	recorder := obs.Default()
	if recorder == nil {
		return
	}
	_ = recorder.RecordError(obs.Event{
		Stage:   obs.StageStreamCaptions,
		Class:   "generation_failed",
		Message: id.String() + ": " + err.Error(),
	})
}
