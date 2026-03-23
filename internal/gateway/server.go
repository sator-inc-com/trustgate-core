package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/trustgate/trustgate/internal/adapter"
	"github.com/trustgate/trustgate/internal/audit"
	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/detector"
	"github.com/trustgate/trustgate/internal/identity"
	"github.com/trustgate/trustgate/internal/inspector"
	"github.com/trustgate/trustgate/internal/policy"
)

// StatsRecorder records events for stats aggregation (sync client).
type StatsRecorder interface {
	RecordEvent(action, detector, userID, policyName string)
}

// Server is the main TrustGate gateway server.
type Server struct {
	cfg             *config.Config
	router          chi.Router
	registry        *detector.Registry
	resolver        *identity.Resolver
	evaluator       *policy.Evaluator
	auditStore      audit.Logger
	adapter         adapter.Adapter
	inspectionQueue *inspector.Queue
	statsRecorder   StatsRecorder
	logger          zerolog.Logger
}

// SetStatsRecorder sets the stats recorder (sync client) for pushing stats to CP.
func (s *Server) SetStatsRecorder(sr StatsRecorder) {
	s.statsRecorder = sr
}

// New creates a new gateway server.
func New(cfg *config.Config) (*Server, error) {
	// Setup logger
	var logger zerolog.Logger
	if cfg.Logging.Format == "text" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	} else {
		logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}

	level, err := zerolog.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	logger = logger.Level(level)
	log.Logger = logger

	// Create detector registry
	registry := detector.NewRegistry(cfg.Detectors)

	// Initialize LLM detector (Stage 2) if enabled
	if cfg.Detectors.LLM.Enabled {
		pg := detector.NewPromptGuardDetector(cfg.Detectors.LLM)
		if err := pg.LoadModel(); err != nil {
			logger.Warn().Err(err).Msg("LLM detector not available — Stage 2 disabled, running Stage 1 (regex) only")
		} else {
			registry.SetLLMDetector(pg)
			logger.Info().Str("model", cfg.Detectors.LLM.Model).Msg("LLM detector loaded (Stage 2 enabled)")
		}
	}

	// Create identity resolver
	resolver := identity.NewResolver(cfg.Identity)

	// Load policies
	var evaluator *policy.Evaluator
	if cfg.Policy.Source == "local" && cfg.Policy.File != "" {
		policies, err := policy.LoadPolicies(cfg.Policy.File)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("load policies: %w", err)
			}
			logger.Warn().Msg("no policies file found, using empty policy set")
			evaluator = policy.NewEvaluator(nil)
		} else {
			evaluator = policy.NewEvaluator(policies)
			logger.Info().Int("count", len(policies)).Msg("policies loaded")
		}
	} else {
		evaluator = policy.NewEvaluator(nil)
	}

	// Open audit log store based on mode
	var auditStore audit.Logger
	if cfg.Mode == "standalone" || cfg.Mode == "" {
		// Standalone: in-memory ring buffer (no disk I/O, last 100 records)
		auditStore = audit.NewMemoryLogger(100)
		logger.Info().Msg("audit store: in-memory (standalone, last 100 records)")
	} else {
		// Managed: WAL (JSONLines with hash chain) for CP flush
		auditDir := cfg.Audit.Path
		if auditDir == "" {
			auditDir = "audit_data"
		}
		wal, err := audit.NewWALWriter(auditDir, logger)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to open WAL audit store, falling back to in-memory")
			auditStore = audit.NewMemoryLogger(100)
		} else {
			auditStore = wal
			count, _ := wal.Count()
			valid, verr := wal.VerifyChain()
			logger.Info().
				Str("dir", auditDir).
				Int("buffered", count).
				Int("chain_valid", valid).
				Err(verr).
				Msg("audit WAL opened (managed, hash chain)")

			// Daily compaction goroutine
			go func() {
				ticker := time.NewTicker(24 * time.Hour)
				defer ticker.Stop()
				for range ticker.C {
					if err := wal.Compact(); err != nil {
						logger.Warn().Err(err).Msg("WAL compaction failed")
					}
				}
			}()
		}
	}

	// Create LLM backend adapter
	var llmAdapter adapter.Adapter
	switch cfg.Backend.Provider {
	case "bedrock":
		logger.Info().Str("region", cfg.Backend.Bedrock.Region).Msg("Bedrock adapter not yet available — use --mock-backend")
	case "mock":
		llmAdapter = adapter.NewMockAdapter(cfg.Backend.Mock)
		logger.Info().Int("delay_ms", cfg.Backend.Mock.DelayMs).Msg("mock backend enabled")
	default:
		logger.Warn().Str("provider", cfg.Backend.Provider).Msg("unknown backend provider, /v1/chat/completions will be unavailable")
	}

	// Create content inspection queue (if enabled)
	var inspQueue *inspector.Queue
	if cfg.ContentInspection.Enabled {
		inspQueue = inspector.NewQueue(cfg.ContentInspection, registry)
		logger.Info().Msg("content inspection enabled (async, opt-in)")
	}

	s := &Server{
		cfg:             cfg,
		router:          nil,
		registry:        registry,
		resolver:        resolver,
		evaluator:       evaluator,
		auditStore:      auditStore,
		adapter:         llmAdapter,
		inspectionQueue: inspQueue,
		logger:          logger,
	}

	s.router = s.setupRoutes()

	return s, nil
}

