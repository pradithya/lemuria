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
	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/auth"
	"github.com/org/lemuria/internal/commands"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/github"
	"github.com/org/lemuria/internal/gitlab"
	"github.com/org/lemuria/internal/lock"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/webhook"
)

// Server is the main HTTP server for Lemuria.
type Server struct {
	config     *config.Config
	router     *chi.Mux
	httpServer *http.Server
	logger     *slog.Logger

	// VCS providers and their handlers
	githubClient         *github.Client
	gitlabClient         *gitlab.Client
	githubWebhookHandler *webhook.GitHubHandler // GitHub webhook handler
	gitlabWebhookHandler *webhook.GitLabHandler // GitLab webhook handler
	argoClient           *argocd.Client
	lockManager          lock.Manager
	githubExecutor       *commands.Executor // executor using GitHub VCS client
	gitlabExecutor       *commands.Executor // executor using GitLab VCS client

	// Auth components (nil if auth disabled)
	redisClient         *redis.Client
	sessionStore        *auth.RedisSessionStore
	roleResolver        *auth.ConfigRoleResolver
	authMiddleware      *auth.Middleware
	githubOAuthProvider *auth.GitHubProvider
	gitlabOAuthProvider *auth.GitLabProvider
	oidcProvider        *auth.OIDCProvider
	basicAuthProvider   *auth.BasicProvider
}

// New creates a new Server instance.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
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

	s := &Server{
		config:      cfg,
		router:      chi.NewRouter(),
		logger:      logger,
		argoClient:  argoClient,
		lockManager: lockMgr,
	}

	// Initialize GitHub provider if configured
	if cfg.HasGitHub() {
		ghClient, err := github.NewClient(cfg.GitHub)
		if err != nil {
			return nil, fmt.Errorf("creating GitHub client: %w", err)
		}
		s.githubClient = ghClient
		s.githubExecutor = commands.NewExecutor(ghClient, argoClient, lockMgr, cfg, logger)
		s.githubWebhookHandler = webhook.NewGitHubHandler(cfg, ghClient, s.githubExecutor, logger)
		logger.Info("GitHub provider initialized")
	}

	// Initialize GitLab provider if configured
	if cfg.HasGitLab() {
		glClient, err := gitlab.NewClient(cfg.GitLab)
		if err != nil {
			return nil, fmt.Errorf("creating GitLab client: %w", err)
		}
		s.gitlabClient = glClient
		s.gitlabExecutor = commands.NewExecutor(glClient, argoClient, lockMgr, cfg, logger)
		s.gitlabWebhookHandler = webhook.NewGitLabHandler(cfg, glClient, s.gitlabExecutor, logger)
		logger.Info("GitLab provider initialized")
	}

	// Initialize auth components if enabled
	if cfg.AuthEnabled() {
		if err := s.setupAuth(cfg); err != nil {
			return nil, fmt.Errorf("setting up auth: %w", err)
		}
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

// setupAuth initializes authentication components.
func (s *Server) setupAuth(cfg *config.Config) error {
	// Create Redis client for sessions
	s.redisClient = redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connecting to Redis for sessions: %w", err)
	}

	// Create session store
	s.sessionStore = auth.NewRedisSessionStore(s.redisClient, cfg.Auth.SessionTTL)

	// Convert config role assignments to auth role assignments
	roleAssignments := make([]auth.RoleAssignment, len(cfg.Auth.RoleAssignments))
	for i, ra := range cfg.Auth.RoleAssignments {
		roleAssignments[i] = auth.RoleAssignment{
			Pattern:  ra.Pattern,
			Role:     ra.Role,
			Provider: ra.Provider,
		}
	}

	// Create role resolver
	defaultRole := models.Role(cfg.Auth.DefaultRole)
	if !defaultRole.Valid() {
		defaultRole = models.RoleUser
	}
	s.roleResolver = auth.NewConfigRoleResolver(roleAssignments, defaultRole, s.sessionStore)

	// Create auth middleware
	s.authMiddleware = auth.NewMiddleware(
		s.sessionStore,
		s.roleResolver,
		cfg.Auth.CookieDomain,
		cfg.Auth.CookieSecure,
		s.logger,
	)

	// Initialize GitHub OAuth provider if configured
	if cfg.HasGitHubOAuth() {
		s.githubOAuthProvider = auth.NewGitHubProvider(cfg.Auth.GitHub, cfg.Server.BaseURL)
		s.logger.Info("GitHub OAuth provider initialized")
	}

	// Initialize GitLab OAuth provider if configured
	if cfg.HasGitLabOAuth() {
		s.gitlabOAuthProvider = auth.NewGitLabProvider(cfg.Auth.GitLab, cfg.Server.BaseURL)
		s.logger.Info("GitLab OAuth provider initialized")
	}

	// Initialize OIDC provider if configured
	if cfg.HasOIDC() {
		oidcProvider, err := auth.NewOIDCProvider(context.Background(), cfg.Auth.OIDC, cfg.Server.BaseURL)
		if err != nil {
			return fmt.Errorf("initializing OIDC provider: %w", err)
		}
		s.oidcProvider = oidcProvider
		s.logger.Info("OIDC provider initialized", "name", cfg.Auth.OIDC.Name)
	}

	// Initialize basic auth provider if configured
	if cfg.HasBasicAuth() {
		s.basicAuthProvider = auth.NewBasicProvider(cfg.Auth.Basic)
		s.logger.Info("basic auth provider initialized", "users", len(cfg.Auth.Basic.Users))
	}

	s.logger.Info("authentication enabled")
	return nil
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
		// Skip logging for health check endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/healthz" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}

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
	// Cleanup any orphaned temporary applications from previous runs
	s.cleanupStaleTempApps()

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
	if s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			s.logger.Error("error closing auth redis client", "error", err)
		}
	}
	return nil
}

// cleanupStaleTempApps removes any orphaned temporary applications from previous runs.
func (s *Server) cleanupStaleTempApps() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tempMgr := argocd.NewTempAppManager(s.argoClient)
	deleted, err := tempMgr.CleanupStaleApps(ctx, 1*time.Hour)
	if err != nil {
		s.logger.Warn("failed to cleanup stale temp apps", "error", err)
		return
	}
	if deleted > 0 {
		s.logger.Info("cleaned up stale temp apps", "count", deleted)
	}
}
