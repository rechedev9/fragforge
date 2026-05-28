package parser

import (
	"errors"
	"fmt"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

// ErrTargetNotFound is returned when the requested target SteamID was never
// observed in the demo (neither as killer nor as victim). The CLI maps this
// to exit code 5.
var ErrTargetNotFound = errors.New("target steamid not found in demo")

type SegmentMode string

const (
	SegmentModeKills   SegmentMode = "kills"
	SegmentModeSmokes  SegmentMode = "smokes"
	SegmentModeUtility SegmentMode = "utility"
)

type RunOptions struct {
	SegmentMode SegmentMode
}

// Run wires kill event handlers on p, drives the parser to completion, and
// returns the assembled kill plan. The passed PlanMeta supplies demo path
// and SHA256; map name, tickrate, and duration are filled in from the
// parser unless already provided.
func Run(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta) (killplan.Plan, error) {
	return RunWithOptions(p, target, r, m, RunOptions{SegmentMode: SegmentModeKills})
}

func RunWithOptions(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta, opts RunOptions) (killplan.Plan, error) {
	switch opts.SegmentMode {
	case "", SegmentModeKills:
		return runKills(p, target, r, m)
	case SegmentModeSmokes:
		return runSmokes(p, target, r, m)
	case SegmentModeUtility:
		return runUtility(p, target, r, m)
	default:
		return killplan.Plan{}, fmt.Errorf("unknown segment mode %q", opts.SegmentMode)
	}
}
