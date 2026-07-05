package main

import (
	"testing"
	"time"
)

// TestTimeoutContexts pins that quick calls get actionTimeout and bulk media
// transfers get the longer transferTimeout, and that the two are distinct.
func TestTimeoutContexts(t *testing.T) {
	if transferTimeout <= actionTimeout {
		t.Fatalf("transferTimeout (%v) must exceed actionTimeout (%v)", transferTimeout, actionTimeout)
	}

	assertDeadline := func(name string, deadline time.Time, ok bool, want time.Duration) {
		if !ok {
			t.Fatalf("%s: context has no deadline", name)
		}
		remaining := time.Until(deadline)
		// The deadline is set a hair in the past of now+want by the time we read
		// it, so allow a small slack below want but never above it.
		if remaining > want || remaining <= want-5*time.Second {
			t.Fatalf("%s: remaining = %v, want in (%v, %v]", name, remaining, want-5*time.Second, want)
		}
	}

	c, cancel := ctx()
	deadline, ok := c.Deadline()
	assertDeadline("ctx", deadline, ok, actionTimeout)
	cancel()

	tc, tcancel := transferCtx()
	tdeadline, tok := tc.Deadline()
	assertDeadline("transferCtx", tdeadline, tok, transferTimeout)
	tcancel()
}
