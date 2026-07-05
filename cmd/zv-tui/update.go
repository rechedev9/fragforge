package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if m.mode == modeBrowse {
			cmds = append(cmds, m.refreshCurrent())
		}
		return m, tea.Batch(cmds...)

	case errMsg:
		m.busy = false
		m.errText = msg.err.Error()
		return m, nil

	case noticeMsg:
		m.notice = string(msg)
		return m, nil

	case capsMsg:
		m.caps = msg.caps
		m.capsLoaded = true
		return m, nil

	case presetsMsg:
		m.presets = msg.presets
		for _, p := range msg.presets {
			if p.Default {
				m.defaultVariant = p.Name
			}
		}
		return m, nil

	case jobsMsg:
		m.jobs = msg.jobs
		m.clampJobCursor()
		return m, m.detailCmd()

	case streamsMsg:
		m.streams = msg.jobs
		m.clampStreamCursor()
		return m, m.streamDetailCmd()

	case jobDetailMsg:
		if msg.id == m.currentJobID() {
			m.detail = jobDetail{job: msg.job, plan: msg.plan, roster: msg.roster, render: msg.render, loaded: true}
			m.detailID = msg.id
		}
		return m, nil

	case streamDetailMsg:
		if msg.id == m.currentStreamID() {
			m.streamDetail = streamDetail{job: msg.job, plan: msg.plan, render: msg.render, loaded: true}
			m.streamID = msg.id
		}
		return m, nil

	case actionMsg:
		m.busy = false
		if msg.note != "" {
			m.notice = msg.note
			m.errText = ""
		}
		if msg.refresh {
			return m, m.refreshCurrent()
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global quit works in any mode.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	// Files dropped onto the terminal arrive as a bracketed paste of their
	// paths; outside the text prompt, treat that as an upload.
	if msg.Paste && m.mode != modePrompt {
		return m.handleDrop(string(msg.Runes))
	}
	switch m.mode {
	case modePrompt:
		return m.handlePromptKey(msg)
	case modeRoster:
		return m.handleRosterKey(msg)
	case modeSegments:
		return m.handleSegmentsKey(msg)
	case modePreset:
		return m.handlePresetKey(msg)
	default:
		return m.handleBrowseKey(msg)
	}
}

// ---- browse mode -----------------------------------------------------------

func (m model) handleBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit
	case "tab", "shift+tab":
		if m.screen == screenDemos {
			m.screen = screenStreams
		} else {
			m.screen = screenDemos
		}
		m.errText = ""
		return m, m.refreshCurrent()
	case "1":
		m.screen = screenDemos
		return m, m.refreshCurrent()
	case "2":
		m.screen = screenStreams
		return m, m.refreshCurrent()
	}
	if m.screen == screenDemos {
		return m.handleDemoKey(msg)
	}
	return m.handleStreamKey(msg)
}

func (m model) handleDemoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.jobCursor > 0 {
			m.jobCursor--
			return m, m.detailCmd()
		}
	case "down", "j":
		if m.jobCursor < len(m.jobs)-1 {
			m.jobCursor++
			return m, m.detailCmd()
		}
	case "g":
		m.jobCursor = 0
		return m, m.detailCmd()
	case "G":
		m.jobCursor = max(0, len(m.jobs)-1)
		return m, m.detailCmd()
	case "u":
		return m.openPrompt(promptUploadDemo, "path to a .dem file", defaultUploadDir())
	case "enter", "p":
		return m.actOnDemo()
	case "r":
		return m.startRecordFlow()
	case "c":
		return m.startCompose()
	case "R":
		return m.startRenderFlow()
	case "d":
		return m.downloadFinal()
	}
	return m, nil
}

// actOnDemo runs the primary contextual action for the focused job (enter/p):
// pick a target when scanned, else fall through to the natural next step.
func (m model) actOnDemo() (tea.Model, tea.Cmd) {
	job := m.focusedJob()
	if job == nil {
		return m, nil
	}
	switch tuiclient.NextStep(job.Status, m.detail.render) {
	case tuiclient.StepPickTarget:
		return m.openRoster()
	case tuiclient.StepRecord, tuiclient.StepRetry:
		return m.startRecordFlow()
	case tuiclient.StepRender:
		return m.startRenderFlow()
	default:
		return m, nil
	}
}

