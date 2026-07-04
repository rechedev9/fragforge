package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// These tests drive the real TUI model (Init/Update/View) through teatest against
// an in-memory orchestrator double (orchFake) that has every capture stage
// enabled, so the whole flow runs here even though this host cannot record CS2.

const e2eTermW, e2eTermH = 120, 40

// tui wraps a running teatest program with a PERSISTENT view of the output. The
// bubbletea renderer only emits diffs, so once the screen is steady it stops
// writing; each drain of teatest's buffer must therefore be accumulated, or a
// later wait would see an empty buffer even though the text is on screen.
type tui struct {
	t   *testing.T
	tm  *teatest.TestModel
	out io.Reader
	acc []byte
}

func startTUI(t *testing.T, fake *orchFake) *tui {
	t.Helper()
	srv := fake.server()
	t.Cleanup(srv.Close)
	cl := tuiclient.New(tuiclient.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	m := newModel(cl, nil)
	// A short poll keeps state fresh and lets held async stages surface without a
	// multi-second wait; the harness handles frame accumulation.
	m.pollInterval = 20 * time.Millisecond
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(e2eTermW, e2eTermH))
	return &tui{t: t, tm: tm, out: tm.Output()}
}

// wait blocks until the accumulated, ANSI-stripped screen contains want.
func (u *tui) wait(want string) {
	u.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		chunk, _ := io.ReadAll(u.out)
		u.acc = append(u.acc, chunk...)
		if strings.Contains(ansi.Strip(string(u.acc)), want) {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	u.t.Fatalf("wait: %q not found within 8s.\n--- accumulated screen ---\n%s",
		want, ansi.Strip(string(u.acc)))
}

func (u *tui) send(m tea.Msg) { u.tm.Send(m) }
func (u *tui) key(s string)   { u.tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }
func (u *tui) typ(s string)   { u.tm.Type(s) }

// drain discards all output produced so far, so a following wait matches only
// NEW frames. Needed when the target word (e.g. "ready") already appears in a
// steady part of the screen (like the footer step label) and we want to assert a
// specific later transition instead of a stale match.
func (u *tui) drain() {
	_, _ = io.ReadAll(u.out)
	u.acc = nil
}

var (
	enter  = tea.KeyMsg{Type: tea.KeyEnter}
	ctrlC  = tea.KeyMsg{Type: tea.KeyCtrlC}
	ctrlU  = tea.KeyMsg{Type: tea.KeyCtrlU}
	tabKey = tea.KeyMsg{Type: tea.KeyTab}
)

// quit stops the program (works from any mode) and returns the final model.
func (u *tui) quit() model {
	u.t.Helper()
	u.tm.Send(ctrlC)
	u.tm.WaitFinished(u.t, teatest.WithFinalTimeout(5*time.Second))
	fm, ok := u.tm.FinalModel(u.t).(model)
	if !ok {
		u.t.Fatalf("final model has unexpected type %T", u.tm.FinalModel(u.t))
	}
	return fm
}

func writeTemp(t *testing.T, name, data string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestReelFlowEndToEnd walks the full demo->reel pipeline the way an operator
// would: upload a demo, scan, pick a player, parse, record, compose, render a
// variant, and download the composed MP4 - asserting the UI at every waypoint
// and the final model state.
func TestReelFlowEndToEnd(t *testing.T) {
	t.Chdir(t.TempDir()) // saveFinal writes fragforge-<id>.mp4 into the working dir

	fake := newOrchFake()
	dem := writeTemp(t, "match.dem", "FAKEDEM")
	u := startTUI(t, fake)

	u.wait("FragForge")
	u.wait("no demos yet")

	// Upload a .dem -> roster scan.
	u.key("u")
	u.wait("Upload a demo")
	u.send(ctrlU) // clear the prefilled default upload dir
	u.typ(dem)
	u.send(enter)

	// Scanned: the roster loads into the detail panel.
	u.wait("scanned")
	u.wait("de_mirage")
	u.wait("s1mple")

	// enter -> pick-a-player overlay showing the roster.
	u.send(enter)
	u.wait("Pick a player to feature")
	u.wait("ZywOo")

	// enter -> parse the focused (top) player.
	u.send(enter)
	u.wait("parsed")
	u.wait("Kill plan")
	u.wait("seg-1")

	// r -> segment multi-select (all preselected) -> preset -> record.
	u.key("r")
	u.wait("Select segments to record")
	u.wait("2 of 2 selected")
	u.send(enter)
	u.wait("Pick a render preset")
	u.send(enter)
	u.wait("recorded")

	// c -> compose the recorded segments.
	u.key("c")
	u.wait("composed")

	// R -> preset -> render the default variant into a publish-ready Short.
	u.key("R")
	u.wait("Pick a render preset")
	u.send(enter)
	u.wait("viral-60-clean")

	// d -> download the composed MP4 to the working dir. The notice is
	// "saved <abs path>", so match the unique "saved " prefix (no other notice
	// uses it) and confirm the actual file below.
	u.key("d")
	u.wait("saved ")

	fm := u.quit()
	if len(fm.jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(fm.jobs))
	}
	if got := fm.jobs[0].Status; got != tuiclient.StatusComposed && got != tuiclient.StatusDone {
		t.Fatalf("want composed/done job, got %q", got)
	}
	if fm.detail.render == nil || fm.detail.render.Status != tuiclient.RenderReady {
		t.Fatalf("want a ready render in detail, got %+v", fm.detail.render)
	}

	// The composed MP4 landed on disk with the fake payload.
	matches, _ := filepath.Glob("fragforge-*.mp4")
	if len(matches) == 0 {
		t.Fatal("no downloaded fragforge-*.mp4 in the working dir")
	}
	if got, _ := os.ReadFile(matches[0]); string(got) != "FAKE-MP4-BYTES" {
		t.Fatalf("downloaded file has unexpected contents %q", got)
	}
}

// TestStreamClipFlowEndToEnd walks the stream-clip pipeline: switch tabs, upload
// an .mp4, and render the default vertical-stack variant.
func TestStreamClipFlowEndToEnd(t *testing.T) {
	fake := newOrchFake()
	mp4 := writeTemp(t, "clip.mp4", "FAKEMP4")
	u := startTUI(t, fake)

	u.wait("FragForge")

	// Switch to the Stream Clips tab.
	u.send(tabKey)
	u.wait("no stream clips yet")

	// Upload an .mp4 -> ready (probed).
	u.key("u")
	u.wait("Upload a streamer clip")
	u.send(ctrlU)
	u.typ(mp4)
	u.send(enter)
	u.wait("ready")
	u.wait("Edit plan")
	u.wait(tuiclient.StreamDefaultVariant)

	// R -> render the default variant.
	u.key("R")
	u.wait("rendered")

	fm := u.quit()
	if len(fm.streams) != 1 || fm.streams[0].Status != tuiclient.StreamRendered {
		t.Fatalf("want 1 rendered stream, got %+v", fm.streams)
	}
}

// TestStreamClipFromURL covers acquiring a clip from a URL (the yt-dlp path).
func TestStreamClipFromURL(t *testing.T) {
	fake := newOrchFake()
	u := startTUI(t, fake)
	u.wait("FragForge")

	u.send(tabKey)
	u.wait("no stream clips yet")

	u.key("U")
	u.wait("Acquire a stream clip by URL")
	u.typ("https://twitch.tv/videos/123")
	u.send(enter)
	u.wait("acquiring")

	fm := u.quit()
	if len(fm.streams) != 1 || fm.streams[0].Status != tuiclient.StreamAcquiring {
		t.Fatalf("want 1 acquiring stream, got %+v", fm.streams)
	}
}

// TestRecordGatedWhenCaptureDisabled documents the real Linux-orchestrator
// behavior: with capture off, the media actions surface a clear, non-fatal
// explanation instead of enqueuing anything.
func TestRecordGatedWhenCaptureDisabled(t *testing.T) {
	fake := newOrchFake()
	fake.caps.Record.Enabled = false
	fake.caps.Compose.Enabled = false
	fake.caps.Render.Enabled = false
	// A recorded job so compose/render pass their status guard and reach the
	// capability check (compose/render first reject non-recorded jobs by status).
	fake.seedJob(tuiclient.StatusRecorded)

	u := startTUI(t, fake)
	u.wait("recorded")
	u.wait("capture:") // header shows the (faint) capability summary

	u.key("r")
	u.wait("recording is not configured")

	u.key("c")
	u.wait("composition is not configured")

	u.key("R")
	u.wait("rendering is not configured")

	u.quit()
}

// TestFailedJobOffersRetry checks a failed job renders its failure and the
// reconcile step steers the operator to retry.
func TestFailedJobOffersRetry(t *testing.T) {
	fake := newOrchFake()
	fake.seedJob(tuiclient.StatusFailed)

	u := startTUI(t, fake)
	u.wait("failed")
	u.wait("parser crashed")
	u.wait("retry")

	u.quit()
}

// TestRenderInFlightThenReady exercises the poll-driven transition: with the
// fake holding renders in-flight, the UI shows "rendering", and once released it
// advances to a ready render on the next poll - no keypress in between.
func TestRenderInFlightThenReady(t *testing.T) {
	fake := newOrchFake()
	fake.holdInFlight(true)
	id := fake.seedJob(tuiclient.StatusComposed)

	u := startTUI(t, fake)
	u.wait("composed")

	// R -> preset -> render; the fake parks it in "rendering".
	u.key("R")
	u.wait("Pick a render preset")
	u.send(enter)
	u.wait("rendering")

	// Discard history (the composed job's footer already says "ready to render"),
	// then release the held stage; the background poll should repaint the render
	// section from "rendering" to "ready".
	u.drain()
	fake.release(id)
	u.wait("ready")

	fm := u.quit()
	if fm.detail.render == nil || fm.detail.render.Status != tuiclient.RenderReady {
		t.Fatalf("want ready render after release, got %+v", fm.detail.render)
	}
}
