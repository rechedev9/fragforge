package streamclips

import (
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
)

func JobPrefix(id uuid.UUID) string {
	return path.Join("stream-jobs", id.String())
}

func SourceKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "source.mp4")
}

func EditPlanKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "edit-plan.json")
}

func CaptionCandidatesKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "captions", "candidates.json")
}

func CaptionCandidateGenerationKey(id, generationID uuid.UUID) (string, error) {
	if generationID == uuid.Nil {
		return "", fmt.Errorf("caption generation id is required")
	}
	return path.Join(JobPrefix(id), "captions", "generations", generationID.String()+".json"), nil
}

// KillfeedAnalysisKey is the active-generation pointer. Its document also
// carries the latest state so GET remains useful if a legacy generation file
// has not been materialized yet.
func KillfeedAnalysisKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "killfeed", "analysis.json")
}

func KillfeedAnalysisGenerationKey(id, generationID uuid.UUID) (string, error) {
	if generationID == uuid.Nil {
		return "", fmt.Errorf("killfeed analysis generation id is required")
	}
	return path.Join(JobPrefix(id), "killfeed", "generations", generationID.String()+".json"), nil
}

// KillfeedEventRowKey identifies the immutable PNG captured for one row born
// in one detector event. All path components are validated before joining so
// detector or persisted-state data cannot escape the job artifact prefix.
func KillfeedEventRowKey(id, generationID uuid.UUID, clipID, eventID string, rowIndex int) (string, error) {
	if generationID == uuid.Nil {
		return "", fmt.Errorf("killfeed analysis generation id is required")
	}
	if !clipIDPattern.MatchString(clipID) {
		return "", fmt.Errorf("invalid clip id %q", clipID)
	}
	if !clipIDPattern.MatchString(eventID) {
		return "", fmt.Errorf("invalid killfeed event id %q", eventID)
	}
	if rowIndex < 0 {
		return "", fmt.Errorf("killfeed row index must be >= 0")
	}
	return path.Join(
		JobPrefix(id), "killfeed", "generations", generationID.String(),
		"events", clipID, eventID, fmt.Sprintf("row-%03d.png", rowIndex),
	), nil
}

func RenderPrefix(id uuid.UUID, variant string) (string, error) {
	if _, ok := VariantByName(variant); !ok {
		return "", unknownVariantError(variant)
	}
	return path.Join(JobPrefix(id), "renders", variant), nil
}

func RenderStateKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "status.json"), nil
}

func RenderResultKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "render-result.json"), nil
}

func RenderGalleryKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "index.html"), nil
}

func RenderVideoKey(id uuid.UUID, variant, clipID string) (string, error) {
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if !clipIDPattern.MatchString(clipID) {
		return "", fmt.Errorf("invalid clip id %q", clipID)
	}
	return path.Join(prefix, "videos", clipID+".mp4"), nil
}

// RenderCaptionKey returns the storage key for a clip's burned-caption ASS
// track, stored next to the rendered videos under the render artifact prefix.
func RenderCaptionKey(id uuid.UUID, variant, clipID string) (string, error) {
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if !clipIDPattern.MatchString(clipID) {
		return "", fmt.Errorf("invalid clip id %q", clipID)
	}
	return path.Join(prefix, "captions", clipID+".ass"), nil
}

// RenderRevisionPrefix is an immutable publication namespace for one render
// attempt. status.json is the sole mutable pointer to a completed revision, so
// an interrupted upload cannot overwrite artifacts referenced by an older
// successful state.
func RenderRevisionPrefix(id uuid.UUID, variant string, revisionID uuid.UUID) (string, error) {
	prefix, err := RenderPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if revisionID == uuid.Nil {
		return "", fmt.Errorf("render revision id is required")
	}
	return path.Join(prefix, "revisions", revisionID.String()), nil
}

