package main

import (
	"time"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// Messages carry the result of async work back into the Bubble Tea update loop.
// Every client call runs in a tea.Cmd goroutine and returns one of these.

// errMsg reports a failed operator-initiated action (upload, parse, record,
// download, ...). It clears the busy flag and is shown until acknowledged or
// superseded. Background fetch failures use pollErrMsg instead.
type errMsg struct{ err error }

// pollSource identifies which background fetch a pollErrMsg came from, so the
// next successful fetch of the same data can clear it.
type pollSource int

const (
	pollCaps pollSource = iota
	pollPresets
	pollJobs
	pollStreams
	pollJobDetail
	pollStreamDetail
)

// pollErrMsg reports a failed background fetch (list/detail poll, caps/preset
// load). Unlike errMsg it never touches the busy flag - an operator action may
// still be in flight - and it clears automatically when the same source next
// succeeds, so a transient blip does not leave a stale error on screen.
type pollErrMsg struct {
	source pollSource
	err    error
}

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

// actionMsg reports the outcome of a mutating action (parse, record, ...).
type actionMsg struct {
	note    string
	refresh bool
}

type tickMsg time.Time
