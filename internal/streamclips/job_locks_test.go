package streamclips

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJobLocksSerializeSameJobButNotDifferentJobs(t *testing.T) {
	locks := NewJobLocks()
	firstID := uuid.New()
	releaseFirst := locks.Lock(firstID)

	sameAcquired := make(chan struct{})
	go func() {
		release := locks.Lock(firstID)
		close(sameAcquired)
		release()
	}()

	select {
	case <-sameAcquired:
		t.Fatal("same job lock acquired before release")
	case <-time.After(20 * time.Millisecond):
	}

	releaseOther := locks.Lock(uuid.New())
	releaseOther()
	releaseFirst()
	// A second release is deliberately harmless so deferred cleanup cannot
	// corrupt the reference count after an early explicit release.
	releaseFirst()

	select {
	case <-sameAcquired:
	case <-time.After(time.Second):
		t.Fatal("same job lock did not acquire after release")
	}
}
