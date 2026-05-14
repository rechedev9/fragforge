package main

import (
	"fmt"
	"os"
	"strconv"
)

type config struct {
	HTTPAddr          string
	DatabaseURL       string
	RedisAddr         string
	DataDir           string
	WorkerConcurrency int
}

func loadConfig() (config, error) {
	c := config{
		HTTPAddr:    envOr("ZV_HTTP_ADDR", ":8080"),
		DatabaseURL: os.Getenv("ZV_DATABASE_URL"),
		RedisAddr:   envOr("ZV_REDIS_ADDR", "localhost:6379"),
		DataDir:     envOr("ZV_DATA_DIR", "./data"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}

	concRaw := envOr("ZV_WORKER_CONCURRENCY", "2")
	conc, err := strconv.Atoi(concRaw)
	if err != nil || conc < 1 {
		return c, fmt.Errorf("ZV_WORKER_CONCURRENCY must be a positive integer, got %q", concRaw)
	}
	c.WorkerConcurrency = conc

	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
