package artifacts

import (
	"testing"

	"github.com/google/uuid"
)

const testRenderVariant = "viral-60-clean"

func TestKeysUseStableJobLayout(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	segmentKey, err := SegmentClipKey(id, "s1")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		JobPrefix(id):            "jobs/11111111-1111-1111-1111-111111111111",
		RecordingResultKey(id):   "jobs/11111111-1111-1111-1111-111111111111/recording/recording-result.json",
		RecordingScriptKey(id):   "jobs/11111111-1111-1111-1111-111111111111/recording/recording.js",
		segmentKey:               "jobs/11111111-1111-1111-1111-111111111111/recording/segments/s1.mp4",
		CompositionResultKey(id): "jobs/11111111-1111-1111-1111-111111111111/composition/composition-result.json",
		FinalMP4Key(id):          "jobs/11111111-1111-1111-1111-111111111111/composition/final.mp4",
		MomentsKey(id):           "jobs/11111111-1111-1111-1111-111111111111/moments/moments.json",
		RosterKey(id):            "jobs/11111111-1111-1111-1111-111111111111/roster.json",
		GenerateIntentKey(id):    "jobs/11111111-1111-1111-1111-111111111111/generate-intent.json",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

func TestRenderVariantKeysUseStableLayout(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	prefix, err := RenderVariantPrefix(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, err := RenderVariantResultKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	statusKey, err := RenderVariantStatusKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	editDocumentKey, err := RenderVariantEditDocumentKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	editManifestKey, err := RenderVariantEditManifestKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	packKey, err := RenderVariantPackManifestKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	summaryKey, err := RenderVariantPublishSummaryKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	uploadedKey, err := RenderVariantUploadStatusKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	videoKey, err := RenderVariantVideoKey(id, testRenderVariant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	coverKey, err := RenderVariantCoverKey(id, testRenderVariant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	captionKey, err := RenderVariantCaptionKey(id, testRenderVariant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	galleryKey, err := RenderVariantGalleryKey(id, testRenderVariant)
	if err != nil {
		t.Fatal(err)
	}
	logKey, err := RenderVariantLogKey(id, testRenderVariant, "seg-001-render")
	if err != nil {
		t.Fatal(err)
	}
	agentContextKey, err := RenderVariantAgentContextKey(id, testRenderVariant, "caption-candidates")
	if err != nil {
		t.Fatal(err)
	}
	agentResultKey, err := RenderVariantAgentResultKey(id, testRenderVariant, "caption-candidates")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		prefix:          "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean",
		resultKey:       "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/render-result.json",
		statusKey:       "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/status.json",
		editDocumentKey: "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/edit-document.json",
		editManifestKey: "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/edit-manifest.json",
		packKey:         "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/pack-manifest.json",
		summaryKey:      "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/publish-summary.md",
		uploadedKey:     "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/uploaded.json",
		videoKey:        "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/videos/seg-001.mp4",
		coverKey:        "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/covers/seg-001.jpg",
		captionKey:      "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/captions/seg-001.caption.txt",
		galleryKey:      "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/index.html",
		logKey:          "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/logs/seg-001-render.log",
		agentContextKey: "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/agents/caption-candidates/context.json",
		agentResultKey:  "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/agents/caption-candidates/result.json",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

func TestSegmentClipKeyRejectsPathLikeIDs(t *testing.T) {
	id := uuid.New()
	for _, segmentID := range []string{"", "../x", "x/y", `x\y`, "-bad"} {
		if _, err := SegmentClipKey(id, segmentID); err == nil {
			t.Fatalf("SegmentClipKey(%q) error = nil, want error", segmentID)
		}
	}
}

func TestRenderVariantKeysRejectPathLikeTokens(t *testing.T) {
	id := uuid.New()
	badTokens := []string{"", "../x", "x/y", `x\y`, "-bad", "x.mp4"}
	for _, token := range badTokens {
		if _, err := RenderVariantPrefix(id, token); err == nil {
			t.Fatalf("RenderVariantPrefix(%q) error = nil, want error", token)
		}
		if _, err := RenderVariantVideoKey(id, testRenderVariant, token); err == nil {
			t.Fatalf("RenderVariantVideoKey(name=%q) error = nil, want error", token)
		}
		if _, err := RenderVariantLogKey(id, testRenderVariant, token); err == nil {
			t.Fatalf("RenderVariantLogKey(name=%q) error = nil, want error", token)
		}
	}
}
