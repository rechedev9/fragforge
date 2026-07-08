// Command zv-agent is the FragForge capture agent that runs on the user's PC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rechedev9/fragforge/internal/agent"
)

func main() {
	baseURL := flag.String("cloud", envOr("FRAGFORGE_CLOUD_URL", "https://app.fragforge.gg"), "cloud base URL")
	pairCode := flag.String("pair", "", "pairing code from the web app")
	name := flag.String("name", hostname(), "agent display name")
	flag.Parse()

	if *pairCode != "" {
		if err := pair(*baseURL, *pairCode, *name); err != nil {
			log.Fatalf("pair: %v", err)
		}
		fmt.Println("paired. run zv-agent with no flags to serve the local data plane.")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("not paired yet: run zv-agent --pair <code> first (%v)", err)
	}

	// Cancel on SIGINT/SIGTERM so the supervised child orchestrator is torn down.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil && ctx.Err() == nil {
		log.Fatalf("agent: %v", err)
	}
}

// pair generates the loopback credential, registers it with the control plane,
// and persists the full config (cloud token plus loopback token and port).
func pair(baseURL, code, name string) error {
	loopbackToken, err := agent.GenerateLoopbackToken()
	if err != nil {
		return err
	}
	port := loopbackPort()
	token, id, err := agent.Pair(context.Background(), baseURL, code, name, loopbackToken, port)
	if err != nil {
		return err
	}
	return saveConfig(Config{
		BaseURL:       baseURL,
		Token:         token,
		AgentID:       id,
		LoopbackToken: loopbackToken,
		LoopbackPort:  port,
	})
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "PC"
	}
	return h
}
