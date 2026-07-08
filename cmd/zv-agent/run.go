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

	// Run-time truth wins: the proxy binds loopbackAddr() from the current env,
	// so the heartbeat must advertise that same port, not the port persisted at
	// pair time (which goes stale if FRAGFORGE_LOOPBACK_ADDR changes afterward).
	addr := loopbackAddr()
	port := portFromAddr(addr)
	go agent.HeartbeatLoop(ctx, c, capabilities, cfg.LoopbackToken, port, 20*time.Second)

	return agent.RunLoopback(ctx, agent.LoopbackConfig{
		Addr:    addr,
		Token:   cfg.LoopbackToken,
		Origins: webOrigins(),
		DataDir: childDataDir(),
	})
}
