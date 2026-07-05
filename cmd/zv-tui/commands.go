package main

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// actionTimeout bounds any single API call the TUI makes. Uploads can be large,
// so it is generous; the client's own HTTP timeout is the finer bound.
const actionTimeout = 60 * time.Second

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), actionTimeout)
}

// loadCaps fetches capture readiness; retried each tick until it succeeds.
func (m *model) loadCaps() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		caps, err := cl.Capabilities(c)
		if err != nil {
			return pollErrMsg{pollCaps, err}
		}
		return capsMsg{caps}
	}
}

// loadPresets fetches the render preset registry; retried each tick until it
// succeeds.
func (m *model) loadPresets() tea.Cmd {
	cl := m.cl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		list, err := cl.Presets(c)
		if err != nil {
			return pollErrMsg{pollPresets, err}
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
			return pollErrMsg{pollJobs, err}
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
			return pollErrMsg{pollStreams, err}
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
			return pollErrMsg{pollJobDetail, err}
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
			return pollErrMsg{pollStreamDetail, err}
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

// tick schedules the next poll.
func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}