func (s *Server) setupRoutes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(corsMiddleware)

	r.Get("/v1/health", s.handleHealth)
	r.Post("/v1/inspect", s.handleInspect)
	r.Post("/v1/chat/completions", s.handleChatCompletions)

	// Content inspection (async file upload)
	r.Post("/v1/inspect/file", s.handleInspectFile)
	r.Get("/v1/inspect/file/{id}", s.handleInspectFileResult)

	// Serve test page
	fs := http.FileServer(http.Dir("extension"))
	r.Handle("/test/*", http.StripPrefix("/test/", fs))

	return r
}

// corsMiddleware allows requests from browser extensions and localhost.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Allow chrome extensions and localhost
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-TrustGate-User, X-TrustGate-Role, X-TrustGate-Department, X-TrustGate-Clearance, X-TrustGate-Debug")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Evaluator returns the policy evaluator for external callers (e.g., sync client).
func (s *Server) Evaluator() *policy.Evaluator {
	return s.evaluator
}

// AuditWAL returns the WAL writer if the audit store is a WALWriter (managed mode).
// Returns nil if the audit store is not a WALWriter (standalone mode).
func (s *Server) AuditWAL() *audit.WALWriter {
	if wal, ok := s.auditStore.(*audit.WALWriter); ok {
		return wal
	}
	return nil
}

// Run starts the HTTP server and blocks until interrupted.
func (s *Server) Run() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Listen.Host, s.cfg.Listen.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.cfg.Listen.ReadTimeout,
		WriteTimeout: s.cfg.Listen.WriteTimeout,
	}

	// Print startup banner
	fmt.Printf("TrustGate Agent\n")
	fmt.Printf("  Listen:     %s\n", addr)
	fmt.Printf("  Mode:       %s\n", s.cfg.Mode)
	fmt.Printf("  Detectors:  %d loaded (Stage 1: regex)\n", len(s.registry.Detectors()))
	if s.registry.LLMReady() {
		fmt.Printf("  LLM:        Stage 2 enabled (Prompt Guard 2)\n")
	} else {
		fmt.Printf("  LLM:        Stage 2 disabled\n")
	}
	fmt.Printf("  Backend:    %s\n", s.cfg.Backend.Provider)
	fmt.Printf("  Endpoints:  /v1/inspect, /v1/chat/completions\n")
	fmt.Printf("\nReady. Accepting connections.\n\n")

	// Graceful shutdown
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		s.logger.Info().Str("signal", sig.String()).Msg("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Listen.WriteTimeout)
		defer cancel()
		return srv.Shutdown(ctx)
	case err := <-errCh:
		return err
	}
}
