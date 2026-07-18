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

const streamKillfeedUniqueTTL = 24 * time.Hour

type applyStreamKillfeedRequest struct {
	GenerationID uuid.UUID `json:"generation_id"`
}

// StartStreamKillfeedAnalysis activates a new durable generation before its
// task becomes visible. FFmpeg is the only required external capability: xAI
// may enrich rows with structured kills, but empty-kill PTS events remain a
// complete renderable result through their exact captured row PNGs.
func (h *Handlers) StartStreamKillfeedAnalysis(w http.ResponseWriter, r *http.Request) {
	releaseJob := h.lockStreamJobRequest(r)
	defer releaseJob()
	h.streamPlanMu.Lock()
	defer h.streamPlanMu.Unlock()

	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if j.Status != streamclips.StatusReady && j.Status != streamclips.StatusRendered {
		writeError(w, http.StatusConflict, fmt.Sprintf("stream job is not ready for killfeed analysis (status=%s)", j.Status))
		return
	}
	if !h.requireKillfeedFFmpeg(w) {
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
	if plan.KillfeedCrop == nil {
		writeError(w, http.StatusConflict, "killfeed analysis requires killfeed_crop in the edit plan")
		return
	}
	if len(plan.Clips) == 0 {
		writeError(w, http.StatusBadRequest, "stream edit plan has no clips")
		return
	}
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(j.SourceSHA256, *plan.KillfeedCrop, plan.Clips)
	if err != nil {
		writeError(w, http.StatusConflict, "cannot fingerprint killfeed analysis inputs: "+err.Error())
		return
	}

	generationID := uuid.New()
	state := streamclips.KillfeedAnalysisState{
		JobID:        j.ID,
		GenerationID: generationID,
		Status:       streamclips.KillfeedAnalysisQueued,
		SourceSHA256: j.SourceSHA256,
		KillfeedCrop: *plan.KillfeedCrop,
		Fingerprint:  fingerprint,
		Clips:        make([]streamclips.KillfeedAnalysisClip, len(plan.Clips)),
		UpdatedAt:    time.Now().UTC(),
	}
	for i, clip := range plan.Clips {
		state.Clips[i] = streamclips.KillfeedAnalysisClip{
			ClipID:       clip.ID,
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
			Events:       []streamclips.KillfeedAnalysisEvent{},
		}
	}
	task, err := tasks.NewGenerateStreamKillfeedTask(j.ID, generationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = h.queue.EnqueueWithTransition(task, func(decision error) error {
		switch {
		case decision == nil:
			return h.writeStreamKillfeedState(state)
		case errors.Is(decision, asynq.ErrDuplicateTask):
			existing, exists, readErr := h.readStreamKillfeedState(j.ID)
			if readErr != nil {
				return readErr
			}
			if exists && existing.GenerationID == generationID {
				state = existing
				return nil
			}
			return h.writeStreamKillfeedState(state)
		default:
			state.Status = streamclips.KillfeedAnalysisFailed
			state.Error = "enqueue killfeed analysis: " + decision.Error()
			state.UpdatedAt = time.Now().UTC()
			return h.writeStreamKillfeedFailureState(state)
		}
	}, asynq.Unique(streamKillfeedUniqueTTL), asynq.MaxRetry(0))
	if err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
		internalError(w, "enqueue stream killfeed analysis", err)
		return
	}
	writeJSON(w, http.StatusAccepted, state)
}

func (h *Handlers) GetStreamKillfeedAnalysis(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	state, exists, err := h.readStreamKillfeedState(j.ID)
	if err != nil {
		internalError(w, "read stream killfeed analysis", err)
		return
	}
	if !exists {
		state = streamclips.KillfeedAnalysisState{
			JobID:     j.ID,
			Status:    streamclips.KillfeedAnalysisNone,
			Clips:     []streamclips.KillfeedAnalysisClip{},
			UpdatedAt: time.Now().UTC(),
		}
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) ApplyStreamKillfeedAnalysis(w http.ResponseWriter, r *http.Request) {
	releaseJob := h.lockStreamJobRequest(r)
	defer releaseJob()
	h.streamPlanMu.Lock()
	defer h.streamPlanMu.Unlock()

	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if j.Status == streamclips.StatusRendering {
		writeError(w, http.StatusConflict, "killfeed analysis cannot be applied while a render is running")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var request applyStreamKillfeedRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "killfeed apply JSON is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid killfeed apply JSON")
		return
	}
	state, exists, err := h.readStreamKillfeedState(j.ID)
	if err != nil {
		internalError(w, "read stream killfeed analysis", err)
		return
	}
	if !exists {
		writeError(w, http.StatusConflict, "stream killfeed analysis has not been generated")
		return
	}
	if request.GenerationID == uuid.Nil || request.GenerationID != state.GenerationID {
		writeError(w, http.StatusConflict, "killfeed analysis generation was replaced; reload before applying")
		return
	}
	// Applied is accepted as an idempotent repair. The generation artifact is
	// written before the active pointer, so a storage failure on that second
	// write can leave the authoritative generation applied while the pointer
	// body still says ready. Repeating Apply repairs only that pointer: the
	// current plan may already contain reviewed OCR or manual corrections that
	// must not be replaced with the original detector payload.
	if state.Status != streamclips.KillfeedAnalysisReady &&
		state.Status != streamclips.KillfeedAnalysisApplied {
		writeError(w, http.StatusConflict, fmt.Sprintf("killfeed analysis is not ready to apply (status=%s)", state.Status))
		return
	}
	if err := state.Validate(); err != nil {
		writeError(w, http.StatusConflict, "killfeed analysis is invalid: "+err.Error())
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
	if plan.KillfeedCrop == nil {
		writeError(w, http.StatusConflict, "killfeed analysis is stale because killfeed_crop is no longer configured")
		return
	}
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(j.SourceSHA256, *plan.KillfeedCrop, plan.Clips)
	if err != nil {
		writeError(w, http.StatusConflict, "cannot fingerprint current killfeed inputs: "+err.Error())
		return
	}
	if fingerprint != state.Fingerprint {
		writeError(w, http.StatusConflict, "killfeed analysis is stale; source, crop, or clip bounds changed")
		return
	}
	if state.Status == streamclips.KillfeedAnalysisApplied {
		if err := validateRenderableKillfeedCues(plan, state); err != nil {
			writeError(w, http.StatusConflict, "cannot repair applied killfeed analysis: "+err.Error())
			return
		}
		if err := h.writeStreamKillfeedActivePointer(state); err != nil {
			internalError(w, "repair applied stream killfeed pointer", err)
			return
		}
		writeJSON(w, http.StatusOK, plan)
		return
	}

	stateClips := make(map[string]streamclips.KillfeedAnalysisClip, len(state.Clips))
	for _, clip := range state.Clips {
		stateClips[clip.ClipID] = clip
	}
	if len(stateClips) != len(plan.Clips) {
		writeError(w, http.StatusConflict, "killfeed analysis clips do not match the current edit plan")
		return
	}
	for i := range plan.Clips {
		candidate, found := stateClips[plan.Clips[i].ID]
		if !found {
			writeError(w, http.StatusConflict, fmt.Sprintf("killfeed analysis has no candidate for clip %q", plan.Clips[i].ID))
			return
		}
		cues := make([]float64, len(candidate.Events))
		kills := make([][]streamclips.KillfeedKill, len(candidate.Events))
		provenance := make([]streamclips.KillfeedCueProvenance, len(candidate.Events))
		for eventIndex, event := range candidate.Events {
			cues[eventIndex] = event.CueSeconds
			kills[eventIndex] = append([]streamclips.KillfeedKill(nil), event.Kills...)
			provenance[eventIndex] = streamclips.KillfeedCueProvenance{
				CueSeconds: event.CueSeconds,
				Origin:     streamclips.KillfeedCueAutomatic,
				EventID:    event.EventID,
			}
		}
		plan.Clips[i].KillfeedSeconds = cues
		plan.Clips[i].KillfeedKills = kills
		plan.Clips[i].KillfeedCueProvenance = provenance
	}
	appliedAt := time.Now().UTC()
	plan.KillfeedAnalysis = &streamclips.KillfeedAnalysisMetadata{
		GenerationID: state.GenerationID,
		Fingerprint:  state.Fingerprint,
		AppliedAt:    appliedAt,
	}
	plan.UpdatedAt = appliedAt
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		writeError(w, http.StatusConflict, "applied killfeed analysis is invalid: "+err.Error())
		return
	}
	if err := h.streamRepo.SetEditPlan(r.Context(), j.ID, plan); err != nil {
		internalError(w, "save applied stream killfeed analysis", err)
		return
	}
	if err := h.writeStreamEditPlanArtifact(j.ID, plan); err != nil {
		internalError(w, "write applied stream edit plan", err)
		return
	}
	state.Status = streamclips.KillfeedAnalysisApplied
	state.Error = ""
	state.UpdatedAt = appliedAt
	if err := h.writeStreamKillfeedState(state); err != nil {
		internalError(w, "write applied stream killfeed state", err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *Handlers) writeStreamKillfeedState(state streamclips.KillfeedAnalysisState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate stream killfeed analysis state: %w", err)
	}
	if err := h.writeStreamKillfeedGenerationState(state); err != nil {
		return err
	}
	return h.writeStreamKillfeedActivePointer(state)
}

func (h *Handlers) writeStreamKillfeedActivePointer(state streamclips.KillfeedAnalysisState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate stream killfeed analysis pointer: %w", err)
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(streamclips.KillfeedAnalysisKey(state.JobID), bytes.NewReader(append(b, '\n')))
}

func (h *Handlers) writeStreamKillfeedGenerationState(state streamclips.KillfeedAnalysisState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate stream killfeed analysis state: %w", err)
	}
	key, err := streamclips.KillfeedAnalysisGenerationKey(state.JobID, state.GenerationID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(key, bytes.NewReader(append(b, '\n')))
}

func (h *Handlers) writeStreamKillfeedFailureState(state streamclips.KillfeedAnalysisState) error {
	current, exists, err := h.readStreamKillfeedState(state.JobID)
	if err != nil {
		return err
	}
	if !exists || current.GenerationID == state.GenerationID {
		return h.writeStreamKillfeedState(state)
	}
	return h.writeStreamKillfeedGenerationState(state)
}

func (h *Handlers) readStreamKillfeedState(id uuid.UUID) (streamclips.KillfeedAnalysisState, bool, error) {
	rc, err := h.storage.Open(streamclips.KillfeedAnalysisKey(id))
	if err != nil {
		if storageNotExist(err) {
			return streamclips.KillfeedAnalysisState{}, false, nil
		}
		return streamclips.KillfeedAnalysisState{}, false, err
	}
	defer rc.Close()
	var state streamclips.KillfeedAnalysisState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("decode stream killfeed analysis pointer: %w", err)
	}
	if state.JobID != id {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("stream killfeed analysis pointer job id does not match its key")
	}
	activeGenerationID := state.GenerationID
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, activeGenerationID)
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, false, err
	}
	generation, err := h.storage.Open(generationKey)
	if err != nil {
		if storageNotExist(err) {
			if err := state.Validate(); err != nil {
				return streamclips.KillfeedAnalysisState{}, false, err
			}
			return state, true, nil
		}
		return streamclips.KillfeedAnalysisState{}, false, err
	}
	defer generation.Close()
	if err := json.NewDecoder(generation).Decode(&state); err != nil {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("decode stream killfeed analysis generation: %w", err)
	}
	if state.JobID != id || state.GenerationID != activeGenerationID {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("stream killfeed analysis generation does not match its active pointer")
	}
	if err := state.Validate(); err != nil {
		return streamclips.KillfeedAnalysisState{}, false, fmt.Errorf("validate stream killfeed analysis generation: %w", err)
	}
	return state, true, nil
}

