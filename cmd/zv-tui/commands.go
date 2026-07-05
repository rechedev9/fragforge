package main

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// actionTimeout bounds quick API calls (JSON control-plane requests). Bulk
// media transfers use transferTimeout instead; the HTTP client itself has no
// whole-exchange timeout, so these contexts are the only duration bound.
const actionTimeout = 60 * time.Second

// transferTimeout bounds calls that move a large media body (demo/clip
// uploads, final MP4 download). Generous because multi-GB files over
// Tailscale legitimately take minutes, but still finite so a stalled
// transfer cannot hang the TUI forever.
const transferTimeout = 15 * time.Minute

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), actionTimeout)
}

func transferCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), transferTimeout)
}

// loadCaps fetches capture readiness once at startup.
func (m *model) loadCaps() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		caps, err := cl.Capabilities(c)
		if err != nil {
			return errMsg{err}
		}
		return capsMsg{caps}
	}
}

// loadPresets fetches the render preset registry once at startup.
func (m *model) loadPresets() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		list, err := cl.Presets(c)
		if err != nil {
			return errMsg{err}
		}
		return presetsMsg{list.Presets}
	}
}

// loadJobs refreshes the demo job list.
func (m *model) loadJobs() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		jobs, err := cl.ListJobs(c, 50)
		if err != nil {
			return errMsg{err}
		}
		return jobsMsg{jobs}
	}
}

// loadStreams refreshes the stream-clip job list.
func (m *model) loadStreams() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		jobs, err := cl.ListStreamJobs(c, 50)
		if err != nil {
			// Stream jobs may be unconfigured (501); surface as empty, not error.
			if tuiclient.StatusCode(err) == 501 {
				return streamsMsg{nil}
			}
			return errMsg{err}
		}
		return streamsMsg{jobs}
	}
}

// loadJobDetail fetches the focused demo job plus the sub-resource relevant to
// its status. Sub-resource "not ready" (409) is treated as nil, not an error.
func (m *model) loadJobDetail(id string) tea.Cmd {
	cl := m.cl
	// Snapshot the default variant here (Update goroutine); reading it inside the
	// closure would race the presetsMsg writer.
	variant := m.defaultVariant
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		job, err := cl.GetJob(c, id)
		if err != nil {
			return errMsg{err}
		}
		out := jobDetailMsg{id: id, job: job, plan: job.KillPlan}
		switch job.Status {
		case tuiclient.StatusScanned:
			if roster, err := cl.GetRoster(c, id); err == nil {
				out.roster = &roster
			}
		case tuiclient.StatusParsed, tuiclient.StatusRecording, tuiclient.StatusRecorded,
			tuiclient.StatusComposing, tuiclient.StatusComposed, tuiclient.StatusDone:
			if out.plan == nil {
				if plan, err := cl.GetPlan(c, id); err == nil {
					out.plan = &plan
				}
			}
		}
		// A render variant may exist for any recorded+ job; report it when present.
		if variant != "" {
			if rv, err := cl.GetRenderVariant(c, id, variant); err == nil {
				out.render = &rv
			}
		}
		return out
	}
}

// loadStreamDetail fetches the focused stream job plus its edit plan and render
// state.
func (m *model) loadStreamDetail(id string) tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		job, err := cl.GetStreamJob(c, id)
		if err != nil {
			return errMsg{err}
		}
		out := streamDetailMsg{id: id, job: job}
		if plan, err := cl.GetStreamEditPlan(c, id); err == nil {
			out.plan = &plan
		}
		if rv, err := cl.GetStreamRender(c, id, ""); err == nil {
			out.render = &rv
		}
		return out
	}
}

// runAction wraps a mutating client call, mapping its result to an actionMsg
// (which triggers a refresh) or an errMsg.
func runAction(note string, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		if err := fn(c); err != nil {
			return errMsg{err}
		}
		return actionMsg{note: note, refresh: true}
	}
}

// runTransfer is runAction for calls that stream a large file body and
// therefore need transferTimeout rather than actionTimeout.
func runTransfer(note string, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		c, cancel := transferCtx()
		defer cancel()
		if err := fn(c); err != nil {
			return errMsg{err}
		}
		return actionMsg{note: note, refresh: true}
	}
}

// tick schedules the next poll.
func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}
