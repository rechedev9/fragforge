package main

import (
	"fmt"
	"os"
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
		HTTPAddr:          envOr("ZV_HTTP_ADDR", ":8080"),
		DatabaseURL:       os.Getenv("ZV_DATABASE_URL"),
		RedisAddr:         envOr("ZV_REDIS_ADDR", "localhost:6379"),
		DataDir:           envOr("ZV_DATA_DIR", "./data"),
		WorkerConcurrency: 2,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
