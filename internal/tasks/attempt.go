package tasks

import "context"

type taskAttemptContextKey struct{}

type taskAttempt struct {
	retried  int
	maxRetry int
}

// WithTaskAttempt records queue execution metadata for consumers that need to
// distinguish an intermediate failure from the final attempt. The desktop
// inline queue uses this because Asynq's server-only context keys are private.
func WithTaskAttempt(ctx context.Context, retried, maxRetry int) context.Context {
	return context.WithValue(ctx, taskAttemptContextKey{}, taskAttempt{
		retried:  retried,
		maxRetry: maxRetry,
	})
}

// TaskAttempt returns queue execution metadata previously attached with
// WithTaskAttempt. A false result means the caller is outside the inline queue.
func TaskAttempt(ctx context.Context) (retried, maxRetry int, ok bool) {
	attempt, ok := ctx.Value(taskAttemptContextKey{}).(taskAttempt)
	if !ok {
		return 0, 0, false
	}
	return attempt.retried, attempt.maxRetry, true
}