func (m model) startRecordFlow() (tea.Model, tea.Cmd) {
	job := m.focusedJob()
	if job == nil {
		return m, nil
	}
	if !m.recordEnabled() {
		m.errText = "recording is not configured on this orchestrator (needs HLAE/CS2 on a Windows+GPU host)"
		return m, nil
	}
	if m.detail.plan == nil || len(m.detail.plan.Segments) == 0 {
		m.errText = "no kill plan segments to record yet"
		return m, nil
	}
	return m.openSegments()
}

func (m model) startCompose() (tea.Model, tea.Cmd) {
	job := m.focusedJob()
	if job == nil {
		return m, nil
	}
	if job.Status != tuiclient.StatusRecorded && job.Status != tuiclient.StatusComposed {
		m.errText = "compose is only available for recorded jobs"
		return m, nil
	}
	if !m.composeEnabled() {
		m.errText = "composition is not configured on this orchestrator"
		return m, nil
	}
	id := job.ID
	cl := m.cl
	m.busy = true
	return m, runAction("compose enqueued", func(c context.Context) error {
		_, err := cl.StartCompose(c, id)
		return err
	})
}

func (m model) startRenderFlow() (tea.Model, tea.Cmd) {
	job := m.focusedJob()
	if job == nil {
		return m, nil
	}
	switch job.Status {
	case tuiclient.StatusRecorded, tuiclient.StatusComposed, tuiclient.StatusDone:
	default:
		m.errText = "render is available once a job is recorded"
		return m, nil
	}
	if !m.renderEnabled() {
		m.errText = "rendering is not configured on this orchestrator (needs ffmpeg on the capture host)"
		return m, nil
	}
	return m.openPreset(presetForRender)
}

func (m model) downloadFinal() (tea.Model, tea.Cmd) {
	job := m.focusedJob()
	if job == nil {
		return m, nil
	}
	if job.Status != tuiclient.StatusComposed && job.Status != tuiclient.StatusDone {
		m.errText = "no composed video to download yet"
		return m, nil
	}
	id := job.ID
	cl := m.cl
	m.busy = true
	return m, func() tea.Msg {
		path, err := saveFinal(cl, id)
		if err != nil {
			return errMsg{err}
		}
		return actionMsg{note: "saved " + path}
	}
}

// ---- stream mode -----------------------------------------------------------

func (m model) handleStreamKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.streamCursor > 0 {
			m.streamCursor--
			return m, m.streamDetailCmd()
		}
	case "down", "j":
		if m.streamCursor < len(m.streams)-1 {
			m.streamCursor++
			return m, m.streamDetailCmd()
		}
	case "g":
		m.streamCursor = 0
		return m, m.streamDetailCmd()
	case "G":
		m.streamCursor = max(0, len(m.streams)-1)
		return m, m.streamDetailCmd()
	case "u":
		return m.openPrompt(promptUploadStream, "path to a streamer .mp4 file", defaultUploadDir())
	case "U":
		return m.openPrompt(promptStreamURL, "source URL (needs yt-dlp on the host)", "")
	case "R", "enter":
		return m.startStreamRender()
	}
	return m, nil
}

func (m model) startStreamRender() (tea.Model, tea.Cmd) {
	job := m.focusedStream()
	if job == nil {
		return m, nil
	}
	if job.Status != tuiclient.StreamReady && job.Status != tuiclient.StreamRendered {
		m.errText = "stream job is not ready to render"
		return m, nil
	}
	if !m.caps.Render.Enabled {
		m.errText = "rendering is not configured on this orchestrator"
		return m, nil
	}
	id := job.ID
	cl := m.cl
	m.busy = true
	return m, runAction("stream render enqueued", func(c context.Context) error {
		_, err := cl.StartStreamRender(c, id, "")
		return err
	})
}

// ---- helpers ---------------------------------------------------------------

func (m *model) clampJobCursor() {
	if m.jobCursor >= len(m.jobs) {
		m.jobCursor = max(0, len(m.jobs)-1)
	}
	if m.jobCursor < 0 {
		m.jobCursor = 0
	}
}

func (m *model) clampStreamCursor() {
	if m.streamCursor >= len(m.streams) {
		m.streamCursor = max(0, len(m.streams)-1)
	}
	if m.streamCursor < 0 {
		m.streamCursor = 0
	}
}

