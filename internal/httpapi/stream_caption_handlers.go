package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const streamCaptionUniqueTTL = 24 * time.Hour

type reviewStreamCaptionsRequest struct {
	GenerationID uuid.UUID                 `json:"generation_id"`
	Clips        []reviewStreamCaptionClip `json:"clips"`
}

type reviewStreamCaptionClip struct {
	ClipID   string                    `json:"clip_id"`
	Words    []streamclips.CaptionWord `json:"words,omitempty"`
	NoSpeech bool                      `json:"no_speech,omitempty"`
}

func (h *Handlers) StartStreamCaptionCandidates(w http.ResponseWriter, r *http.Request) {
	releaseJob := h.lockStreamJobRequest(r)
	defer releaseJob()
	h.streamPlanMu.Lock()
	defer h.streamPlanMu.Unlock()
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if j.Status != streamclips.StatusReady && j.Status != streamclips.StatusRendered {
		writeError(w, http.StatusConflict, fmt.Sprintf("stream job is not ready for captions (status=%s)", j.Status))
		return
	}
	plan, err := h.currentStreamEditPlan(j)
	if err != nil {
		internalError(w, "load stream edit plan", err)
		return
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !plan.Captions.Enabled {
		writeError(w, http.StatusConflict, "captions are not enabled in the edit plan")
		return
	}
	if len(plan.Clips) == 0 {
		writeError(w, http.StatusBadRequest, "stream edit plan has no clips")
		return
	}
	if !plan.CaptionsNeedBackend() || j.Probe.AudioCodec == "" {
		state, err := readyCaptionCandidateState(j, plan)
		if err != nil {
			internalError(w, "build stream caption state", err)
			return
		}
		if err := h.writeStreamCaptionState(state); err != nil {
			internalError(w, "write stream caption state", err)
			return
		}
		writeJSON(w, http.StatusOK, state)
		return
	}
	// xAI is needed only to generate missing candidates. Reviewed plans render
	// without cloud credentials and never reach this gate.
	if !h.requireCaptionsEnabled(w) {
		return
	}
	generationID := uuid.New()
	task, err := tasks.NewGenerateStreamCaptionsTask(j.ID, generationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := streamclips.CaptionCandidateState{
		JobID:        j.ID,
		GenerationID: generationID,
		Status:       streamclips.CaptionCandidatesQueued,
		Clips:        []streamclips.CaptionCandidateClip{},
		UpdatedAt:    time.Now().UTC(),
	}
	_, err = h.queue.EnqueueWithTransition(task, func(decision error) error {
		switch {
		case decision == nil:
			return h.writeStreamCaptionState(state)
		case errors.Is(decision, asynq.ErrDuplicateTask):
			existing, ok, readErr := h.readStreamCaptionState(j.ID)
			if readErr != nil {
				return readErr
			}
			if ok {
				state = existing
			}
			return nil
		default:
			state.Status = streamclips.CaptionCandidatesFailed
			state.Error = "enqueue captions: " + decision.Error()
			state.UpdatedAt = time.Now().UTC()
			return h.writeStreamCaptionFailureState(state)
		}
	}, asynq.Unique(streamCaptionUniqueTTL), asynq.MaxRetry(0))
	if err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
		internalError(w, "enqueue stream captions", err)
		return
	}
	writeJSON(w, http.StatusAccepted, state)
}

func (h *Handlers) GetStreamCaptionCandidates(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	state, exists, err := h.readStreamCaptionState(j.ID)
	if err != nil {
		internalError(w, "read stream caption state", err)
		return
	}
	if !exists {
		state = streamclips.CaptionCandidateState{
			JobID:     j.ID,
			Status:    streamclips.CaptionCandidatesNone,
			Clips:     []streamclips.CaptionCandidateClip{},
			UpdatedAt: time.Now().UTC(),
		}
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) ReviewStreamCaptionCandidates(w http.ResponseWriter, r *http.Request) {
	releaseJob := h.lockStreamJobRequest(r)
	defer releaseJob()
	h.streamPlanMu.Lock()
	defer h.streamPlanMu.Unlock()
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if j.Status == streamclips.StatusRendering {
		writeError(w, http.StatusConflict, "caption review cannot change the edit plan while a render is running")
		return
	}
	state, exists, err := h.readStreamCaptionState(j.ID)
	if err != nil {
		internalError(w, "read stream caption state", err)
		return
	}
	if !exists {
		writeError(w, http.StatusConflict, "stream caption candidates have not been generated")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var request reviewStreamCaptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "caption review JSON is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid caption review JSON")
		return
	}
	if len(request.Clips) == 0 {
		writeError(w, http.StatusBadRequest, "caption review requires at least one clip")
		return
	}
	if request.GenerationID == uuid.Nil || request.GenerationID != state.GenerationID {
		writeError(w, http.StatusConflict, "caption generation was replaced; reload candidates before reviewing")
		return
	}
	if state.Status != streamclips.CaptionCandidatesReviewRequired &&
		state.Status != streamclips.CaptionCandidatesFailed &&
		state.Status != streamclips.CaptionCandidatesReady {
		writeError(w, http.StatusConflict, fmt.Sprintf("caption candidates are not ready for review (status=%s)", state.Status))
		return
	}
	plan, err := h.currentStreamEditPlan(j)
	if err != nil {
		internalError(w, "load stream edit plan", err)
		return
	}
	plan = streamclips.NormalizeEditPlan(plan)
	planClips := make(map[string]int, len(plan.Clips))
	for i, clip := range plan.Clips {
		planClips[clip.ID] = i
	}
	stateClips := make(map[string]int, len(state.Clips))
	for i, clip := range state.Clips {
		stateClips[clip.ClipID] = i
	}
	seen := make(map[string]struct{}, len(request.Clips))
	for _, decision := range request.Clips {
		if _, duplicate := seen[decision.ClipID]; duplicate {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("duplicate caption review for clip %q", decision.ClipID))
			return
		}
		seen[decision.ClipID] = struct{}{}
		planIndex, found := planClips[decision.ClipID]
		if !found {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown caption clip %q", decision.ClipID))
			return
		}
		stateIndex, found := stateClips[decision.ClipID]
		if !found {
			writeError(w, http.StatusConflict, fmt.Sprintf("clip %q has no generated caption candidate", decision.ClipID))
			return
		}
		clip := plan.Clips[planIndex]
		fingerprint, err := streamclips.CaptionClipFingerprint(j.SourceSHA256, clip)
		if err != nil {
			internalError(w, "fingerprint stream caption clip", err)
			return
		}
		if fingerprint != state.Clips[stateIndex].Fingerprint {
			writeError(w, http.StatusConflict, fmt.Sprintf("caption candidate for clip %q is stale; generate it again", decision.ClipID))
			return
		}
		if decision.NoSpeech && len(decision.Words) > 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("clip %q cannot contain words and be marked no_speech", decision.ClipID))
			return
		}
		if !decision.NoSpeech && len(decision.Words) == 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("clip %q needs reviewed words or no_speech=true", decision.ClipID))
			return
		}
		clip.CaptionWords = append([]streamclips.CaptionWord(nil), decision.Words...)
		clip.CaptionReviewed = true
		plan.Clips[planIndex] = clip
		state.Clips[stateIndex].CandidateWords = append([]streamclips.CaptionWord(nil), decision.Words...)
		if decision.NoSpeech {
			state.Clips[stateIndex].Status = streamclips.CaptionClipNoSpeech
		} else {
			state.Clips[stateIndex].Status = streamclips.CaptionClipReady
		}
		state.Clips[stateIndex].Error = ""
	}
	plan = streamclips.NormalizeEditPlan(plan)
	plan.UpdatedAt = time.Now().UTC()
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.streamRepo.SetEditPlan(r.Context(), j.ID, plan); err != nil {
		internalError(w, "save reviewed stream captions", err)
		return
	}
	if err := h.writeStreamEditPlanArtifact(j.ID, plan); err != nil {
		internalError(w, "write reviewed stream edit plan", err)
		return
	}
	state.Status = captionStateStatus(plan, state)
	state.Error = ""
	if state.Status == streamclips.CaptionCandidatesFailed {
		state.Error = "one or more clips failed caption generation"
	}
	state.UpdatedAt = time.Now().UTC()
	if err := h.writeStreamCaptionGenerationState(state); err != nil {
		internalError(w, "write reviewed stream caption state", err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func readyCaptionCandidateState(j streamclips.Job, plan streamclips.EditPlan) (streamclips.CaptionCandidateState, error) {
	state := streamclips.CaptionCandidateState{
		JobID:        j.ID,
		GenerationID: uuid.New(),
		Status:       streamclips.CaptionCandidatesReady,
		Clips:        []streamclips.CaptionCandidateClip{},
		UpdatedAt:    time.Now().UTC(),
	}
	for _, clip := range plan.Clips {
		fingerprint, err := streamclips.CaptionClipFingerprint(j.SourceSHA256, clip)
		if err != nil {
			return streamclips.CaptionCandidateState{}, err
		}
		state.Clips = append(state.Clips, streamclips.CaptionCandidateClip{
			ClipID:         clip.ID,
			StartSeconds:   clip.StartSeconds,
			EndSeconds:     clip.EndSeconds,
			Fingerprint:    fingerprint,
			Status:         streamclips.CaptionClipReady,
			CandidateWords: append([]streamclips.CaptionWord(nil), clip.CaptionWords...),
		})
	}
	if j.Probe.AudioCodec == "" {
		state.Warnings = []string{"source has no audio; no caption candidates are needed"}
	}
	return state, nil
}

func captionStateStatus(plan streamclips.EditPlan, state streamclips.CaptionCandidateState) streamclips.CaptionCandidateStatus {
	if !plan.CaptionsNeedBackend() {
		return streamclips.CaptionCandidatesReady
	}
	for _, clip := range state.Clips {
		if clip.Status == streamclips.CaptionClipFailed {
			return streamclips.CaptionCandidatesFailed
		}
	}
	return streamclips.CaptionCandidatesReviewRequired
}

func (h *Handlers) writeStreamCaptionState(state streamclips.CaptionCandidateState) error {
	if err := h.writeStreamCaptionGenerationState(state); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(streamclips.CaptionCandidatesKey(state.JobID), bytes.NewReader(append(b, '\n')))
}

func (h *Handlers) writeStreamCaptionGenerationState(state streamclips.CaptionCandidateState) error {
	key, err := streamclips.CaptionCandidateGenerationKey(state.JobID, state.GenerationID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(key, bytes.NewReader(append(b, '\n')))
}

func (h *Handlers) writeStreamCaptionFailureState(state streamclips.CaptionCandidateState) error {
	_, exists, err := h.readStreamCaptionState(state.JobID)
	if err != nil {
		return err
	}
	if exists {
		// Updating the generation-specific artifact is enough: GET follows the
		// active pointer. In particular, a late discard from an older task must
		// never reactivate that generation over a newer request.
		return h.writeStreamCaptionGenerationState(state)
	}
	return h.writeStreamCaptionState(state)
}

func (h *Handlers) readStreamCaptionState(id uuid.UUID) (streamclips.CaptionCandidateState, bool, error) {
	rc, err := h.storage.Open(streamclips.CaptionCandidatesKey(id))
	if err != nil {
		if storageNotExist(err) {
			return streamclips.CaptionCandidateState{}, false, nil
		}
		return streamclips.CaptionCandidateState{}, false, err
	}
	defer rc.Close()
	var state streamclips.CaptionCandidateState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return streamclips.CaptionCandidateState{}, false, fmt.Errorf("decode stream caption state: %w", err)
	}
	generationKey, err := streamclips.CaptionCandidateGenerationKey(id, state.GenerationID)
	if err != nil {
		return streamclips.CaptionCandidateState{}, false, err
	}
	generation, err := h.storage.Open(generationKey)
	if err != nil {
		if storageNotExist(err) {
			return state, true, nil
		}
		return streamclips.CaptionCandidateState{}, false, err
	}
	defer generation.Close()
	if err := json.NewDecoder(generation).Decode(&state); err != nil {
		return streamclips.CaptionCandidateState{}, false, fmt.Errorf("decode stream caption generation state: %w", err)
	}
	return state, true, nil
}
