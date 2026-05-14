package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reche/zackvideo/internal/httpapi"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
	"github.com/reche/zackvideo/internal/workers"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	store, err := storage.NewLocal(cfg.DataDir)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	repo := job.NewRepository(pool)
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr}
	client := asynq.NewClient(redisOpt)
	defer client.Close()

	handlers := httpapi.NewHandlers(repo, store, client)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Routes(handlers),
		ReadHeaderTimeout: 10 * time.Second,
	}

	worker := workers.NewParserWorker(repo, store)
	asynqSrv := asynq.NewServer(redisOpt, asynq.Config{Concurrency: cfg.WorkerConcurrency})
	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeParseDemo, worker.HandleParseDemo)

	// Start HTTP
	go func() {
		log.Printf("http: listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	// Start Asynq (blocks until ctx is cancelled)
	go func() {
		log.Printf("asynq: starting worker (concurrency=%d)", cfg.WorkerConcurrency)
		if err := asynqSrv.Run(mux); err != nil {
			log.Printf("asynq: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutdown: received signal, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	asynqSrv.Shutdown()
	log.Print("shutdown: done")
}
