package main

import (
	"fmt"

	"github.com/rechedev9/fragforge/internal/obs"
)

// recordShortFailure records a failed `zv short` stage to the local obs journal.
// It is best-effort: observability must never block or fail the CLI, so any
// recorder error is ignored.
func recordShortFailure(stage shortStage, code int, demo string) {
	rec := obs.Default()
	if rec == nil {
		return
	}
	obsStage, class := shortStageClass(stage.binary, code)
	_ = rec.RecordError(obs.Event{
		Stage:    obsStage,
		Class:    class,
		Message:  fmt.Sprintf("stage %q exited %d", stage.label, code),
		Demo:     demo,
		ExitCode: code,
	})
}

// shortStageClass maps a stage binary and its exit code to an obs stage label
// and error class. Parser exit codes are defined by cmd/zv-parser.
func shortStageClass(binary string, code int) (stage, class string) {
	switch binary {
	case "zv-parser":
		switch code {
		case 3:
			return obs.StageParse, "file_error"
		case 4:
			return obs.StageParse, "corrupt"
		case 5:
			return obs.StageParse, "target_not_found"
		default:
			return obs.StageParse, "parse_failed"
		}
	case "zv-recorder":
		// Exit code 6 is cmd/zv-recorder's exitHookIncompatible; keep the two
		// integers in sync (separate `main` packages can't share the constant).
		if code == 6 {
			return obs.StageRecord, "capture_incompatible"
		}
		return obs.StageRecord, "record_failed"
	case "zv-rhythm":
		return obs.StageRender, "rhythm_failed"
	case "zv-editor":
		return obs.StageRender, "render_failed"
	default:
		return "short", "stage_failed"
	}
}
