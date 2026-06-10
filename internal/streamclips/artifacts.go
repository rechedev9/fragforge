package streamclips

import (
	"fmt"
	"path"

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

func RenderPrefix(id uuid.UUID, variant string) (string, error) {
	if variant != VariantStreamerVerticalStack {
		return "", fmt.Errorf("unsupported stream render variant %q", variant)
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
