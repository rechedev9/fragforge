// Package artifacts defines durable object-storage keys for job outputs.
package artifacts

import (
	"fmt"
	"path"
	"regexp"

	"github.com/google/uuid"
)

var artifactTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func JobPrefix(id uuid.UUID) string {
	return path.Join("jobs", id.String())
}

func RecordingResultKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "recording", "recording-result.json")
}

func RecordingScriptKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "recording", "recording.js")
}

func SegmentClipKey(id uuid.UUID, segmentID string) (string, error) {
	if err := validateArtifactToken("segment id", segmentID); err != nil {
		return "", err
	}
	return path.Join(JobPrefix(id), "recording", "segments", segmentID+".mp4"), nil
}

func CompositionResultKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "composition", "composition-result.json")
}

func FinalMP4Key(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "composition", "final.mp4")
}

// MomentsKey returns the durable JSON key for the job's scored moment index.
func MomentsKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "moments", "moments.json")
}

// RenderVariantPrefix returns the durable storage prefix for a named render
// variant, such as a vertical Shorts pack or a future mobile render.
func RenderVariantPrefix(id uuid.UUID, variant string) (string, error) {
	if err := validateArtifactToken("render variant", variant); err != nil {
		return "", err
	}
	return path.Join(JobPrefix(id), "renders", variant), nil
}

// RenderVariantResultKey returns the JSON result key for a named render
// variant.
func RenderVariantResultKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "render-result.json"), nil
}

// RenderVariantStatusKey returns the durable status document key for a named
// render variant.
func RenderVariantStatusKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "status.json"), nil
}

// RenderVariantEditDocumentKey returns the stable user/edit intent document key
// for a named render variant.
func RenderVariantEditDocumentKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "edit-document.json"), nil
}

// RenderVariantEditManifestKey returns the compiled editor manifest key for a
// named render variant.
func RenderVariantEditManifestKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "edit-manifest.json"), nil
}

// RenderVariantPackManifestKey returns the publish-pack manifest key for a
// named render variant.
func RenderVariantPackManifestKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "pack-manifest.json"), nil
}

// RenderVariantPublishSummaryKey returns the markdown summary key for a named
// render variant.
func RenderVariantPublishSummaryKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "publish-summary.md"), nil
}

// RenderVariantUploadStatusKey returns the local manual-upload marker for a
// named render variant.
func RenderVariantUploadStatusKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "uploaded.json"), nil
}

// RenderVariantVideoKey returns the MP4 key for one video artifact inside a
// named render variant.
func RenderVariantVideoKey(id uuid.UUID, variant, name string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if err := validateArtifactToken("artifact name", name); err != nil {
		return "", err
	}
	return path.Join(prefix, "videos", name+".mp4"), nil
}

// RenderVariantCoverKey returns the JPG cover key for one artifact inside a
// named render variant.
func RenderVariantCoverKey(id uuid.UUID, variant, name string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if err := validateArtifactToken("artifact name", name); err != nil {
		return "", err
	}
	return path.Join(prefix, "covers", name+".jpg"), nil
}

// RenderVariantCaptionKey returns the caption text key for one artifact inside
// a named render variant.
func RenderVariantCaptionKey(id uuid.UUID, variant, name string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if err := validateArtifactToken("artifact name", name); err != nil {
		return "", err
	}
	return path.Join(prefix, "captions", name+".caption.txt"), nil
}

// RenderVariantGalleryKey returns the HTML gallery key for a named render
// variant.
func RenderVariantGalleryKey(id uuid.UUID, variant string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, "index.html"), nil
}

// RenderVariantLogKey returns a log artifact key for a named render variant.
func RenderVariantLogKey(id uuid.UUID, variant, name string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if err := validateArtifactToken("log name", name); err != nil {
		return "", err
	}
	return path.Join(prefix, "logs", name+".log"), nil
}

func RenderVariantAgentContextKey(id uuid.UUID, variant, name string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if err := validateArtifactToken("agent name", name); err != nil {
		return "", err
	}
	return path.Join(prefix, "agents", name, "context.json"), nil
}

func RenderVariantAgentResultKey(id uuid.UUID, variant, name string) (string, error) {
	prefix, err := RenderVariantPrefix(id, variant)
	if err != nil {
		return "", err
	}
	if err := validateArtifactToken("agent name", name); err != nil {
		return "", err
	}
	return path.Join(prefix, "agents", name, "result.json"), nil
}

func validateArtifactToken(label, value string) error {
	if !artifactTokenPattern.MatchString(value) {
		return fmt.Errorf("invalid %s %q", label, value)
	}
	return nil
}
