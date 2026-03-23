package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/trustgate/trustgate/internal/config"
)

// Server is the TrustGate Control Plane server.
type Server struct {
	cfg      *config.ServerConfig
	router   chi.Router
	store    *Store
	logger   zerolog.Logger
	sessions *sessionStore
}

// New creates a new Control Plane server.
func New(cfg *config.ServerConfig) (*Server, error) {
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

	// Auto-generate org API key if empty or placeholder
	if cfg.Auth.ApiKey == "" || cfg.Auth.ApiKey == "changeme" ||
		strings.HasSuffix(cfg.Auth.ApiKey, "_changeme_generate_a_real_key") {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("generate org api key: %w", err)
		}
		cfg.Auth.ApiKey = "tg_org_" + hex.EncodeToString(b)
		logger.Warn().
			Str("api_key", cfg.Auth.ApiKey).
			Msg("auto-generated org API key (api_key was empty or placeholder). Save this key to server.yaml auth.api_key")
	}

	// Open store
	store, err := NewStore(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	// Seed default data (departments, policies) if database is fresh
	if err := store.SeedDefaults(logger); err != nil {
		store.Close()
		return nil, fmt.Errorf("seed defaults: %w", err)
	}

	s := &Server{
		cfg:      cfg,
		store:    store,
		logger:   logger,
		sessions: newSessionStore(),
	}

	s.router = s.setupRoutes()

	return s, nil
}

func (s *Server) setupRoutes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Root redirect to UI
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})

	// Health check (no auth)
	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Token generation endpoint (authenticates with username/password, returns API token)
	r.Post("/api/v1/auth/token", s.handleAuthToken)

	// Agent auto-registration via org or department API key (separate auth from admin routes)
	if s.cfg.Auth.ApiKey != "" {
		r.Group(func(r chi.Router) {
			r.Use(apiKeyAuth(s.cfg.Auth.ApiKey, s.store))
			r.Post("/api/v1/agents/register", s.handleAgentAutoRegister)
		})
	}

	// Admin API routes (admin API token auth)
	r.Group(func(r chi.Router) {
		r.Use(adminApiAuth(s.store))

		// Agent management
		if s.cfg.Auth.ApiKey == "" {
			r.Post("/api/v1/agents/register", s.handleAgentRegister)
		}
		r.Get("/api/v1/agents", s.handleAgentList)

		// Policy management (admin)
		r.Get("/api/v1/policies", s.handlePolicyGet)
		r.Get("/api/v1/policies/version", s.handlePolicyVersionGet)
		r.Post("/api/v1/policies", s.handlePolicyCreate)

		// Department management (admin)
		r.Get("/api/v1/departments", s.handleDepartmentList)
		r.Post("/api/v1/departments", s.handleDepartmentCreate)
		r.Put("/api/v1/departments/{id}", s.handleDepartmentUpdate)
		r.Delete("/api/v1/departments/{id}", s.handleDepartmentDelete)

		// Stats summary (admin)
		r.Get("/api/v1/stats/summary", s.handleStatsSummary)

		// CSV report exports (admin)
		r.Get("/api/v1/reports/audit.csv", s.handleReportAuditCSV)
		r.Get("/api/v1/reports/summary.csv", s.handleReportSummaryCSV)
		r.Get("/api/v1/reports/agents.csv", s.handleReportAgentsCSV)
		r.Get("/api/v1/reports/risk-users.csv", s.handleReportRiskUsersCSV)

		// PDF report export (admin)
		r.Get("/api/v1/reports/summary.pdf", s.handleReportSummaryPDF)
	})

	// Agent API routes (agent_token auth)
	r.Group(func(r chi.Router) {
		r.Use(agentAuth(s.store))

		// Agent heartbeat
		r.Put("/api/v1/agents/{id}/heartbeat", s.handleAgentHeartbeat)

		// Policy pull (agent) - department-aware
		r.Get("/api/v1/agents/policies", s.handleAgentPolicyGet)
		r.Get("/api/v1/agents/policies/version", s.handlePolicyVersionGet)

		// Stats push (agent)
		r.Post("/api/v1/audit/stats", s.handleStatsPush)
	})

	// UI login/logout (no session required)
	r.Get("/ui/login", s.handleUILoginPage)
	r.Post("/ui/login", s.handleUILoginSubmit)
	r.Get("/ui/logout", s.handleUILogout)

	// MFA routes (requires token_verified cookie, checked in handlers)
	r.Get("/ui/mfa/setup", s.handleMFASetupPage)
	r.Post("/ui/mfa/setup", s.handleMFASetupSubmit)
	r.Get("/ui/mfa/verify", s.handleMFAVerifyPage)
	r.Post("/ui/mfa/verify", s.handleMFAVerifySubmit)

	// UI routes (session-based auth)
	r.Group(func(r chi.Router) {
		r.Use(sessionAuth(s.sessions))

		r.Get("/ui/", s.handleUIDashboard)
		r.Get("/ui/agents", s.handleUIAgents)
		r.Get("/ui/departments", s.handleUIDepartments)
		r.Get("/ui/policies", s.handleUIPolicies)
		r.Get("/ui/reports", s.handleUIReports)
		r.Get("/ui/admins", s.handleUIAdmins)

		// UI API proxy (session-authenticated, calls handlers directly)
		r.Post("/ui/api/policies", s.handlePolicyCreate)
		r.Get("/ui/api/departments", s.handleUIDepartmentsJSON)
		r.Post("/ui/api/departments", s.handleDepartmentCreate)
		r.Put("/ui/api/departments/{id}", s.handleDepartmentUpdate)
		r.Delete("/ui/api/departments/{id}", s.handleDepartmentDelete)

		// Agent management API
		r.Delete("/ui/api/agents/{id}", s.handleUIAgentDelete)

		// Admin management API
		r.Post("/ui/api/admins", s.handleUIAdminCreate)
		r.Delete("/ui/api/admins/{id}", s.handleUIAdminDelete)
		r.Put("/ui/api/admins/{id}/password", s.handleUIAdminChangePassword)

		// API token management
		r.Post("/ui/api/tokens", s.handleUITokenCreate)
		r.Delete("/ui/api/tokens/{hash}", s.handleUITokenRevoke)

		// CSV report downloads (session-auth, same handlers as API)
		r.Get("/ui/reports/audit.csv", s.handleReportAuditCSV)
		r.Get("/ui/reports/summary.csv", s.handleReportSummaryCSV)
		r.Get("/ui/reports/agents.csv", s.handleReportAgentsCSV)
		r.Get("/ui/reports/risk-users.csv", s.handleReportRiskUsersCSV)

		// PDF report download
		r.Get("/ui/reports/summary.pdf", s.handleReportSummaryPDF)
	})

	return r
}

// Run starts the HTTP server and blocks until interrupted.
func (s *Server) Run() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Listen.Host, s.cfg.Listen.Port)

	srv := &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	// Print startup banner
	fmt.Printf("TrustGate Control Plane\n")
	fmt.Printf("  Listen:   %s\n", addr)
	fmt.Printf("  Database: %s\n", s.cfg.Database.Path)
	fmt.Printf("  API:      /api/v1/agents, /api/v1/policies, /api/v1/audit/stats\n")
	fmt.Printf("  UI:       /ui/\n")
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*1e9) // 10 seconds
		defer cancel()
		s.store.Close()
		return srv.Shutdown(ctx)
	case err := <-errCh:
		s.store.Close()
		return err
	}
}
