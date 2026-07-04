package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

func (m model) View() string {
	if !m.ready || m.width < 40 || m.height < 10 {
		return "starting FragForge TUI… (make the terminal at least 40x10)"
	}
	header := m.viewHeader()
	footer := m.viewFooter()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 3 {
		bodyH = 3
	}
	var body string
	if m.mode == modeBrowse {
		body = m.viewBrowse(bodyH)
	} else {
		body = m.viewOverlay(bodyH)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// ---- header ----------------------------------------------------------------

func (m model) viewHeader() string {
	tabDemos := "Demos → Reel"
	tabStreams := "Stream Clips"
	var tabs string
	if m.screen == screenDemos {
		tabs = tabActive.Render(tabDemos) + " " + tabInactive.Render(tabStreams)
	} else {
		tabs = tabInactive.Render(tabDemos) + " " + tabActive.Render(tabStreams)
	}
	left := titleStyle.Render("FragForge") + "  " + tabs

	right := hintStyle.Render(m.cl.BaseURL())
	if m.capsLoaded {
		right = m.capsSummary() + "  " + right
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) capsSummary() string {
	tag := func(label string, on bool) string {
		if on {
			return lipgloss.NewStyle().Foreground(colOK).Render(label)
		}
		return lipgloss.NewStyle().Foreground(colFaint).Render(label)
	}
	return hintStyle.Render("capture: ") +
		tag("rec", m.caps.Record.Enabled) + " " +
		tag("render", m.caps.Render.Enabled) + " " +
		tag("compose", m.caps.Compose.Enabled)
}

// ---- footer ----------------------------------------------------------------

func (m model) viewFooter() string {
	var status string
	switch {
	case m.errText != "":
		status = errorStyle.Render("✗ " + truncate(m.errText, m.width-2))
	case m.busy:
		status = m.spinner.View() + " working…"
	case m.notice != "":
		status = noticeStyle.Render("✓ " + truncate(m.notice, m.width-2))
	default:
		status = hintStyle.Render(m.contextLine())
	}
	hints := m.hintBar()
	return lipgloss.JoinVertical(lipgloss.Left, padLine(status, m.width), padLine(hints, m.width))
}

// contextLine describes the focused job's current pipeline step.
func (m model) contextLine() string {
	if m.screen == screenDemos {
		if j := m.focusedJob(); j != nil {
			return "step: " + tuiclient.NextStep(j.Status, m.detail.render).Label()
		}
		return "no demos yet - press u to upload a .dem, or drop one here"
	}
	if s := m.focusedStream(); s != nil {
		return "step: " + string(tuiclient.NextStreamStep(s.Status))
	}
	return "no stream clips yet - press u to upload an .mp4 or U for a URL"
}

func (m model) hintBar() string {
	sep := hintStyle.Render("  ")
	switch m.mode {
	case modePrompt:
		return join(sep, keyHint("enter", "confirm"), keyHint("esc", "cancel"))
	case modeRoster:
		return join(sep, keyHint("↑↓", "player"), keyHint("enter", "parse"), keyHint("esc", "back"))
	case modeSegments:
		return join(sep, keyHint("↑↓", "move"), keyHint("space", "toggle"), keyHint("a", "all"), keyHint("enter", "preset"), keyHint("esc", "back"))
	case modePreset:
		return join(sep, keyHint("↑↓", "preset"), keyHint("enter", "confirm"), keyHint("esc", "back"))
	case modeStreamEdit:
		return join(sep, keyHint("↑↓", "clip"), keyHint("a", "add"), keyHint("e", "edit"),
			keyHint("d", "delete"), keyHint("s", "save"), keyHint("esc", "back"))
	case modePublish:
		return join(sep, keyHint("m", "toggle uploaded"), keyHint("r", "refresh"), keyHint("esc", "back"))
	}
	if m.screen == screenDemos {
		return join(sep,
			keyHint("↑↓", "nav"), keyHint("tab", "streams"), keyHint("u", "upload"),
			keyHint("enter", "next step"), keyHint("r", "record"), keyHint("c", "compose"),
			keyHint("R", "render"), keyHint("d", "download"), keyHint("P", "publish"), keyHint("q", "quit"))
	}
	return join(sep,
		keyHint("↑↓", "nav"), keyHint("tab", "demos"), keyHint("u", "upload"),
		keyHint("U", "url"), keyHint("e", "edit clips"), keyHint("R", "render"), keyHint("q", "quit"))
}

// ---- browse body -----------------------------------------------------------

func (m model) viewBrowse(bodyH int) string {
	leftTotal := clampInt(m.width*38/100, 30, 54)
	if leftTotal > m.width-20 {
		leftTotal = m.width - 20
	}
	rightTotal := m.width - leftTotal

	// Content is built one column narrower than the inner width to leave a
	// single-column left gutter inside the border (panelBox adds it).
	var listTitle, listContent, detailTitle, detailContent string
	if m.screen == screenDemos {
		listTitle = fmt.Sprintf("Jobs (%d)", len(m.jobs))
		listContent = m.demoList(leftTotal-3, bodyH-3)
		detailTitle, detailContent = m.demoDetail(rightTotal-3, bodyH-3)
	} else {
		listTitle = fmt.Sprintf("Stream jobs (%d)", len(m.streams))
		listContent = m.streamList(leftTotal-3, bodyH-3)
		detailTitle, detailContent = m.streamDetail_(rightTotal-3, bodyH-3)
	}

	left := panelBox(listTitle, listContent, leftTotal, bodyH, true)
	right := panelBox(detailTitle, detailContent, rightTotal, bodyH, false)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// demoList renders the scrollable jobs list, keeping the cursor visible.
func (m model) demoList(w, h int) string {
	if len(m.jobs) == 0 {
		return itemDim.Render("(no demos)")
	}
	start := scrollStart(m.jobCursor, len(m.jobs), h)
	var lines []string
	for i := start; i < len(m.jobs) && i < start+h; i++ {
		j := m.jobs[i]
		target := j.TargetSteamID
		if target == "" {
			target = "unscanned"
		}
		text := fmt.Sprintf("%-8s %-9s %3s  %s", shortID(j.ID), j.Status, relTime(j.CreatedAt), target)
		lines = append(lines, listRow(text, statusDot(j.Status), i == m.jobCursor, w))
	}
	return strings.Join(lines, "\n")
}

func (m model) streamList(w, h int) string {
	if len(m.streams) == 0 {
		return itemDim.Render("(no stream clips)")
	}
	start := scrollStart(m.streamCursor, len(m.streams), h)
	var lines []string
	for i := start; i < len(m.streams) && i < start+h; i++ {
		s := m.streams[i]
		label := s.Title
		if label == "" {
			label = shortID(s.ID)
		}
		text := fmt.Sprintf("%-9s %3s  %s", s.Status, relTime(s.CreatedAt), label)
		lines = append(lines, listRow(text, statusDot(s.Status), i == m.streamCursor, w))
	}
	return strings.Join(lines, "\n")
}

// ---- demo detail -----------------------------------------------------------

func (m model) demoDetail(w, h int) (string, string) {
	j := m.focusedJob()
	if j == nil {
		return "Detail", itemDim.Render("Select or upload a demo.")
	}
	var b []string
	b = append(b, field("job", shortID(j.ID)+"  "+statusBadge(j.Status), w))
	if j.TargetSteamID != "" {
		b = append(b, field("target", j.TargetSteamID, w))
	}
	if j.FailureReason != "" {
		b = append(b, field("error", errorStyle.Render(truncate(j.FailureReason, w-8)), w))
	}
	b = append(b, "")

	switch {
	case m.detail.roster != nil:
		b = append(b, sectionRoster(m.detail.roster, w)...)
	case m.detail.plan != nil:
		b = append(b, sectionPlan(m.detail.plan, w)...)
	case !m.detail.loaded:
		b = append(b, itemDim.Render(m.spinner.View()+" loading…"))
	}

	if m.detail.render != nil {
		b = append(b, "")
		b = append(b, sectionRender(m.detail.render, w)...)
	}

	b = append(b, "")
	b = append(b, hintStyle.Render(nextHint(tuiclient.NextStep(j.Status, m.detail.render))))
	return "Detail", strings.Join(b, "\n")
}

func sectionRoster(r *tuiclient.RosterResult, w int) []string {
	out := []string{panelTitle.Render("Roster") + labelStyle.Render(fmt.Sprintf("  %s  %d-%d", r.Match.Map, r.Match.ScoreCT, r.Match.ScoreT))}
	top := r.Players
	if len(top) > 8 {
		top = top[:8]
	}
	for _, p := range top {
		out = append(out, truncate(fmt.Sprintf("  %-18s %2s  %d/%d/%d  %.0f rating",
			p.Name, p.Team, p.Kills, p.Deaths, p.Assists, p.Rating), w))
	}
	out = append(out, labelStyle.Render("  press enter to pick a player"))
	return out
}

func sectionPlan(p *tuiclient.Plan, w int) []string {
	out := []string{
		panelTitle.Render("Kill plan") + labelStyle.Render("  "+p.Demo.Map),
		field("target", p.Target.NameInDemo, w),
		field("kills", fmt.Sprintf("%d (%d after filters)", p.Stats.TotalKillsTarget, p.Stats.KillsAfterFilters), w),
		field("segments", fmt.Sprintf("%d  ·  %.0fs total", p.Stats.SegmentsCreated, p.Stats.DurationSecondsTotal), w),
	}
	shown := p.Segments
	if len(shown) > 6 {
		shown = shown[:6]
	}
	for _, s := range shown {
		out = append(out, truncate(fmt.Sprintf("  %s  r%d  %d kill(s)", s.ID, s.Round, len(s.Kills)), w))
	}
	if len(p.Segments) > 6 {
		out = append(out, labelStyle.Render(fmt.Sprintf("  … +%d more", len(p.Segments)-6)))
	}
	return out
}

func sectionRender(r *tuiclient.RenderVariantState, w int) []string {
	out := []string{panelTitle.Render("Render: ") + r.Variant + "  " + statusBadge(r.Status)}
	if r.Error != "" {
		out = append(out, field("error", errorStyle.Render(truncate(r.Error, w-8)), w))
	}
	for _, warn := range r.Warnings {
		out = append(out, labelStyle.Render("  ! "+truncate(warn, w-4)))
	}
	return out
}

// ---- stream detail ---------------------------------------------------------

func (m model) streamDetail_(w, h int) (string, string) {
	s := m.focusedStream()
	if s == nil {
		return "Detail", itemDim.Render("Select or upload a stream clip.")
	}
	var b []string
	title := s.Title
	if title == "" {
		title = shortID(s.ID)
	}
	b = append(b, field("clip", title+"  "+statusBadge(s.Status), w))
	if s.SourceURL != "" {
		b = append(b, field("source", truncate(s.SourceURL, w-8), w))
	}
	if s.FailureReason != "" {
		b = append(b, field("error", errorStyle.Render(truncate(s.FailureReason, w-8)), w))
	}
	if s.Probe.Width > 0 {
		b = append(b, field("video", fmt.Sprintf("%dx%d  %.0fs  %s", s.Probe.Width, s.Probe.Height, s.Probe.DurationSeconds, s.Probe.VideoCodec), w))
	}
	b = append(b, "")
	if m.streamDetail.plan != nil {
		b = append(b, panelTitle.Render("Edit plan"))
		b = append(b, field("variant", m.streamDetail.plan.Variant, w))
		b = append(b, field("clips", fmt.Sprintf("%d", len(m.streamDetail.plan.Clips)), w))
	} else if !m.streamDetail.loaded {
		b = append(b, itemDim.Render(m.spinner.View()+" loading…"))
	}
	if m.streamDetail.render != nil && m.streamDetail.render.Status != "" {
		b = append(b, "")
		b = append(b, panelTitle.Render("Render: ")+statusBadge(m.streamDetail.render.Status))
		for _, v := range m.streamDetail.render.Videos {
			b = append(b, truncate("  • "+v.ClipID, w))
		}
	}
	b = append(b, "")
	b = append(b, hintStyle.Render("press e to edit clips, R to render (needs ffmpeg)"))
	return "Detail", strings.Join(b, "\n")
}

// ---- overlays --------------------------------------------------------------

func (m model) viewOverlay(bodyH int) string {
	var title, content string
	switch m.mode {
	case modePrompt:
		title = m.promptTitle()
		content = m.prompt.View()
	case modeRoster:
		title = "Pick a player to feature"
		content = m.roster.View()
	case modeSegments:
		title = "Select segments to record"
		content = m.segmentsView()
	case modePreset:
		title = "Pick a render preset"
		content = m.presetView()
	case modeStreamEdit:
		title = "Edit clip plan"
		content = m.clipEditorView()
	case modePublish:
		title = "Publish"
		content = m.publishView()
	default:
		title = ""
		content = ""
	}
	box := overlayStyle.Render(panelTitleFocused.Render(title) + "\n\n" + content)
	return lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, box)
}

func (m model) promptTitle() string {
	switch m.promptKind {
	case promptUploadDemo:
		return "Upload a demo (.dem)"
	case promptUploadStream:
		return "Upload a streamer clip (.mp4)"
	case promptStreamURL:
		return "Acquire a stream clip by URL"
	case promptClipRange:
		return "Clip range (start end [title], seconds)"
	}
	return "Input"
}

// publishView renders the publish board: artifact readiness per segment and
// whether the reel has been marked uploaded.
func (m model) publishView() string {
	if !m.pub.loaded {
		return itemDim.Render(m.spinner.View() + " loading publish board…")
	}
	b := m.pub.board
	ready := checkboxOff.Render("not ready")
	if b.RenderReady {
		ready = checkboxOn.Render("ready")
	}
	uploaded := checkboxOff.Render("not uploaded")
	if b.Uploaded {
		uploaded = checkboxOn.Render("uploaded")
	}
	lines := []string{
		labelStyle.Render("variant ") + b.Variant,
		labelStyle.Render("render  ") + ready + "   " + uploaded,
	}
	if b.Error != "" {
		lines = append(lines, errorStyle.Render("error   "+b.Error))
	}
	lines = append(lines, "", panelTitle.Render("Artifacts (video / cover / caption)"))
	if len(b.Items) == 0 {
		lines = append(lines, itemDim.Render("  (no items)"))
	}
	for _, it := range b.Items {
		lines = append(lines, fmt.Sprintf("  %-10s %s %s %s",
			shortID(it.SegmentID), mark(it.VideoReady), mark(it.CoverReady), mark(it.CaptionReady)))
	}
	return strings.Join(lines, "\n")
}

// mark renders a readiness tick or cross.
func mark(ok bool) string {
	if ok {
		return checkboxOn.Render("✓")
	}
	return checkboxOff.Render("✗")
}

// clipEditorView renders the stream clip list being edited.
func (m model) clipEditorView() string {
	if len(m.clipEd.clips) == 0 {
		return itemDim.Render("(no clips - press a to add one)")
	}
	var lines []string
	for i, c := range m.clipEd.clips {
		title := c.Title
		if title == "" {
			title = "(untitled)"
		}
		row := fmt.Sprintf("%-4s %6.1f - %-6.1f  %s", c.ID, c.StartSeconds, c.EndSeconds, title)
		if i == m.clipEd.cursor {
			lines = append(lines, itemSelected.Render("▌ "+row))
		} else {
			lines = append(lines, "  "+itemNormal.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) segmentsView() string {
	var lines []string
	start := scrollStart(m.segs.cursor, len(m.segs.segments), 14)
	for i := start; i < len(m.segs.segments) && i < start+14; i++ {
		s := m.segs.segments[i]
		box := checkboxOff.Render("[ ]")
		if m.segs.selected[i] {
			box = checkboxOn.Render("[x]")
		}
		text := fmt.Sprintf("%s  r%d  %d kill(s)", s.ID, s.Round, len(s.Kills))
		cursor := "  "
		if i == m.segs.cursor {
			cursor = hintKey.Render("▌ ")
			text = itemNormal.Bold(true).Render(text)
		} else {
			text = itemNormal.Render(text)
		}
		lines = append(lines, cursor+box+" "+text)
	}
	footer := labelStyle.Render(fmt.Sprintf("%d of %d selected", len(m.segs.selectedIDs()), len(m.segs.segments)))
	return strings.Join(lines, "\n") + "\n\n" + footer
}

func (m model) presetView() string {
	var lines []string
	for i, p := range m.presets {
		label := p.Name
		if p.Label != "" {
			label = p.Label
		}
		row := fmt.Sprintf("%-16s %dx%d @%dfps", label, p.Width, p.Height, p.FPS)
		if i == m.presetCursor {
			lines = append(lines, itemSelected.Render("▌ "+row))
			if p.Description != "" {
				lines = append(lines, labelStyle.Render("   "+truncate(p.Description, 48)))
			}
		} else {
			marker := "  "
			if p.Default {
				marker = lipgloss.NewStyle().Foreground(colOK).Render("★ ")
			}
			lines = append(lines, marker+itemNormal.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

// ---- low-level rendering helpers -------------------------------------------

// panelBox draws a titled, bordered box of exactly totalW x totalH cells.
func panelBox(title, content string, totalW, totalH int, focused bool) string {
	innerW := totalW - 2
	innerH := totalH - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 2 {
		innerH = 2
	}
	titleStyleFor := panelTitle
	if focused {
		titleStyleFor = panelTitleFocused
	}
	contentW := innerW - 1
	if contentW < 1 {
		contentW = 1
	}
	// One-column left gutter: indent the title and every body line by a space.
	head := padLine(" "+titleStyleFor.Render(truncate(title, contentW)), innerW)
	raw := strings.Split(content, "\n")
	indented := make([]string, len(raw))
	for i, l := range raw {
		indented[i] = " " + l
	}
	bodyLines := block(indented, innerW, innerH-1)
	inner := head + "\n" + bodyLines
	style := panelBlurred
	if focused {
		style = panelFocused
	}
	return style.Render(inner)
}

// block pads/limits a slice of lines to exactly h rows, each padded to width w.
func block(lines []string, w, h int) string {
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out = append(out, padLine(lines[i], w))
		} else {
			out = append(out, padLine("", w))
		}
	}
	return strings.Join(out, "\n")
}

// padLine pads or truncates a single line to width w, ANSI-aware. It truncates
// first so lipgloss's Width only ever pads: Width wraps a too-long line into
// extra rows (before MaxWidth would trim it), which would break the panels'
// fixed height. Truncating up front guarantees one row out.
func padLine(s string, w int) string {
	if w < 1 {
		w = 1
	}
	s = ansi.Truncate(s, w, "")
	return lipgloss.NewStyle().Width(w).Render(s)
}

// listRow renders one selectable list row: colored dot + text, or a full-width
// highlight when selected.
func listRow(text, dot string, selected bool, w int) string {
	t := truncate(text, w-2)
	if selected {
		return itemSelected.Width(w).MaxWidth(w).Render("▌ " + t)
	}
	return dot + " " + itemNormal.Render(t)
}

// field renders a "label  value" detail line truncated to width w.
func field(label, value string, w int) string {
	l := labelStyle.Render(fmt.Sprintf("%-9s", label))
	return padLine(l+" "+value, w)
}

// scrollStart returns the first index to render so the cursor stays visible.
func scrollStart(cursor, total, visible int) int {
	if total <= visible || visible <= 0 {
		return 0
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start > total-visible {
		start = total - visible
	}
	return start
}

func nextHint(step tuiclient.Step) string {
	switch step {
	case tuiclient.StepPickTarget:
		return "→ press enter to pick a player and parse"
	case tuiclient.StepRecord:
		return "→ press r to record segments"
	case tuiclient.StepRender:
		return "→ press R to render, c to compose, d to download"
	case tuiclient.StepReady:
		return "→ reel ready - press d to download, P to publish"
	case tuiclient.StepRetry:
		return "→ press r to retry recording"
	default:
		return "→ " + step.Label()
	}
}

func join(sep string, parts ...string) string {
	return strings.Join(parts, sep)
}
