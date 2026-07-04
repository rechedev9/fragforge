package main

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// defaultPollInterval is how often the browse screen re-fetches the active list
// so async pipeline transitions (recording -> recorded, rendering -> ready)
// surface without a keypress. It is copied into model.pollInterval at
// construction so tests can dial it down instead of mutating a shared global.
const defaultPollInterval = 2 * time.Second

// fallbackRenderVariant is the default render variant name used until the preset
// registry loads; "viral-60-clean" is the documented default. Once presets load
// it is replaced by the registry default, stored per-model (m.defaultVariant) so
// no mutable global is shared with the Cmd goroutines.
const fallbackRenderVariant = "viral-60-clean"

type screen int

const (
	screenDemos screen = iota
	screenStreams
)

type mode int

const (
	modeBrowse     mode = iota // navigating a job list
	modePrompt                 // text-input overlay (upload path / URL)
	modeRoster                 // pick a player from the roster
	modeSegments               // multi-select segments to record
	modePreset                 // pick a render preset
	modeStreamEdit             // edit a stream job's clip plan
)

type promptKind int

const (
	promptUploadDemo promptKind = iota
	promptUploadStream
	promptStreamURL
	promptClipRange // "start end [title]" for one clip of a stream edit plan
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

// clipEditor is the in-progress edit of a stream job's clip list. It keeps the
// base plan (variant, crops) so saving preserves everything the TUI does not
// expose, only replacing the Clips. editIndex is -1 while adding a new clip.
type clipEditor struct {
	jobID     string
	basePlan  tuiclient.StreamEditPlan
	clips     []tuiclient.ClipRange
	cursor    int
	editIndex int
}

type model struct {
	cl           *tuiclient.Client
	pollInterval time.Duration
	width        int
	height       int
	ready        bool

	screen screen
	mode   mode

	caps       tuiclient.Capabilities
	capsLoaded bool
	presets    []tuiclient.Preset
	// defaultVariant is the render variant used when the operator does not pick a
	// preset; it tracks the preset registry default (set from presetsMsg on the
	// Update goroutine, read only there and when building Cmds - no shared global).
	defaultVariant string
	spinner        spinner.Model
	busy           bool
	notice         string
	errText        string

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

	// initialDrops are file paths given on the command line (e.g. a .dem
	// dragged onto the executable), uploaded once at startup.
	initialDrops []string

	// overlays
	prompt        textinput.Model
	promptKind    promptKind
	roster        table.Model
	rosterPlayers []tuiclient.PlayerStat
	segs          segmentPicker
	presetCursor  int
	presetPurpose presetPurpose
	clipEd        clipEditor
}

func newModel(cl *tuiclient.Client, initialDrops []string) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	ti := textinput.New()
	ti.CharLimit = 4096
	ti.Width = 60

	return model{
		cl:             cl,
		pollInterval:   defaultPollInterval,
		initialDrops:   initialDrops,
		screen:         screenDemos,
		mode:           modeBrowse,
		defaultVariant: fallbackRenderVariant,
		spinner:        sp,
		prompt:         ti,
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.loadCaps(),
		m.loadPresets(),
		m.loadJobs(),
		m.loadStreams(),
		m.tick(),
	}
	drops, _ := m.dropCmds(m.initialDrops)
	cmds = append(cmds, drops...)
	return tea.Batch(cmds...)
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