func RenderRevisionResultKey(id uuid.UUID, variant string, revisionID uuid.UUID) (string, error) {
	prefix, err := RenderRevisionPrefix(id, variant, revisionID)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "render-result.json"), nil
}

func RenderRevisionGalleryKey(id uuid.UUID, variant string, revisionID uuid.UUID) (string, error) {
	prefix, err := RenderRevisionPrefix(id, variant, revisionID)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "index.html"), nil
}

func RenderRevisionVideoKey(id uuid.UUID, variant string, revisionID uuid.UUID, clipID string) (string, error) {
	prefix, err := RenderRevisionPrefix(id, variant, revisionID)
	if err != nil {
		return "", err
	}
	if !clipIDPattern.MatchString(clipID) {
		return "", fmt.Errorf("invalid clip id %q", clipID)
	}
	return path.Join(prefix, "videos", clipID+".mp4"), nil
}

func RenderRevisionCaptionKey(id uuid.UUID, variant string, revisionID uuid.UUID, clipID string) (string, error) {
	prefix, err := RenderRevisionPrefix(id, variant, revisionID)
	if err != nil {
		return "", err
	}
	if !clipIDPattern.MatchString(clipID) {
		return "", fmt.Errorf("invalid clip id %q", clipID)
	}
	return path.Join(prefix, "captions", clipID+".ass"), nil
}

// ValidateRenderStateArtifacts ensures every key exposed by the mutable render
// state points either at the legacy canonical namespace or at one exact
// immutable revision owned by the same job and variant.
func ValidateRenderStateArtifacts(state RenderState) error {
	if state.JobID == uuid.Nil {
		return fmt.Errorf("render job id is required")
	}
	base, err := RenderPrefix(state.JobID, state.Variant)
	if err != nil {
		return err
	}
	artifactDir := state.ArtifactDir
	if artifactDir == "" {
		return fmt.Errorf("render artifact dir is required")
	}
	wantResult := ""
	wantGallery := ""
	videoDir := ""
	switch {
	case artifactDir == base:
		wantResult, err = RenderResultKey(state.JobID, state.Variant)
		if err == nil {
			wantGallery, err = RenderGalleryKey(state.JobID, state.Variant)
		}
		videoDir = path.Join(base, "videos")
	default:
		revisionPrefix := base + "/revisions/"
		revisionText := strings.TrimPrefix(artifactDir, revisionPrefix)
		if revisionText == artifactDir || revisionText == "" || strings.Contains(revisionText, "/") {
			return fmt.Errorf("render artifact dir is outside the job revision namespace")
		}
		revisionID, parseErr := uuid.Parse(revisionText)
		if parseErr != nil || revisionID == uuid.Nil {
			return fmt.Errorf("render artifact dir has an invalid revision id")
		}
		wantPrefix, prefixErr := RenderRevisionPrefix(state.JobID, state.Variant, revisionID)
		if prefixErr != nil || wantPrefix != artifactDir {
			return fmt.Errorf("render artifact dir does not match its revision")
		}
		wantResult, err = RenderRevisionResultKey(state.JobID, state.Variant, revisionID)
		if err == nil {
			wantGallery, err = RenderRevisionGalleryKey(state.JobID, state.Variant, revisionID)
		}
		videoDir = path.Join(artifactDir, "videos")
	}
	if err != nil {
		return err
	}
	if state.ResultKey != wantResult || state.GalleryKey != wantGallery {
		return fmt.Errorf("render result or gallery key does not match artifact dir")
	}
	for _, video := range state.Videos {
		videoName := strings.TrimSuffix(path.Base(video.Key), ".mp4")
		if video.Key == "" || path.Clean(video.Key) != video.Key || path.Dir(video.Key) != videoDir ||
			path.Ext(video.Key) != ".mp4" || !clipIDPattern.MatchString(videoName) {
			return fmt.Errorf("render video key %q is outside artifact dir", video.Key)
		}
	}
	return nil
}
