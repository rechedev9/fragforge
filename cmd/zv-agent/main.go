// Command zv-agent is the FragForge capture agent that runs on the user's PC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/rechedev9/fragforge/internal/agent"
)

func main() {
	baseURL := flag.String("cloud", envOr("FRAGFORGE_CLOUD_URL", "https://app.fragforge.gg"), "cloud base URL")
	pairCode := flag.String("pair", "", "pairing code from the web app")
	name := flag.String("name", hostname(), "agent display name")
	flag.Parse()

	ctx := context.Background()

	if *pairCode != "" {
		token, id, err := agent.Pair(ctx, *baseURL, *pairCode, *name)
		if err != nil {
			log.Fatalf("pair: %v", err)
		}
		if err := saveConfig(Config{BaseURL: *baseURL, Token: token, AgentID: id}); err != nil {
			log.Fatalf("save config: %v", err)
		}
		fmt.Println("paired. run zv-agent with no flags to start working.")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("not paired yet: run zv-agent --pair <code> first (%v)", err)
	}
	if err := run(ctx, cfg); err != nil {
		log.Fatalf("agent: %v", err)
	}
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
