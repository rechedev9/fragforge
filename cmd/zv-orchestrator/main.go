package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rechedev9/fragforge/internal/generateintent"
	"github.com/rechedev9/fragforge/internal/httpapi"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/workers"
	"github.com/rechedev9/fragforge/internal/youtubetrends"
)

type orchestratorStreamJobRepository interface {
	httpapi.StreamJobRepository
	streamInterruptSweeper
}

const gracefulShutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := clearXAIAPIKeyEnvironment(); err != nil {
		return fmt.Errorf("config: clear xai credential from process environment: %w", err)
	}
	if err := clearGroqAPIKeyEnvironment(); err != nil {
		return fmt.Errorf("config: clear groq credential from process environment: %w", err)
	}
	if err := clearDiscoverySecretEnvironment(); err != nil {
		return fmt.Errorf("config: clear discovery credential from process environment: %w", err)
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
	if missing := cfg.missingRecordConfig(); len(missing) > 0 {
		log.Printf("capture: record worker disabled, missing after auto-detection: %v", missing)
	}
	if missing := cfg.missingRecordTools(); len(missing) > 0 {
		log.Printf("capture: configured record tool path(s) not found on disk: %v", missing)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewLocal(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	generateIntents := generateintent.New(store)
	youtubeTrends, err := youtubetrends.New(youtubetrends.Options{APIKey: cfg.FirecrawlAPIKey})
	if err != nil {
		return fmt.Errorf("youtube trends client: %w", err)
	}
	log.Printf("publish assistant: firecrawl trends enabled=%v", cfg.firecrawlEnabled())

	var repo orchestratorJobRepository
	var streamRepo orchestratorStreamJobRepository
	switch {
	case cfg.DatabaseURL == databaseURLMemory:
		repo = newMemoryJobRepository()
		streamRepo = newMemoryStreamJobRepository()
		log.Printf("jobs: using in-memory repository (state resets on restart)")
	case cfg.DatabaseURL == databaseURLSQLite || strings.HasPrefix(cfg.DatabaseURL, databaseURLSQLite+":"):
		path := sqlitePath(cfg.DatabaseURL, cfg.DataDir)
		sqliteRepo, err := newSQLiteJobRepository(path)
		if err != nil {
			return fmt.Errorf("sqlite: %w", err)
		}
		defer func() { _ = sqliteRepo.Close() }()
		repo = sqliteRepo
		sqliteStreamRepo, err := newSQLiteStreamJobRepository(sqliteRepo.db)
		if err != nil {
			return fmt.Errorf("sqlite stream jobs: %w", err)
		}
		streamRepo = sqliteStreamRepo
		log.Printf("jobs: using sqlite repository at %s", path)
	default:
		return fmt.Errorf("unsupported ZV_DATABASE_URL %q: fragforge desktop only supports %q or %q", cfg.DatabaseURL, databaseURLMemory, databaseURLSQLite)
	}

	// Reconcile durable state whose process-local work vanished with the previous
	// desktop process. Run every sweep before serving traffic so clients never
	// observe an active state with no queue owner capable of advancing it.
	reconciled, err := reconcileInterruptedWork(ctx, repo, streamRepo, store, obs.Default())
	if err != nil {
		return fmt.Errorf("startup reconciliation: %w", err)
	}
	if reconciled.total() > 0 {
		log.Printf(
			"startup: reconciled interrupted work (demo_jobs=%d demo_renders=%d generate_runs=%d stream_jobs=%d stream_renders=%d)",
			reconciled.DemoJobs,
			reconciled.DemoRenders,
			reconciled.GenerateRuns,
			reconciled.StreamJobs,
			reconciled.StreamRenderStates,
		)
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
			WorkDir:             cfg.MediaWorkDir,
			FFmpegPath:          cfg.FFmpegPath,
			Timeout:             cfg.RenderTimeout,
			MusicDir:            cfg.MusicDir,
			WhisperPath:         cfg.WhisperPath,
			WhisperModelPath:    cfg.WhisperModelPath,
			XAIAPIKey:           cfg.XAIAPIKey,
			GroqAPIKey:          cfg.GroqAPIKey,
			GroqModel:           cfg.GroqModel,
			GroqCorrectionModel: cfg.GroqCorrectionModel,
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
	inline := newInlineQueue(taskHandlers, cfg.WorkerConcurrency)
	queue = inline
	// Wire the chaining queue before processing starts so the record worker
	// never handles a task with a half-set enqueuer.
	if recordWorker != nil {
		recordWorker.UseGenerateIntentStore(generateIntents)
		recordWorker.UseEnqueuer(queue)
	}
	// Defense-in-depth gating only kicks in on an exposed (non-loopback) bind;
	// the loopback default stays unauthenticated and unthrottled for the local
	// UI and e2e.
	exposed := !httpapi.IsLoopbackAddr(cfg.HTTPAddr)
	rateLimitRPS := 0.0
	rateLimitBurst := 0
	if exposed {
		rateLimitRPS = 20
		rateLimitBurst = 40
	}
	handlers := httpapi.NewHandlers(repo, store, queue,
		httpapi.WithMutationToken(cfg.MutationToken),
		httpapi.WithDiscoverySecret(cfg.DiscoverySecret),
		httpapi.WithRequireReadAuth(exposed),
		httpapi.WithRateLimit(rateLimitRPS, rateLimitBurst),
		httpapi.WithStreamRepository(streamRepo),
		httpapi.WithStreamProber(streamclips.FFprobeProber{Path: cfg.FFprobePath}),
		httpapi.WithFFmpegPath(cfg.FFmpegPath),
		httpapi.WithXAIKey(cfg.XAIAPIKey),
		httpapi.WithMusicDir(cfg.MusicDir),
		httpapi.WithCapabilities(cfg.captureCapabilities(captureSource)),
		httpapi.WithGenerateIntentStore(generateIntents),
		httpapi.WithPublishAssistantTrends(youtubeTrends),
	)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Routes(handlers),
		ReadHeaderTimeout: 10 * time.Second,
	}
	httpRuntime, err := prepareHTTPServer(srv)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}

	// The address is reserved now, so workers cannot start behind a server that
	// already failed to bind.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	inline.Start(workerCtx)
	log.Printf("queue: inline mode enabled (concurrency=%d)", cfg.WorkerConcurrency)
	httpRuntime.Start()
	log.Printf("http: listening on %s", httpRuntime.Addr())

	serveErr := waitAndCancelOnHTTPFailure(ctx, stop, httpRuntime)
	if serveErr != nil {
		log.Printf("shutdown: http server failed, draining: %v", serveErr)
	} else {
		log.Print("shutdown: received signal, draining")
	}

	// Stop accepting mutations before canceling workers. Any request already in
	// flight can finish its atomic admission while the queue is still live;
	// shutdown then compensates accepted work that remains pending.
	httpShutdownCtx, cancelHTTPShutdown := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	if err := httpRuntime.Shutdown(httpShutdownCtx); err != nil {
		log.Printf("shutdown: HTTP drain failed: %v", err)
	}
	cancelHTTPShutdown()

	cancelWorkers()
	queueShutdownCtx, cancelQueueShutdown := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	if err := inline.Shutdown(queueShutdownCtx); err != nil {
		log.Printf("shutdown: queue drain failed: %v", err)
	}
	cancelQueueShutdown()
	log.Print("shutdown: done")
	if serveErr != nil {
		return fmt.Errorf("http: %w", serveErr)
	}
	return nil
}