func (h *Handlers) currentAppliedStreamKillfeed(j streamclips.Job, plan streamclips.EditPlan) (bool, error) {
	_, current, err := h.currentAppliedStreamKillfeedAnalysis(j, plan)
	return current, err
}

func (h *Handlers) currentAppliedStreamKillfeedAnalysis(
	j streamclips.Job,
	plan streamclips.EditPlan,
) (streamclips.KillfeedAnalysisState, bool, error) {
	if plan.KillfeedCrop == nil || plan.KillfeedAnalysis == nil {
		return streamclips.KillfeedAnalysisState{}, false, nil
	}
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(j.SourceSHA256, *plan.KillfeedCrop, plan.Clips)
	if err != nil {
		return streamclips.KillfeedAnalysisState{}, false, err
	}
	if plan.KillfeedAnalysis.Fingerprint != fingerprint {
		return streamclips.KillfeedAnalysisState{}, false, nil
	}
	state, exists, err := h.readStreamKillfeedState(j.ID)
	if err != nil || !exists {
		return streamclips.KillfeedAnalysisState{}, false, err
	}
	current := state.Status == streamclips.KillfeedAnalysisApplied &&
		state.GenerationID == plan.KillfeedAnalysis.GenerationID &&
		state.Fingerprint == fingerprint
	return state, current, nil
}

