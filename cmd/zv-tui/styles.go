package main

import "github.com/charmbracelet/lipgloss"

// Palette. Kept small and terminal-256 friendly so it reads on both light and
// dark backgrounds.
var (
	colAccent   = lipgloss.Color("39")  // cyan-blue: focus, selection
	colMuted    = lipgloss.Color("244") // dim gray: secondary text, borders
	colFaint    = lipgloss.Color("240")
	colOK       = lipgloss.Color("42")  // green: ready/done
	colWarn     = lipgloss.Color("214") // amber: in-flight
	colErr      = lipgloss.Color("203") // red: failed/errors
	colText     = lipgloss.Color("252")
	colInverted = lipgloss.Color("236")
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)

	tabActive = lipgloss.NewStyle().Bold(true).Foreground(colInverted).
			Background(colAccent).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)

	panelFocused = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(colAccent)
	panelBlurred = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(colFaint)

	panelTitle        = lipgloss.NewStyle().Bold(true).Foreground(colText)
	panelTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(colAccent)

	itemSelected = lipgloss.NewStyle().Bold(true).Foreground(colInverted).Background(colAccent)
	itemNormal   = lipgloss.NewStyle().Foreground(colText)
	itemDim      = lipgloss.NewStyle().Foreground(colMuted)

	labelStyle = lipgloss.NewStyle().Foreground(colMuted)
	valueStyle = lipgloss.NewStyle().Foreground(colText)

	statusBar = lipgloss.NewStyle().Foreground(colText)
	hintStyle = lipgloss.NewStyle().Foreground(colMuted)
	hintKey   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	errorStyle  = lipgloss.NewStyle().Foreground(colErr).Bold(true)
	noticeStyle = lipgloss.NewStyle().Foreground(colOK)

	overlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).
			Padding(0, 1)

	checkboxOn  = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	checkboxOff = lipgloss.NewStyle().Foreground(colFaint)

	spinnerStyle = lipgloss.NewStyle().Foreground(colWarn)
)

// statusBadge renders a job/render/stream status with a color that signals where
// it sits in the pipeline: green = a resting/success state the operator can act
// on, amber = a stage running, red = failed, muted = queued/idle.
func statusBadge(status string) string {
	var c lipgloss.Color
	switch status {
	case "parsed", "scanned", "recorded", "composed", "done", "ready", "rendered", "uploaded":
		c = colOK
	case "scanning", "parsing", "recording", "composing", "rendering", "queued", "acquiring":
		c = colWarn
	case "failed":
		c = colErr
	default:
		c = colMuted
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(status)
}

// key renders a "key action" pair for the hint bar.
func keyHint(k, action string) string {
	return hintKey.Render(k) + hintStyle.Render(" "+action)
}

// statusDot is a one-glyph colored indicator of a pipeline state, for list rows.
func statusDot(status string) string {
	var c lipgloss.Color
	switch status {
	case "parsed", "scanned", "recorded", "composed", "done", "ready", "rendered", "uploaded":
		c = colOK
	case "scanning", "parsing", "recording", "composing", "rendering", "queued", "acquiring":
		c = colWarn
	case "failed":
		c = colErr
	default:
		c = colMuted
	}
	return lipgloss.NewStyle().Foreground(c).Render("●")
}
