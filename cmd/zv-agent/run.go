package main

import (
	"context"
	"time"

	"github.com/rechedev9/fragforge/internal/agent"
)

// run serves the local data plane: it heartbeats the control plane for liveness
// and supervises the child orchestrator behind the loopback auth proxy until
// ctx is cancelled.
func run(ctx context.Context, cfg Config) error {
	c := agent.NewClient(cfg.BaseURL, cfg.Token)
	capabilities := map[string]any{"parser": true}
	go agent.HeartbeatLoop(ctx, c, capabilities, cfg.LoopbackToken, cfg.LoopbackPort, 20*time.Second)

	return agent.RunLoopback(ctx, agent.LoopbackConfig{
		Addr:    loopbackAddr(),
		Token:   cfg.LoopbackToken,
		Origins: webOrigins(),
	})
}
