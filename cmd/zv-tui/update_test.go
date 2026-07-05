package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// apply runs one message through Update and returns the resulting model.
func apply(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	nm, _ := m.Update(msg)
	out, ok := nm.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", nm)
	}
	return out
}

func testModel() model {
	m := newModel(nil, nil)
	m.ready = true
	m.width = 80
	m.height = 24
	return m
}

// A transient poll failure must be cleared by the next successful poll of the
// same data, so a network blip does not leave a stale error on screen.
func TestPollErrorClearedByNextSuccessfulPoll(t *testing.T) {
	m := testModel()
	m = apply(t, m, pollErrMsg{pollJobs, errors.New("GET /api/jobs: connection refused")})
	if m.pollErrText() == "" {
		t.Fatal("poll failure not shown")
	}
	m = apply(t, m, jobsMsg{jobs: []tuiclient.Job{{ID: "job-1"}}})
	if got := m.pollErrText(); got != "" {
		t.Fatalf("stale poll error survived a successful poll: %q", got)
	}
}

// While the failure persists (poll keeps failing), the error must keep showing.
func TestPersistentPollFailureKeepsShowing(t *testing.T) {
	m := testModel()
	for i := 0; i < 3; i++ {
		m = apply(t, m, pollErrMsg{pollJobs, errors.New("connection refused")})
		if m.pollErrText() == "" {
			t.Fatalf("iteration %d: persistent failure not shown", i)
		}
	}
}

// A background poll failure must not remove the busy indicator of a
// still-running operator-initiated action.
func TestPollErrorDoesNotClearBusy(t *testing.T) {
	m := testModel()
	m.busy = true // an upload is in flight
	m = apply(t, m, pollErrMsg{pollJobs, errors.New("connection refused")})
	if !m.busy {
		t.Fatal("background poll failure cleared the busy flag")
	}
	if !strings.Contains(m.viewFooter(), "working") {
		t.Fatal("footer hides the in-flight action behind a poll error")
	}
}

// Action errors keep the old contract: they end the action (busy off) and stay
// visible until acknowledged or superseded, even across successful polls.
func TestActionErrorClearsBusyAndSurvivesPolls(t *testing.T) {
	m := testModel()
	// Same job stays focused across the poll; moving the cursor to another job
	// is the documented way an error gets superseded.
	m.jobs = []tuiclient.Job{{ID: "job-1"}}
	m.detailID = "job-1"
	m.busy = true
	m = apply(t, m, errMsg{errors.New("upload failed: file too large")})
	if m.busy {
		t.Fatal("action error did not clear busy")
	}
	m = apply(t, m, jobsMsg{jobs: []tuiclient.Job{{ID: "job-1"}}})
	if m.errText == "" {
		t.Fatal("action error was cleared by an unrelated successful poll")
	}
	if !strings.Contains(m.viewFooter(), "file too large") {
		t.Fatal("footer does not show the action error")
	}
}

// Poll errors are shown only for the screen whose data is actually being
// polled; the other screen's sources are stale by definition.
func TestPollErrorScopedToItsScreen(t *testing.T) {
	m := testModel()
	m.screen = screenDemos
	m = apply(t, m, pollErrMsg{pollStreams, errors.New("streams down")})
	if got := m.pollErrText(); got != "" {
		t.Fatalf("streams poll error shown on demos screen: %q", got)
	}
	m.screen = screenStreams
	if m.pollErrText() == "" {
		t.Fatal("streams poll error not shown on streams screen")
	}
}

// Caps/presets load failures are global and must clear once the load succeeds.
func TestCapsAndPresetsErrorsClearOnSuccess(t *testing.T) {
	m := testModel()
	m = apply(t, m, pollErrMsg{pollCaps, errors.New("caps down")})
	m = apply(t, m, pollErrMsg{pollPresets, errors.New("presets down")})
	if m.pollErrText() == "" {
		t.Fatal("startup load failure not shown")
	}
	m = apply(t, m, capsMsg{})
	m = apply(t, m, presetsMsg{presets: []tuiclient.Preset{{Name: "viral-60-clean", Default: true}}})
	if got := m.pollErrText(); got != "" {
		t.Fatalf("caps/presets error survived successful load: %q", got)
	}
	if !m.capsLoaded || !m.presetsLoaded {
		t.Fatal("loaded flags not set")
	}
}

// When the focused job disappears (empty list), its detail source stops being
// polled, so its error must be dropped rather than lingering unclearable.
func TestDetailPollErrorDroppedWhenNoJobFocused(t *testing.T) {
	m := testModel()
	m.jobs = []tuiclient.Job{{ID: "job-1"}}
	m = apply(t, m, pollErrMsg{pollJobDetail, errors.New("GET /api/jobs/job-1: 500")})
	if m.pollErrText() == "" {
		t.Fatal("detail poll failure not shown")
	}
	m = apply(t, m, jobsMsg{jobs: nil})
	if got := m.pollErrText(); got != "" {
		t.Fatalf("detail poll error lingered with no job focused: %q", got)
	}
}

// A successful detail poll clears a previous detail poll failure.
func TestDetailPollErrorClearedByDetailSuccess(t *testing.T) {
	m := testModel()
	m.jobs = []tuiclient.Job{{ID: "job-1"}}
	m.detailID = "job-1"
	m = apply(t, m, pollErrMsg{pollJobDetail, errors.New("GET /api/jobs/job-1: 500")})
	m = apply(t, m, jobDetailMsg{id: "job-1", job: tuiclient.Job{ID: "job-1"}})
	if got := m.pollErrText(); got != "" {
		t.Fatalf("detail poll error survived a successful detail poll: %q", got)
	}
}

// An ongoing poll failure outranks a stale success notice in the footer, so an
// orchestrator outage is visible even right after a completed action.
func TestPollErrorOutranksStaleNotice(t *testing.T) {
	m := testModel()
	m.notice = "saved /tmp/fragforge-job1.mp4"
	m = apply(t, m, pollErrMsg{pollJobs, errors.New("connection refused")})
	if !strings.Contains(m.viewFooter(), "connection refused") {
		t.Fatal("footer shows the stale notice instead of the ongoing poll failure")
	}
}
