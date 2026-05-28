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

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres config: %v", err)
	}
	// The pool is shared by the Asynq workers (each in-flight task can hold a
	// connection) and the HTTP server. Size it for worker concurrency plus
	// request headroom so tasks and requests do not block acquiring a connection,
	// and keep a couple of warm connections to avoid cold-start latency.
	const httpConnHeadroom = 8
	if want := int32(cfg.WorkerConcurrency + httpConnHeadroom); want > poolCfg.MaxConns {
		poolCfg.MaxConns = want
	}
	poolCfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(pingCtx)
	cancelPing()
	if err != nil {
		log.Fatalf("postgres ping: %v", err)
	}

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
	if cfg.recordWorkerEnabled() {
		recordWorker := workers.NewRecordWorker(repo, store, workers.RecordWorkerConfig{
			WorkDir:      cfg.MediaWorkDir,
			RecorderPath: cfg.RecorderPath,
			HLAEPath:     cfg.HLAEPath,
			CS2Path:      cfg.CS2Path,
			Timeout:      cfg.RecordTimeout,
		})
		mux.HandleFunc(tasks.TypeRecordDemo, recordWorker.HandleRecordDemo)
		log.Printf("asynq: record worker enabled")
	}
	if cfg.composeWorkerEnabled() {
		composeWorker := workers.NewComposeWorker(repo, store, workers.ComposeWorkerConfig{
			WorkDir:      cfg.MediaWorkDir,
			ComposerPath: cfg.ComposerPath,
			FFmpegPath:   cfg.FFmpegPath,
			Timeout:      cfg.ComposeTimeout,
		})
		mux.HandleFunc(tasks.TypeComposeFinal, composeWorker.HandleComposeFinal)
		log.Printf("asynq: compose worker enabled")
	}

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
