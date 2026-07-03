package main

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

const pollInterval = 2 * time.Second

// defaultRenderVariant is the preset name used as the default render variant
// (POST /api/jobs/{id}/renders/{variant}). It is overwritten once the preset
// registry loads; "viral-60-clean" is the documented default.
var defaultRenderVariant = "viral-60-clean"

type screen int

const (
	screenDemos screen = iota
	screenStreams
)

type mode int

const (
	modeBrowse   mode = iota // navigating a job list
	modePrompt               // text-input overlay (upload path / URL)
	modeRoster               // pick a player from the roster
	modeSegments             // multi-select segments to record
	modePreset               // pick a render preset
	modeConfirm              // yes/no confirmation
)

type promptKind int

const (
	promptUploadDemo promptKind = iota
	promptUploadStream
	promptStreamURL
)

type presetPurpose int

const (
	presetForRecord presetPurpose = iota
	presetForRender
)

// jobDetail is the loaded view of the focused demo job.
type jobDetail struct {
	job    tuiclient.Job
	plan   *tuiclient.Plan
	roster *tuiclient.RosterResult
	render *tuiclient.RenderVariantState
	loaded bool
}

// streamDetail is the loaded view of the focused stream job.
type streamDetail struct {
	job    tuiclient.StreamJob
	plan   *tuiclient.StreamEditPlan
	render *tuiclient.StreamRenderState
	loaded bool
}

// segmentPicker is a multi-select over a plan's segments.
type segmentPicker struct {
	segments []tuiclient.Segment
	cursor   int
	selected map[int]bool
}

type model struct {
	cl     *tuiclient.Client
	width  int
	height int
	ready  bool

	screen screen
	mode   mode

	caps       tuiclient.Capabilities
	capsLoaded bool
	presets    []tuiclient.Preset
	spinner    spinner.Model
	busy       bool
	notice     string
	errText    string

	// demos
	jobs      []tuiclient.Job
	jobCursor int
	detail    jobDetail
	detailID  string

	// streams
	streams      []tuiclient.StreamJob
	streamCursor int
	streamDetail streamDetail
	streamID     string

	// overlays
	prompt        textinput.Model
	promptKind    promptKind
	roster        table.Model
	rosterPlayers []tuiclient.PlayerStat
	segs          segmentPicker
	presetCursor  int
	presetPurpose presetPurpose
	confirmText   string
	confirmCmd    tea.Cmd
}

func newModel(cl *tuiclient.Client) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	ti := textinput.New()
	ti.CharLimit = 4096
	ti.Width = 60

	return model{
		cl:      cl,
		screen:  screenDemos,
		mode:    modeBrowse,
		spinner: sp,
		prompt:  ti,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.loadCaps(),
		m.loadPresets(),
		m.loadJobs(),
		m.loadStreams(),
		tick(),
	)
}

// focusedJob returns the job under the cursor on the demos screen, or nil.
func (m model) focusedJob() *tuiclient.Job {
	if m.jobCursor < 0 || m.jobCursor >= len(m.jobs) {
		return nil
	}
	return &m.jobs[m.jobCursor]
}

// focusedStream returns the stream job under the cursor, or nil.
func (m model) focusedStream() *tuiclient.StreamJob {
	if m.streamCursor < 0 || m.streamCursor >= len(m.streams) {
		return nil
	}
	return &m.streams[m.streamCursor]
}

// recordEnabled/renderEnabled/composeEnabled gate the media actions on the
// orchestrator's reported capabilities (record/render need HLAE/CS2/ffmpeg,
// which only exist on a Windows+GPU capture host).
func (m model) recordEnabled() bool  { return m.caps.Record.Enabled }
func (m model) renderEnabled() bool  { return m.caps.Render.Enabled }
func (m model) composeEnabled() bool { return m.caps.Compose.Enabled }
