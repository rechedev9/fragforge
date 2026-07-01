package main

import (
	"context"

	"github.com/rechedev9/fragforge/internal/agent"
)

func run(ctx context.Context, cfg Config) error {
	return agent.Run(ctx, agent.NewClient(cfg.BaseURL, cfg.Token))
}
