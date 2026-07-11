package main

import (
	"time"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// Messages carry the result of async work back into the Bubble Tea update loop.
// Every client call runs in a tea.Cmd goroutine and returns one of these.

type errMsg struct{ err error }

type noticeMsg string

type capsMsg struct{ caps tuiclient.Capabilities }

type presetsMsg struct{ presets []tuiclient.Preset }

type jobsMsg struct{ jobs []tuiclient.Job }

type streamsMsg struct{ jobs []tuiclient.StreamJob }

// jobDetailMsg bundles the focused demo job with the sub-resources relevant to
// its status (roster when scanned, plan when parsed, render state when
// recorded+). Sub-resources that are not ready are left nil.
type jobDetailMsg struct {
	id     string
	job    tuiclient.Job
	plan   *tuiclient.Plan
	roster *tuiclient.RosterResult
	render *tuiclient.RenderVariantState
}

type streamDetailMsg struct {
	id     string
	job    tuiclient.StreamJob
	plan   *tuiclient.StreamEditPlan
	render *tuiclient.StreamRenderState
}

// publishMsg carries a freshly loaded publish board into the update loop.
type publishMsg struct{ board tuiclient.PublishBoard }

// actionMsg reports the outcome of a mutating action (parse, record, ...).
type actionMsg struct {
	note    string
	refresh bool
}

type tickMsg time.Time
