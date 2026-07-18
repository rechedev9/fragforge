package workers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// PrepareLocalKillfeedAnalysis runs the same source-PTS scan and exact row
// extraction as Studio, but synchronously for the CLI-first render path. The
// returned plan keeps the user's selected cues and reviewed structured kills;
// analysis metadata only proves where cue-only automatic notices came from.
func (w *StreamRenderWorker) PrepareLocalKillfeedAnalysis(
	ctx context.Context,
	j streamclips.Job,
	plan streamclips.EditPlan,
) (streamclips.EditPlan, error) {
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		return plan, err
	}
	if plan.KillfeedCrop == nil {
		return plan, fmt.Errorf("local exact killfeed analysis requires killfeed_crop")
	}

	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		j.SourceSHA256, *plan.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		return plan, fmt.Errorf("fingerprint local killfeed analysis: %w", err)
	}
	now := time.Now().UTC()
	state := streamclips.KillfeedAnalysisState{
		JobID:        j.ID,
		GenerationID: uuid.New(),
		Status:       streamclips.KillfeedAnalysisAnalyzing,
		SourceSHA256: j.SourceSHA256,
		KillfeedCrop: *plan.KillfeedCrop,
		Fingerprint:  fingerprint,
		Clips:        make([]streamclips.KillfeedAnalysisClip, len(plan.Clips)),
		UpdatedAt:    now,
	}
	for i, clip := range plan.Clips {
		state.Clips[i] = streamclips.KillfeedAnalysisClip{
			ClipID:       clip.ID,
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
			Events:       []streamclips.KillfeedAnalysisEvent{},
		}
	}
	if err := w.writeLocalKillfeedAnalysisState(state); err != nil {
		return plan, fmt.Errorf("initialize local killfeed analysis: %w", err)
	}

	state, err = w.generateStreamKillfeedAnalysis(ctx, j, state)
	if err != nil {
		state.Status = streamclips.KillfeedAnalysisFailed
		state.Error = singleLine(err)
		state.UpdatedAt = time.Now().UTC()
		if stateErr := w.writeLocalKillfeedAnalysisState(state); stateErr != nil {
			return plan, errors.Join(err, fmt.Errorf("write failed local killfeed analysis: %w", stateErr))
		}
		return plan, err
	}
	if state.Status != streamclips.KillfeedAnalysisReady {
		return plan, fmt.Errorf("local killfeed analysis finished with status %s", state.Status)
	}
	if err := w.writeLocalKillfeedAnalysisState(state); err != nil {
		return plan, fmt.Errorf("write ready local killfeed analysis: %w", err)
	}
	plan, err = bindLocalKillfeedCueProvenance(plan, state)
	if err != nil {
		return plan, err
	}

	appliedAt := time.Now().UTC()
	state.Status = streamclips.KillfeedAnalysisApplied
	state.Error = ""
	state.UpdatedAt = appliedAt
	if err := w.writeLocalKillfeedAnalysisState(state); err != nil {
		return plan, fmt.Errorf("apply local killfeed analysis: %w", err)
	}
	plan.KillfeedAnalysis = &streamclips.KillfeedAnalysisMetadata{
		GenerationID: state.GenerationID,
		Fingerprint:  state.Fingerprint,
		AppliedAt:    appliedAt,
	}
	plan.UpdatedAt = appliedAt
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		return plan, fmt.Errorf("validate locally analyzed edit plan: %w", err)
	}
	return plan, nil
}

func (w *StreamRenderWorker) writeLocalKillfeedAnalysisState(state streamclips.KillfeedAnalysisState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate local killfeed analysis: %w", err)
	}
	key, err := streamclips.KillfeedAnalysisGenerationKey(state.JobID, state.GenerationID)
	if err != nil {
		return err
	}
	// The generation is authoritative. Publish it before refreshing the active
	// selector, matching Studio's split-Put recovery contract.
	if err := putJSONToStorage(w.storage, key, state); err != nil {
		return err
	}
	return putJSONToStorage(w.storage, streamclips.KillfeedAnalysisKey(state.JobID), state)
}

func bindLocalKillfeedCueProvenance(
	plan streamclips.EditPlan,
	state streamclips.KillfeedAnalysisState,
) (streamclips.EditPlan, error) {
	clips := make(map[string]streamclips.KillfeedAnalysisClip, len(state.Clips))
	for _, clip := range state.Clips {
		clips[clip.ClipID] = clip
	}
	for clipIndex := range plan.Clips {
		clip := &plan.Clips[clipIndex]
		analysis, ok := clips[clip.ID]
		if !ok {
			return plan, fmt.Errorf("local killfeed analysis has no clip %s", clip.ID)
		}
		provenance := make([]streamclips.KillfeedCueProvenance, 0, len(clip.KillfeedSeconds))
		for cueIndex, cue := range clip.KillfeedSeconds {
			var matched *streamclips.KillfeedAnalysisEvent
			for _, event := range analysis.Events {
				if event.CueSeconds == cue {
					copy := event
					matched = &copy
					break
				}
			}
			reviewed := cueIndex < len(clip.KillfeedKills) && len(clip.KillfeedKills[cueIndex]) > 0
			current, explicit := clip.KillfeedProvenanceAt(cue)
			if !explicit {
				// Legacy reviewed cues are manual unless the saved plan already
				// carried authoritative analysis metadata. Detecting another cue
				// must not silently change an existing cue's visual source.
				current = streamclips.KillfeedCueProvenance{CueSeconds: cue}
				if plan.KillfeedAnalysis != nil {
					current.Origin = streamclips.KillfeedCueAutomatic
				} else if reviewed {
					current.Origin = streamclips.KillfeedCueManual
				} else {
					current.Origin = streamclips.KillfeedCueAutomatic
				}
			}
			if current.Origin == streamclips.KillfeedCueManual {
				if !reviewed {
					return plan, fmt.Errorf(
						"clip %s manual cue %.9f needs reviewed kills before rendering",
						clip.ID, cue,
					)
				}
				current.CueSeconds = cue
				current.EventID = ""
				provenance = append(provenance, current)
				continue
			}
			if matched == nil || (current.EventID != "" && current.EventID != matched.EventID) {
				return plan, fmt.Errorf(
					"clip %s cue %.9f has no exact captured killfeed event; rerun zv stream plan --detect-killfeed or provide reviewed kills",
					clip.ID, cue,
				)
			}
			current.CueSeconds = cue
			current.Origin = streamclips.KillfeedCueAutomatic
			current.EventID = matched.EventID
			provenance = append(provenance, current)
		}
		clip.KillfeedCueProvenance = provenance
	}
	return plan, nil
}
