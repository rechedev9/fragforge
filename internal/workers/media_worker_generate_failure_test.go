package workers

import (
	"context"
	"strings"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
)

func TestRecordWorkerMarksGenerateFailedWhenIntentHeaderIsMalformed(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	w := NewRecordWorker(repo, store, RecordWorkerConfig{})
	valid := generateRecordTask(t, id, renderplan.GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit:    renderplan.DefaultEditRequest(),
	})
	headers := valid.Headers()
	if len(headers) != 1 {
		t.Fatalf("generate task headers = %d, want 1", len(headers))
	}
	for name := range headers {
		headers[name] = "{not-json"
	}
	malformed := asynq.NewTaskWithHeaders(valid.Type(), valid.Payload(), headers)

	err := w.HandleRecordDemo(context.Background(), malformed)
	if err == nil || !strings.Contains(err.Error(), "decode record task generate intent") {
		t.Fatalf("HandleRecordDemo error = %v, want generate intent decode error", err)
	}
	got := repo.jobs[id]
	if got.Status != job.StatusFailed || !strings.Contains(got.FailureReason, "decode record task generate intent") {
		t.Fatalf("job after malformed intent = status %s reason %q, want failed decode reason", got.Status, got.FailureReason)
	}
}
