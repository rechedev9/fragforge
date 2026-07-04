package main

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type dropKind int

const (
	dropUnknown dropKind = iota
	dropDemo
	dropStream
)

// classifyDrop maps a dropped file path to the upload flow it belongs to.
func classifyDrop(path string) dropKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".dem":
		return dropDemo
	case ".mp4", ".mov", ".mkv", ".webm":
		return dropStream
	}
	return dropUnknown
}

// parseDroppedPaths splits text produced by dropping files onto the terminal
// (or pasting paths) into candidate file paths. Terminals quote paths with
// spaces, so quoted segments win; otherwise the whole trimmed text is one path.
func parseDroppedPaths(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var out []string
	for _, quote := range []byte{'"', '\''} {
		rest := text
		for {
			start := strings.IndexByte(rest, quote)
			if start < 0 {
				break
			}
			end := strings.IndexByte(rest[start+1:], quote)
			if end < 0 {
				break
			}
			if p := strings.TrimSpace(rest[start+1 : start+1+end]); p != "" {
				out = append(out, p)
			}
			rest = rest[start+1+end+1:]
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{text}
}

// dropCmds builds one upload Cmd per recognized dropped path and reports how
// many paths were skipped as neither demos nor videos.
func (m *model) dropCmds(paths []string) (cmds []tea.Cmd, skipped int) {
	cl := m.cl
	for _, p := range paths {
		p := p
		switch classifyDrop(p) {
		case dropDemo:
			cmds = append(cmds, runAction("demo uploaded - scanning "+filepath.Base(p), func(c context.Context) error {
				_, err := cl.CreateJob(c, p, "")
				return err
			}))
		case dropStream:
			cmds = append(cmds, runAction("stream uploaded "+filepath.Base(p), func(c context.Context) error {
				_, err := cl.CreateStreamJobUpload(c, p, "")
				return err
			}))
		default:
			skipped++
		}
	}
	return cmds, skipped
}

// handleDrop uploads files dropped onto the terminal window (which arrive as a
// bracketed paste of their paths).
func (m model) handleDrop(text string) (tea.Model, tea.Cmd) {
	cmds, skipped := m.dropCmds(parseDroppedPaths(text))
	if len(cmds) == 0 {
		if skipped > 0 {
			m.errText = "dropped file is not a .dem demo or a video clip"
		}
		return m, nil
	}
	m.busy = true
	m.errText = ""
	return m, tea.Batch(cmds...)
}
