package streamclips

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestKillfeedAnalysisFingerprintBindsSourceCropAndClipBounds(t *testing.T) {
	crop := CropRect{X: 0.7, Y: 0.05, Width: 0.25, Height: 0.2}
	clips := []ClipRange{{ID: "clip-1", StartSeconds: 1, EndSeconds: 4}}
	base, err := KillfeedAnalysisFingerprint("SOURCE-A", crop, clips)
	if err != nil {
		t.Fatal(err)
	}
	again, err := KillfeedAnalysisFingerprint("source-a", crop, clips)
	if err != nil {
		t.Fatal(err)
	}
	if base != again {
		t.Fatalf("case-normalized source fingerprint changed: %q != %q", base, again)
	}

	changedSource, _ := KillfeedAnalysisFingerprint("source-b", crop, clips)
	changedCrop := crop
	changedCrop.X = 0.69
	cropFingerprint, _ := KillfeedAnalysisFingerprint("source-a", changedCrop, clips)
	changedClips := append([]ClipRange(nil), clips...)
	changedClips[0].EndSeconds = 4.5
	boundsFingerprint, _ := KillfeedAnalysisFingerprint("source-a", crop, changedClips)
	for label, got := range map[string]string{
		"source": changedSource,
		"crop":   cropFingerprint,
		"bounds": boundsFingerprint,
	} {
		if got == base {
			t.Fatalf("%s change did not invalidate fingerprint %q", label, base)
		}
	}
}

func TestKillfeedAnalysisStateValidatesPTSContractAndAllowsEmptyKills(t *testing.T) {
	jobID := uuid.New()
	generationID := uuid.New()
	crop := CropRect{X: 0.7, Y: 0.05, Width: 0.25, Height: 0.2}
	clips := []ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}}
	fingerprint, err := KillfeedAnalysisFingerprint("source-sha", crop, clips)
	if err != nil {
		t.Fatal(err)
	}
	state := KillfeedAnalysisState{
		JobID: jobID, GenerationID: generationID, Status: KillfeedAnalysisReady,
		SourceSHA256: "source-sha", KillfeedCrop: crop, Fingerprint: fingerprint,
		Clips: []KillfeedAnalysisClip{{
			ClipID: "clip-1", StartSeconds: 0, EndSeconds: 2,
			Events: []KillfeedAnalysisEvent{{
				EventID: "event-1", SourcePTS: 500, TimeBase: KillfeedTimeBase{Num: 1, Den: 1000},
				CueSeconds: 0.5, OnsetStartPTS: 499, OnsetEndPTS: 500,
				SamplePTS: 850, SampleSeconds: 0.85, Mode: KillfeedEventAlignedFrame,
				Rows: []KillfeedRowEvidence{{
					OnsetRowIndex: 0, SampleRowIndex: 0, Fingerprint: "row-1",
					OnsetBounds:  NoticeRow{X: 10, Y: 10, Width: 100, Height: 30},
					SampleBounds: NoticeRow{X: 10, Y: 10, Width: 100, Height: 30},
				}},
				Kills: []KillfeedKill{},
			}},
		}},
		UpdatedAt: time.Now().UTC(),
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	state.Clips[0].Events[0].OnsetEndPTS = 501
	if err := state.Validate(); err == nil || !strings.Contains(err.Error(), "source_pts must equal") {
		t.Fatalf("Validate() error = %v, want source/onset mismatch", err)
	}
}

func TestKillfeedAnalysisReadyStateMayContainZeroEvents(t *testing.T) {
	crop := CropRect{X: 0.7, Y: 0.05, Width: 0.25, Height: 0.2}
	clips := []ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}}
	fingerprint, err := KillfeedAnalysisFingerprint("source-sha", crop, clips)
	if err != nil {
		t.Fatal(err)
	}
	state := KillfeedAnalysisState{
		JobID: uuid.New(), GenerationID: uuid.New(), Status: KillfeedAnalysisReady,
		SourceSHA256: "source-sha", KillfeedCrop: crop, Fingerprint: fingerprint,
		Clips: []KillfeedAnalysisClip{{
			ClipID: "clip-1", StartSeconds: 0, EndSeconds: 2, Events: []KillfeedAnalysisEvent{},
		}},
		UpdatedAt: time.Now().UTC(),
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestKillfeedAnalysisArtifactKeys(t *testing.T) {
	jobID := uuid.New()
	generationID := uuid.New()
	if got, want := KillfeedAnalysisKey(jobID), JobPrefix(jobID)+"/killfeed/analysis.json"; got != want {
		t.Fatalf("active key = %q, want %q", got, want)
	}
	got, err := KillfeedAnalysisGenerationKey(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	want := JobPrefix(jobID) + "/killfeed/generations/" + generationID.String() + ".json"
	if got != want {
		t.Fatalf("generation key = %q, want %q", got, want)
	}
	if _, err := KillfeedAnalysisGenerationKey(jobID, uuid.Nil); err == nil {
		t.Fatal("nil generation id unexpectedly accepted")
	}
	rowKey, err := KillfeedEventRowKey(jobID, generationID, "clip-001", "kf_event-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	wantRowKey := JobPrefix(jobID) + "/killfeed/generations/" + generationID.String() + "/events/clip-001/kf_event-1/row-002.png"
	if rowKey != wantRowKey {
		t.Fatalf("row key = %q, want %q", rowKey, wantRowKey)
	}
	for _, bad := range []struct {
		name       string
		generation uuid.UUID
		clipID     string
		eventID    string
		rowIndex   int
	}{
		{name: "nil generation", clipID: "clip-001", eventID: "event-1"},
		{name: "unsafe clip", generation: generationID, clipID: "../clip", eventID: "event-1"},
		{name: "unsafe event", generation: generationID, clipID: "clip-001", eventID: "../event"},
		{name: "negative row", generation: generationID, clipID: "clip-001", eventID: "event-1", rowIndex: -1},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := KillfeedEventRowKey(jobID, bad.generation, bad.clipID, bad.eventID, bad.rowIndex); err == nil {
				t.Fatal("unsafe artifact key accepted")
			}
		})
	}
}
