package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/lock"
	"github.com/org/lemuria/internal/webhook"
)

// Server is the main HTTP server for Lemuria.
type Server struct {
	config        *config.Config
	router        *chi.Mux
	httpServer    *http.Server
	logger        *slog.Logger
	webhookHandler *webhook.Handler
	githubClient  *github.Client
	argoClient    *argocd.Client
	lockManager   lock.Manager
	cmdExecutor   *commands.Executor
}

// New creates a new Server instance.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	// Initialize GitHub client
	ghClient, err := github.NewClient(cfg.GitHub)
	if err != nil {
		return nil, fmt.Errorf("creating GitHub client: %w", err)
	}

	// Initialize Argo CD client
	argoClient, err := argocd.NewClient(cfg.ArgoCD)
	if err != nil {
		return nil, fmt.Errorf("creating Argo CD client: %w", err)
	}

	// Initialize lock manager
	lockMgr, err := lock.NewRedisManager(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("creating lock manager: %w", err)
	}

	// Initialize command executor
	cmdExecutor := commands.NewExecutor(ghClient, argoClient, lockMgr, cfg, logger)

	// Initialize webhook handler
	webhookHandler := webhook.NewHandler(cfg, ghClient, cmdExecutor, logger)

	s := &Server{
		config:        cfg,
		router:        chi.NewRouter(),
		logger:        logger,
		webhookHandler: webhookHandler,
		githubClient:  ghClient,
		argoClient:    argoClient,
		lockManager:   lockMgr,
		cmdExecutor:   cmdExecutor,
	}

	s.setupMiddleware()
	s.setupRoutes()

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s, nil
}

// setupMiddleware configures the middleware stack.
func (s *Server) setupMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(s.requestLogger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))
}

// requestLogger is a middleware that logs HTTP requests.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			s.logger.Info("request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start),
				"request_id", middleware.GetReqID(r.Context()),
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

// Run starts the server and blocks until shutdown.
func (s *Server) Run() error {
	// Channel to receive shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Channel to receive server errors
	serverErr := make(chan error, 1)

	// Start the server in a goroutine
	go func() {
		s.logger.Info("starting server",
			"host", s.config.Server.Host,
			"port", s.config.Server.Port,
		)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case sig := <-shutdown:
		s.logger.Info("shutdown signal received", "signal", sig)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.logger.Info("shutting down server")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	s.logger.Info("server stopped")
	return nil
}

// Close releases all resources held by the server.
func (s *Server) Close() error {
	if s.lockManager != nil {
		if err := s.lockManager.Close(); err != nil {
			s.logger.Error("error closing lock manager", "error", err)
		}
	}
	return nil
}
