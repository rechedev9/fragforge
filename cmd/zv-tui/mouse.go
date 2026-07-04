package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// listTopRow is the first screen row of a list panel's rows: header (1) +
// panel top border (1) + panel title (1).
const listTopRow = 3

// handleMouse implements lazygit-style mouse support: wheel scrolls the
// focused list (browse and overlays), a click on a header tab switches
// screens, a click on a list row selects it, and a click on the already
// selected row runs its primary action (same as enter).
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.moveCursor(-1)
	case tea.MouseButtonWheelDown:
		return m.moveCursor(1)
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}
	if m.mode != modeBrowse {
		return m, nil // overlays stay keyboard-driven (wheel handled above)
	}
	if msg.Y == 0 {
		return m.clickTab(msg.X)
	}
	if msg.X >= leftPanelTotal(m.width) {
		return m, nil
	}
	return m.clickListRow(msg.Y)
}

// moveCursor moves whichever cursor the current mode owns by delta rows.
func (m model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeBrowse:
		if m.screen == screenDemos {
			next := clampInt(m.jobCursor+delta, 0, max(0, len(m.jobs)-1))
			if next != m.jobCursor {
				m.jobCursor = next
				return m, m.detailCmd()
			}
			return m, nil
		}
		next := clampInt(m.streamCursor+delta, 0, max(0, len(m.streams)-1))
		if next != m.streamCursor {
			m.streamCursor = next
			return m, m.streamDetailCmd()
		}
	case modeSegments:
		m.segs.cursor = clampInt(m.segs.cursor+delta, 0, max(0, len(m.segs.segments)-1))
	case modePreset:
		m.presetCursor = clampInt(m.presetCursor+delta, 0, max(0, len(m.presets)-1))
	case modeRoster:
		if delta < 0 {
			m.roster.MoveUp(1)
		} else {
			m.roster.MoveDown(1)
		}
	}
	return m, nil
}

func (m model) clickTab(x int) (tea.Model, tea.Cmd) {
	switch tabAtX(x) {
	case 0:
		if m.screen != screenDemos {
			m.screen = screenDemos
			m.errText = ""
			return m, m.refreshCurrent()
		}
	case 1:
		if m.screen != screenStreams {
			m.screen = screenStreams
			m.errText = ""
			return m, m.refreshCurrent()
		}
	}
	return m, nil
}

func (m model) clickListRow(y int) (tea.Model, tea.Cmd) {
	visible := m.height - 3 - listTopRow // bodyH (height - header - footer) minus panel chrome
	if m.screen == screenDemos {
		idx := listIndexAt(y, m.jobCursor, len(m.jobs), visible)
		if idx < 0 {
			return m, nil
		}
		if idx == m.jobCursor {
			return m.actOnDemo()
		}
		m.jobCursor = idx
		return m, m.detailCmd()
	}
	idx := listIndexAt(y, m.streamCursor, len(m.streams), visible)
	if idx < 0 {
		return m, nil
	}
	if idx == m.streamCursor {
		return m.startStreamRender()
	}
	m.streamCursor = idx
	return m, m.streamDetailCmd()
}

// tabAtX maps a header-row x coordinate to a tab index (0 = demos,
// 1 = streams), or -1. Both tab styles pad identically, so the zones do not
// depend on which tab is active.
func tabAtX(x int) int {
	base := lipgloss.Width(titleStyle.Render("FragForge")) + 2
	demosW := lipgloss.Width(tabInactive.Render("Demos → Reel"))
	streamsW := lipgloss.Width(tabInactive.Render("Stream Clips"))
	switch {
	case x >= base && x < base+demosW:
		return 0
	case x >= base+demosW+1 && x < base+demosW+1+streamsW:
		return 1
	}
	return -1
}

// leftPanelTotal mirrors viewBrowse's list-panel width so click zones match
// what is drawn.
func leftPanelTotal(width int) int {
	total := clampInt(width*38/100, 30, 54)
	if total > width-20 {
		total = width - 20
	}
	return total
}

// listIndexAt maps a screen row y to an index in a scrolled list of `total`
// items showing `visible` rows, or -1 when the row is empty or off-list.
func listIndexAt(y, cursor, total, visible int) int {
	row := y - listTopRow
	if row < 0 || row >= visible {
		return -1
	}
	idx := scrollStart(cursor, total, visible) + row
	if idx >= total {
		return -1
	}
	return idx
}