func (m model) currentJobID() string {
	if j := m.focusedJob(); j != nil {
		return j.ID
	}
	return ""
}

func (m model) currentStreamID() string {
	if s := m.focusedStream(); s != nil {
		return s.ID
	}
	return ""
}

// detailCmd loads the focused demo job's detail. When the cursor moves to a
// different job it first drops the previous job's detail so a stale plan/roster/
// render can't drive an action against the newly-focused job, and clears any
// per-job error/notice that belonged to the old selection.
func (m *model) detailCmd() tea.Cmd {
	id := m.currentJobID()
	if id == "" {
		m.detail = jobDetail{}
		m.detailID = ""
		return nil
	}
	if id != m.detailID {
		m.detail = jobDetail{}
		m.detailID = id
		m.errText = ""
		m.notice = ""
	}
	return m.loadJobDetail(id)
}

func (m *model) streamDetailCmd() tea.Cmd {
	id := m.currentStreamID()
	if id == "" {
		m.streamDetail = streamDetail{}
		m.streamID = ""
		return nil
	}
	if id != m.streamID {
		m.streamDetail = streamDetail{}
		m.streamID = id
		m.errText = ""
		m.notice = ""
	}
	return m.loadStreamDetail(id)
}

// refreshCurrent reloads the active screen's list; the resulting jobsMsg/
// streamsMsg then reloads the focused detail via detailCmd (so detail is not
// fetched twice per poll).
func (m model) refreshCurrent() tea.Cmd {
	if m.screen == screenDemos {
		return m.loadJobs()
	}
	return m.loadStreams()
}

// ---- overlays: prompt ------------------------------------------------------

func (m model) openPrompt(kind promptKind, placeholder, prefill string) (model, tea.Cmd) {
	m.mode = modePrompt
	m.promptKind = kind
	m.errText = ""
	m.prompt.Placeholder = placeholder
	m.prompt.SetValue(prefill)
	m.prompt.CursorEnd()
	cmd := m.prompt.Focus()
	return m, cmd
}

func (m model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.prompt.Blur()
		return m, nil
	case "enter":
		value := m.prompt.Value()
		m.mode = modeBrowse
		m.prompt.Blur()
		if value == "" {
			return m, nil
		}
		return m.submitPrompt(value)
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m model) submitPrompt(value string) (tea.Model, tea.Cmd) {
	cl := m.cl
	m.busy = true
	switch m.promptKind {
	case promptUploadDemo:
		return m, runTransfer("demo uploaded - scanning", func(c context.Context) error {
			_, err := cl.CreateJob(c, value, "")
			return err
		})
	case promptUploadStream:
		return m, runTransfer("stream uploaded", func(c context.Context) error {
			_, err := cl.CreateStreamJobUpload(c, value, "")
			return err
		})
	case promptStreamURL:
		return m, runAction("stream source queued", func(c context.Context) error {
			_, err := cl.CreateStreamJobFromURL(c, value, "")
			return err
		})
	}
	return m, nil
}

// ---- overlays: roster ------------------------------------------------------

func (m model) openRoster() (tea.Model, tea.Cmd) {
	if m.detail.roster == nil {
		m.errText = "roster is not ready yet"
		return m, nil
	}
	if len(m.detail.roster.Players) == 0 {
		m.errText = "no players found in this demo"
		return m, nil
	}
	players := m.detail.roster.Players
	m.rosterPlayers = players
	cols := []table.Column{
		{Title: "Player", Width: 20},
		{Title: "Team", Width: 4},
		{Title: "K", Width: 4},
		{Title: "D", Width: 4},
		{Title: "A", Width: 4},
		{Title: "HS%", Width: 5},
		{Title: "ADR", Width: 6},
		{Title: "Rating", Width: 6},
	}
	rows := make([]table.Row, len(players))
	for i, p := range players {
		rows[i] = table.Row{
			truncate(p.Name, 20), p.Team,
			fmt.Sprintf("%d", p.Kills), fmt.Sprintf("%d", p.Deaths), fmt.Sprintf("%d", p.Assists),
			fmt.Sprintf("%.0f", p.HSPct), fmt.Sprintf("%.0f", p.ADR), fmt.Sprintf("%.2f", p.Rating),
		}
	}
	h := clampInt(len(players)+1, 3, 16)
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
	)
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).Foreground(colAccent).BorderBottom(true).BorderForeground(colFaint)
	st.Selected = st.Selected.Bold(true).Foreground(colInverted).Background(colAccent)
	t.SetStyles(st)
	m.roster = t
	m.mode = modeRoster
	m.errText = ""
	return m, nil
}

