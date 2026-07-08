package agent

import (
	"context"
	"time"
)

// Heartbeat sends one liveness ping with the agent's current capabilities and
// its loopback registration, so a re-paired or re-configured agent propagates
// the fresh token and port to the control plane on every tick.
func Heartbeat(ctx context.Context, c *Client, capabilities map[string]any, loopbackToken string, loopbackPort int) error {
	body := map[string]any{
		"capabilities":  capabilities,
		"loopbackToken": loopbackToken,
		"loopbackPort":  loopbackPort,
	}
	_, err := c.Do(ctx, "POST", "/api/agent/heartbeat", body, nil)
	return err
}

// HeartbeatLoop pings every interval until ctx is cancelled.
func HeartbeatLoop(ctx context.Context, c *Client, capabilities map[string]any, loopbackToken string, loopbackPort int, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	// Heartbeats are fire-and-forget liveness pings: a dropped one just makes the
	// agent look briefly offline until the next tick, so the error is intentionally
	// ignored rather than retried or surfaced from this background loop.
	_ = Heartbeat(ctx, c, capabilities, loopbackToken, loopbackPort)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = Heartbeat(ctx, c, capabilities, loopbackToken, loopbackPort)
		}
	}
}