// validateRenderableKillfeedCues proves every empty structured cue came from
// an exact event in the applied generation. A cue without either proof is a
// manual placeholder; rendering it would otherwise fail later in the worker or
// fall back to an approximate crop.
func validateRenderableKillfeedCues(
	plan streamclips.EditPlan,
	state streamclips.KillfeedAnalysisState,
) error {
	if plan.KillfeedAnalysis == nil ||
		plan.KillfeedAnalysis.GenerationID != state.GenerationID ||
		plan.KillfeedAnalysis.Fingerprint != state.Fingerprint {
		return errors.New("edit-plan metadata does not identify this applied generation")
	}
	stateClips := make(map[string]streamclips.KillfeedAnalysisClip, len(state.Clips))
	for _, clip := range state.Clips {
		stateClips[clip.ClipID] = clip
	}
	if len(stateClips) != len(plan.Clips) {
		return errors.New("analysis clips do not match the current edit plan")
	}
	for _, clip := range plan.Clips {
		analyzed, ok := stateClips[clip.ID]
		if !ok {
			return fmt.Errorf("analysis has no clip %q", clip.ID)
		}
		exactCues := make(map[float64]bool, len(analyzed.Events))
		for _, event := range analyzed.Events {
			exactCues[event.CueSeconds] = false
		}
		for cueIndex, cue := range clip.KillfeedSeconds {
			if _, exact := exactCues[cue]; exact {
				exactCues[cue] = true
				continue
			}
			if cueIndex < len(clip.KillfeedKills) && len(clip.KillfeedKills[cueIndex]) > 0 {
				continue
			}
			return fmt.Errorf(
				"clip %q cue %.9f has no exact captured event and no reviewed kills; read or enter its kills, or remove the cue before rendering",
				clip.ID, cue,
			)
		}
		for cue, present := range exactCues {
			if !present {
				return fmt.Errorf(
					"clip %q is missing exact analyzed cue %.9f; reapply the current killfeed analysis before rendering",
					clip.ID, cue,
				)
			}
		}
	}
	return nil
}