func (m model) handleRosterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBrowse
		return m, nil
	case "enter":
		idx := m.roster.Cursor()
		if idx < 0 || idx >= len(m.rosterPlayers) {
			return m, nil
		}
		steamID := m.rosterPlayers[idx].SteamID64
		job := m.focusedJob()
		if job == nil {
			m.mode = modeBrowse
			return m, nil
		}
		id := job.ID
		cl := m.cl
		m.mode = modeBrowse
		m.busy = true
		return m, runAction("parsing "+truncate(m.rosterPlayers[idx].Name, 20), func(c context.Context) error {
			_, err := cl.StartParse(c, id, steamID)
			return err
		})
	}
	var cmd tea.Cmd
	m.roster, cmd = m.roster.Update(msg)
	return m, cmd
}

// ---- overlays: segments ----------------------------------------------------

func (m model) openSegments() (tea.Model, tea.Cmd) {
	segs := m.detail.plan.Segments
	sel := make(map[int]bool, len(segs))
	for i := range segs {
		sel[i] = true // default: record every segment, like the "all kills" flow
	}
	m.segs = segmentPicker{segments: segs, cursor: 0, selected: sel}
	m.mode = modeSegments
	m.errText = ""
	return m, nil
}

func (m model) handleSegmentsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBrowse
		return m, nil
	case "up", "k":
		if m.segs.cursor > 0 {
			m.segs.cursor--
		}
	case "down", "j":
		if m.segs.cursor < len(m.segs.segments)-1 {
			m.segs.cursor++
		}
	case " ", "x":
		m.segs.selected[m.segs.cursor] = !m.segs.selected[m.segs.cursor]
	case "a":
		all := !m.segs.allSelected()
		for i := range m.segs.segments {
			m.segs.selected[i] = all
		}
	case "enter":
		if len(m.segs.selectedIDs()) == 0 {
			m.errText = "select at least one segment"
			return m, nil
		}
		return m.openPreset(presetForRecord)
	}
	return m, nil
}

// ---- overlays: preset ------------------------------------------------------

func (m model) openPreset(purpose presetPurpose) (tea.Model, tea.Cmd) {
	if len(m.presets) == 0 {
		m.errText = "preset registry not loaded"
		return m, nil
	}
	m.mode = modePreset
	m.presetPurpose = purpose
	m.presetCursor = 0
	for i, p := range m.presets {
		if p.Default {
			m.presetCursor = i
		}
	}
	m.errText = ""
	return m, nil
}

func (m model) handlePresetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBrowse
		return m, nil
	case "up", "k":
		if m.presetCursor > 0 {
			m.presetCursor--
		}
	case "down", "j":
		if m.presetCursor < len(m.presets)-1 {
			m.presetCursor++
		}
	case "enter":
		preset := m.presets[m.presetCursor].Name
		job := m.focusedJob()
		if job == nil {
			m.mode = modeBrowse
			return m, nil
		}
		id := job.ID
		cl := m.cl
		if m.presetPurpose == presetForRecord {
			ids := m.segs.selectedIDs()
			m.mode = modeBrowse
			m.busy = true
			return m, runAction(fmt.Sprintf("recording %d segment(s)", len(ids)), func(c context.Context) error {
				_, err := cl.StartRecording(c, id, preset, ids)
				return err
			})
		}
		m.mode = modeBrowse
		m.busy = true
		return m, runAction("render enqueued ("+preset+")", func(c context.Context) error {
			_, err := cl.StartRenderVariant(c, id, preset)
			return err
		})
	}
	return m, nil
}

// ---- segmentPicker methods -------------------------------------------------

func (s segmentPicker) selectedIDs() []string {
	var out []string
	for i, seg := range s.segments {
		if s.selected[i] {
			out = append(out, seg.ID)
		}
	}
	return out
}

func (s segmentPicker) allSelected() bool {
	for i := range s.segments {
		if !s.selected[i] {
			return false
		}
	}
	return len(s.segments) > 0
}
