package workers

import (
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/obs"
)

// recordWorkerFailure appends a terminal worker failure to the local obs
// journal so orchestrator failures show up in the same error log as CLI and
// batch runs. It is best-effort: observability never blocks job processing.
func recordWorkerFailure(id uuid.UUID, taskType string, err error) {
	rec := obs.Default()
	if rec == nil {
		return
	}
	_ = rec.RecordError(obs.Event{
		Stage:   obs.StageWorker,
		Class:   taskType,
		Message: id.String() + ": " + err.Error(),
	})
}
