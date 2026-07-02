package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rechedev9/fragforge/internal/httpapi"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/workers"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// Auto-detect HLAE/CS2/recorder/editor/ffmpeg on the host so capture and
	// rendering work without the user setting env vars; explicit env still wins.
	// Best-effort, never fatal.
	cfg, captureSource := detectCaptureTools(cfg)
	for _, name := range []string{"ZV_RECORDER_PATH", "ZV_HLAE_PATH", "ZV_CS2_PATH", "ZV_EDITOR_PATH", "ZV_FFMPEG_PATH", "ZV_FFPROBE_PATH"} {
		if captureSource[name] == "detected" {
			log.Printf("capture: auto-detected %s", name)
		}
	}
	log.Printf("capture: record worker enabled=%v", cfg.recordWorkerEnabled())
	log.Printf("capture: render worker enabled=%v", cfg.renderWorkerEnabled())
	if missing := cfg.missingRecordTools(); len(missing) > 0 {
		log.Printf("capture: configured record tool path(s) not found on disk: %v", missing)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewLocal(cfg.DataDir)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	var repo orchestratorJobRepository
	var streamRepo httpapi.StreamJobRepository
	switch {
	case cfg.DatabaseURL == databaseURLMemory:
		repo = newMemoryJobRepository()
		streamRepo = newMemoryStreamJobRepository()
		log.Printf("jobs: using in-memory repository (state resets on restart)")
	case cfg.DatabaseURL == databaseURLSQLite || strings.HasPrefix(cfg.DatabaseURL, databaseURLSQLite+":"):
		path := sqlitePath(cfg.DatabaseURL, cfg.DataDir)
		sqliteRepo, err := newSQLiteJobRepository(path)
		if err != nil {
			log.Fatalf("sqlite: %v", err)
		}
		defer func() { _ = sqliteRepo.Close() }()
		repo = sqliteRepo
		sqliteStreamRepo, err := newSQLiteStreamJobRepository(sqliteRepo.db)
		if err != nil {
			log.Fatalf("sqlite stream jobs: %v", err)
		}
		streamRepo = sqliteStreamRepo
		log.Printf("jobs: using sqlite repository at %s", path)
	default:
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
		repo = job.NewRepository(pool)
		streamRepo = streamclips.NewRepository(pool)
	}

	taskHandlers := map[string]taskHandler{}
	parserWorker := workers.NewParserWorker(repo, store)
	taskHandlers[tasks.TypeParseDemo] = parserWorker.HandleParseDemo
	taskHandlers[tasks.TypeScanRoster] = parserWorker.HandleScanRoster
	var recordWorker *workers.RecordWorker
	if cfg.recordWorkerEnabled() {
		recordWorker = workers.NewRecordWorker(repo, store, workers.RecordWorkerConfig{
			WorkDir:      cfg.MediaWorkDir,
			RecorderPath: cfg.RecorderPath,
			HLAEPath:     cfg.HLAEPath,
			CS2Path:      cfg.CS2Path,
			Timeout:      cfg.RecordTimeout,
			HUDMode:      cfg.RecordHUD,
		})
		taskHandlers[tasks.TypeRecordDemo] = recordWorker.HandleRecordDemo
		log.Printf("worker: record enabled")
	}
	if cfg.composeWorkerEnabled() {
		composeWorker := workers.NewComposeWorker(repo, store, workers.ComposeWorkerConfig{
			WorkDir:      cfg.MediaWorkDir,
			ComposerPath: cfg.ComposerPath,
			FFmpegPath:   cfg.FFmpegPath,
			Timeout:      cfg.ComposeTimeout,
		})
		taskHandlers[tasks.TypeComposeFinal] = composeWorker.HandleComposeFinal
		log.Printf("worker: compose enabled")
	}
	if cfg.renderWorkerEnabled() {
		renderWorker := workers.NewRenderWorker(repo, store, workers.RenderWorkerConfig{
			WorkDir:     cfg.MediaWorkDir,
			EditorPath:  cfg.EditorPath,
			FFmpegPath:  cfg.FFmpegPath,
			FFprobePath: cfg.FFprobePath,
			Timeout:     cfg.RenderTimeout,
			MusicDir:    cfg.MusicDir,
		})
		taskHandlers[tasks.TypeRenderVariant] = renderWorker.HandleRenderVariant
		log.Printf("worker: render enabled")
	}
	if cfg.streamRenderWorkerEnabled() && streamRepo != nil {
		streamWorker := workers.NewStreamRenderWorker(streamRepo, store, workers.StreamRenderWorkerConfig{
			WorkDir:          cfg.MediaWorkDir,
			FFmpegPath:       cfg.FFmpegPath,
			Timeout:          cfg.RenderTimeout,
			WhisperPath:      cfg.WhisperPath,
			WhisperModelPath: cfg.WhisperModelPath,
			GroqAPIKey:       cfg.GroqAPIKey,
			GroqModel:        cfg.GroqModel,
		})
		taskHandlers[tasks.TypeRenderStreamClip] = streamWorker.HandleRenderStreamClip
		log.Printf("worker: stream render enabled")
	}
	if cfg.streamAcquireWorkerEnabled() && streamRepo != nil {
		acquireWorker := workers.NewAcquireWorker(streamRepo, store, workers.AcquireWorkerConfig{
			WorkDir:     cfg.MediaWorkDir,
			YtdlpPath:   cfg.YtdlpPath,
			FFprobePath: cfg.FFprobePath,
			Timeout:     cfg.RenderTimeout,
		})
		taskHandlers[tasks.TypeStreamAcquire] = acquireWorker.HandleStreamAcquire
		log.Printf("worker: stream acquire enabled")
	}
	if cfg.agentWorkerEnabled() {
		agentWorker := workers.NewAgentWorker(store, workers.AgentWorkerConfig{
			WorkDir:   cfg.MediaWorkDir,
			CodexPath: cfg.CodexPath,
			Model:     cfg.CodexModel,
			Timeout:   cfg.AgentTimeout,
		})
		taskHandlers[tasks.TypeCodexAgent] = agentWorker.HandleCodexAgent
		log.Printf("worker: codex agent enabled")
	}

	var queue httpapi.Enqueuer
	var asynqSrv *asynq.Server
	var inline *inlineQueue
	if cfg.QueueMode == queueModeInline {
		inline = newInlineQueue(taskHandlers, cfg.WorkerConcurrency)
		queue = inline
		// Wire the chaining queue before processing starts so the record worker
		// never handles a task with a half-set enqueuer.
		if recordWorker != nil {
			recordWorker.UseEnqueuer(queue)
		}
		inline.Start(ctx)
		log.Printf("queue: inline mode enabled (concurrency=%d)", cfg.WorkerConcurrency)
	} else {
		redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr}
		client := asynq.NewClient(redisOpt)
		defer client.Close()
		queue = client
		// Wire the chaining queue before the asynq server starts consuming.
		if recordWorker != nil {
			recordWorker.UseEnqueuer(queue)
		}

		asynqSrv = asynq.NewServer(redisOpt, asynq.Config{Concurrency: cfg.WorkerConcurrency})
		mux := asynq.NewServeMux()
		for taskType, handler := range taskHandlers {
			mux.HandleFunc(taskType, handler)
		}
		go func() {
			log.Printf("asynq: starting worker (concurrency=%d)", cfg.WorkerConcurrency)
			if err := asynqSrv.Run(mux); err != nil {
				log.Printf("asynq: %v", err)
			}
		}()
	}

	// Defense-in-depth gating only kicks in on an exposed (non-loopback) bind;
	// the loopback default stays unauthenticated and unthrottled for the local
	// UI and e2e.
	exposed := !isLoopbackHTTPAddr(cfg.HTTPAddr)
	rateLimitRPS := 0.0
	rateLimitBurst := 0
	if exposed {
		rateLimitRPS = 20
		rateLimitBurst = 40
	}
	handlers := httpapi.NewHandlers(repo, store, queue,
		httpapi.WithMutationToken(cfg.MutationToken),
		httpapi.WithRequireReadAuth(exposed),
		httpapi.WithRateLimit(rateLimitRPS, rateLimitBurst),
		httpapi.WithStreamRepository(streamRepo),
		httpapi.WithStreamProber(streamclips.FFprobeProber{Path: cfg.FFprobePath}),
		httpapi.WithMusicDir(cfg.MusicDir),
		httpapi.WithCapabilities(cfg.captureCapabilities(captureSource)),
	)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Routes(handlers),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start HTTP
	go func() {
		log.Printf("http: listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutdown: received signal, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if asynqSrv != nil {
		asynqSrv.Shutdown()
	}
	if inline != nil {
		inline.Shutdown(shutdownCtx)
	}
	log.Print("shutdown: done")
}
