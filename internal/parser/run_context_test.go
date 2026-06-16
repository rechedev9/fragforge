package parser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	dp "github.com/markus-wa/godispatch"

	"github.com/rechedev9/fragforge/internal/rules"
)

const testTargetSteamID = "76561198000000000"

// blockingParser is a minimal demoinfocs.Parser test double. Only the methods
// the run* functions touch before dispatching events are implemented; the
// embedded interface makes any other call panic loudly, so the test fails fast
// if the production path starts depending on something new.
type blockingParser struct {
	demoinfocs.Parser
	entered      chan struct{}
	release      chan struct{}
	cancelOnce   sync.Once
	cancelCalled atomic.Bool
	block        bool
	parseErr     error
}

func (p *blockingParser) RegisterEventHandler(any) dp.HandlerIdentifier      { return nil }
func (p *blockingParser) RegisterNetMessageHandler(any) dp.HandlerIdentifier { return nil }
func (p *blockingParser) TickRate() float64                                  { return 64 }
func (p *blockingParser) Close() error                                       { return nil }

func (p *blockingParser) Cancel() {
	p.cancelCalled.Store(true)
	p.cancelOnce.Do(func() { close(p.release) })
}

func (p *blockingParser) ParseToEnd() error {
	if !p.block {
		return p.parseErr
	}
	close(p.entered)
	<-p.release
	return demoinfocs.ErrCancelled
}

func TestRunWithContextAbortsWhenContextCancelled(t *testing.T) {
	p := &blockingParser{entered: make(chan struct{}), release: make(chan struct{}), block: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel once ParseToEnd is actually running, mimicking a task deadline or
	// server shutdown firing mid-parse.
	go func() {
		<-p.entered
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := RunWithContext(ctx, p, testTargetSteamID, rules.Rules{}, PlanMeta{}, RunOptions{})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunWithContext err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithContext did not return after context cancellation")
	}
	if !p.cancelCalled.Load() {
		t.Error("parser Cancel() was not called on context cancellation")
	}
}

func TestRunWithContextReturnsUnderlyingResultWhenNotCancelled(t *testing.T) {
	p := &blockingParser{block: false}

	_, err := RunWithContext(context.Background(), p, testTargetSteamID, rules.Rules{}, PlanMeta{}, RunOptions{})

	if errors.Is(err, context.Canceled) {
		t.Fatalf("RunWithContext err = %v, want the underlying parser result, not a context error", err)
	}
	if p.cancelCalled.Load() {
		t.Error("parser Cancel() was called on a run that was never cancelled")
	}
}

func TestRosterWithContextAbortsWhenContextCancelled(t *testing.T) {
	p := &blockingParser{entered: make(chan struct{}), release: make(chan struct{}), block: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-p.entered
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := RosterWithContext(ctx, p)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RosterWithContext err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RosterWithContext did not return after context cancellation")
	}
	if !p.cancelCalled.Load() {
		t.Error("parser Cancel() was not called on context cancellation")
	}
}

func TestRosterWithContextReturnsResultWhenNotCancelled(t *testing.T) {
	p := &blockingParser{block: false}

	roster, err := RosterWithContext(context.Background(), p)

	if err != nil {
		t.Fatalf("RosterWithContext err = %v, want nil", err)
	}
	if len(roster) != 0 {
		t.Fatalf("roster = %#v, want empty (no events dispatched)", roster)
	}
	if p.cancelCalled.Load() {
		t.Error("parser Cancel() was called on a run that was never cancelled")
	}
}

func TestRunWithOptionsAllowsUnexpectedEndOfDemo(t *testing.T) {
	p := &blockingParser{parseErr: fmt.Errorf("parse frame: %w", demoinfocs.ErrUnexpectedEndOfDemo)}

	_, err := RunWithOptions(p, testTargetSteamID, rules.Default(), PlanMeta{}, RunOptions{})

	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("RunWithOptions err = %v, want target-not-found after recovering from unexpected EOF", err)
	}
	if errors.Is(err, demoinfocs.ErrUnexpectedEndOfDemo) {
		t.Fatalf("RunWithOptions err = %v, want unexpected EOF treated as recoverable", err)
	}
}
