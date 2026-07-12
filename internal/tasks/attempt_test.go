package tasks

import (
	"context"
	"testing"
)

func TestTaskAttemptRoundTrip(t *testing.T) {
	ctx := WithTaskAttempt(context.Background(), 1, 3)
	retried, maxRetry, ok := TaskAttempt(ctx)
	if !ok {
		t.Fatal("TaskAttempt ok = false, want true")
	}
	if retried != 1 || maxRetry != 3 {
		t.Fatalf("TaskAttempt = (%d, %d), want (1, 3)", retried, maxRetry)
	}
}

func TestTaskAttemptReportsAbsentMetadata(t *testing.T) {
	retried, maxRetry, ok := TaskAttempt(context.Background())
	if ok || retried != 0 || maxRetry != 0 {
		t.Fatalf("TaskAttempt = (%d, %d, %v), want (0, 0, false)", retried, maxRetry, ok)
	}
}
