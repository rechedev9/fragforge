package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestRunFlowsShowDemoJSONIsCompleteAgentJourney(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "show", "demo", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result struct {
		OK   bool           `json:"ok"`
		Flow productionFlow `json:"flow"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Flow.Name != "demo" || len(result.Flow.Phases) < 8 {
		t.Fatalf("result = %#v", result)
	}
	body := stdout.String()
	for _, want := range []string{
		"zv demo players",
		"zv demo moments",
		"zv demo select",
		"zv record",
		"parse-preflight",
		"moments-preflight",
		"creative-brief",
		"kill numbering or counter",
		"thumbnail-selection",
		editor.OutputFormatShort9x16,
		editor.OutputFormatLandscape16x9,
		"shortslistosparasubir",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("flow JSON missing %q: %s", want, body)
		}
	}
}

func TestDemoFlowKeepsPreflightPersistRhythm(t *testing.T) {
	flow, ok := findProductionFlow("demo")
	if !ok {
		t.Fatal("demo flow missing")
	}
	phases := make(map[string]flowPhase, len(flow.Phases))
	for _, phase := range flow.Phases {
		phases[phase.ID] = phase
	}
	// Each cheap deterministic stage exposes a read-only --dry-run preflight
	// before the persisting command, mirroring select/capture/edit.
	for _, pair := range []struct{ preflight, persist string }{
		{"parse-preflight", "parse"},
		{"moments-preflight", "moments"},
	} {
		pre, ok := phases[pair.preflight]
		if !ok {
			t.Fatalf("missing preflight phase %q", pair.preflight)
		}
		if !pre.ReadOnly || !strings.Contains(pre.Command, "--dry-run") {
			t.Fatalf("preflight %q = %#v, want read-only --dry-run", pair.preflight, pre)
		}
		persist, ok := phases[pair.persist]
		if !ok {
			t.Fatalf("missing persist phase %q", pair.persist)
		}
		if persist.ReadOnly || strings.Contains(persist.Command, "--dry-run") {
			t.Fatalf("persist %q = %#v, want a mutating command", pair.persist, persist)
		}
	}
}

func TestDemoFlowRequiresCreativeAndThumbnailGates(t *testing.T) {
	flow, ok := findProductionFlow("demo")
	if !ok {
		t.Fatal("demo flow missing")
	}
	gates := make(map[string]flowPhase)
	for _, phase := range flow.Phases {
		if phase.Gate {
			gates[phase.ID] = phase
		}
	}
	for _, id := range []string{"creative-brief", "thumbnail-selection"} {
		phase, ok := gates[id]
		if !ok || phase.Decision == "" {
			t.Fatalf("gate %q = %#v, want required decision", id, phase)
		}
	}
	phases := make(map[string]flowPhase, len(flow.Phases))
	for _, phase := range flow.Phases {
		phases[phase.ID] = phase
	}
	for _, want := range []string{"--hud <gameplay|clean|deathnotices>", "--kill-effect <approved-effect>", "--kill-counter=<true|false>", "--covers=<true|false>"} {
		commands := phases["capture-preflight"].Command + phases["edit-preflight"].Command + phases["edit"].Command
		if !strings.Contains(commands, want) {
			t.Fatalf("approved choice %q missing from downstream commands: %s", want, commands)
		}
	}
	if !strings.Contains(phases["edit"].Produces, "upload-ready pack when covers are disabled") {
		t.Fatalf("edit produces %q, want no-cover completion branch", phases["edit"].Produces)
	}
	if !strings.Contains(phases["thumbnail-selection"].Produces, "upload-ready") {
		t.Fatalf("thumbnail gate produces %q, want upload-ready pack", phases["thumbnail-selection"].Produces)
	}
	if phases["thumbnail-selection"].When != "covers enabled" {
		t.Fatalf("thumbnail gate condition = %q, want covers enabled", phases["thumbnail-selection"].When)
	}
}

func TestRunFlowsShowStreamIncludesLandscapeAndCaptionDecision(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "show", "stream", "--format=json"}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result struct {
		Flow productionFlow `json:"flow"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	commands := make(map[string]string, len(result.Flow.Phases))
	for _, phase := range result.Flow.Phases {
		commands[phase.ID] = phase.Command
	}
	for _, id := range []string{"plan", "enrich", "captions", "render"} {
		if commands[id] == "" || strings.Contains(commands[id], "--dry-run") {
			t.Fatalf("persistent phase %q = %q, want a real artifact-producing command", id, commands[id])
		}
	}
	for _, id := range []string{"plan-preflight", "killfeed-preflight", "captions-preflight", "render-preflight"} {
		if !strings.Contains(commands[id], "--dry-run") {
			t.Fatalf("preflight phase %q = %q, want --dry-run", id, commands[id])
		}
	}
	for _, output := range result.Flow.Outputs {
		if output.Destination != "<run>/render/shortslistosparasubir" {
			t.Fatalf("stream output destination = %q, want render publish directory", output.Destination)
		}
	}
	body := stdout.String()
	for _, want := range []string{
		streamclips.VariantStreamerLandscape16x9,
		"--detect-killfeed",
		"--captions",
		"factual notices",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("flow JSON missing %q: %s", want, body)
		}
	}
}

func TestDemoArtifactWritingPreflightsAreNotReadOnly(t *testing.T) {
	flow, ok := findProductionFlow("demo")
	if !ok {
		t.Fatal("demo flow missing")
	}
	for _, phase := range flow.Phases {
		if phase.ID == "capture-preflight" || phase.ID == "edit-preflight" {
			if phase.ReadOnly || phase.Produces == "" {
				t.Fatalf("phase %s = %#v, want mutating dry-run metadata", phase.ID, phase)
			}
		}
	}
}

func TestRunFlowsRejectsUnknownFlowAsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "show", "other", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitInvalidArgs || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK {
		t.Fatalf("result = %#v", result)
	}
}
