// Copyright 2026 Lemuria Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/org/lemuria/internal/auth"
	"github.com/org/lemuria/internal/models"
)

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes() {
	// Health check endpoints (always public)
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/healthz", s.handleHealth)
	s.router.Get("/ready", s.handleReady)

	// Webhook endpoints (validated by signature/token, not session)
	if s.githubWebhookHandler != nil {
		s.router.Post("/webhook/github", s.githubWebhookHandler.Handle)
	}
	if s.gitlabWebhookHandler != nil {
		s.router.Post("/webhook/gitlab", s.gitlabWebhookHandler.Handle)
	}

	// Auth endpoints
	s.router.Route("/auth", func(r chi.Router) {
		// Always expose providers endpoint (returns empty if auth disabled)
		r.Get("/providers", s.handleAuthProviders)

		// Only register OAuth routes if auth is enabled
		if s.authMiddleware != nil {
			// GitHub OAuth
			if s.githubOAuthProvider != nil {
				r.Get("/github/login", s.handleGitHubLogin)
				r.Get("/github/callback", s.handleGitHubCallback)
			}

			// GitLab OAuth
			if s.gitlabOAuthProvider != nil {
				r.Get("/gitlab/login", s.handleGitLabLogin)
				r.Get("/gitlab/callback", s.handleGitLabCallback)
			}

			// OIDC
			if s.oidcProvider != nil {
				r.Get("/oidc/login", s.handleOIDCLogin)
				r.Get("/oidc/callback", s.handleOIDCCallback)
			}

			// Basic auth
			if s.basicAuthProvider != nil {
				r.Post("/basic/login", s.handleBasicLogin)
			}

			// Protected auth endpoints
			r.Group(func(r chi.Router) {
				r.Use(s.authMiddleware.Authenticate)
				r.Use(s.authMiddleware.RequireAuth)
				r.Post("/logout", s.handleLogout)
				r.Get("/me", s.handleMe)
			})
		}
	})

	// API endpoints
	s.router.Route("/api/v1", func(r chi.Router) {
		// Apply auth middleware if enabled
		if s.authMiddleware != nil {
			r.Use(s.authMiddleware.Authenticate)
			r.Use(s.authMiddleware.RequireAuth)

			// VULN-07: Require X-Requested-With header on state-changing requests
			// to provide CSRF protection beyond SameSite=Lax cookies.
			// Only needed when cookie-based auth is enabled.
			r.Use(requireCSRFHeader)
		}

		r.Get("/status", s.handleStatus)
		r.Get("/locks", s.handleListLocks)

		// Admin-only endpoints
		r.Group(func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware.RequireAdmin)
			}
			r.Delete("/locks/{app}", s.handleDeleteLock)
			r.Get("/users", s.handleListUsers)
			r.Put("/users/{id}/role", s.handleUpdateUserRole)
		})
	})

	// Serve static frontend files (if auth enabled).
	// VULN-06: Static files (JS, CSS, index.html) are served without authentication.
	// This is intentional: the SPA must load to render the login page. All sensitive
	// data is served exclusively through /api/v1 endpoints which are auth-protected.
	// The frontend shows a login form for unauthenticated users and only fetches data
	// after successful authentication.
	if s.authMiddleware != nil {
		s.setupStaticFiles()
	}
}

// handleHealth responds to health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// handleReady responds to readiness check requests.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check Redis connectivity
	if err := s.lockManager.Ping(r.Context()); err != nil {
		s.logger.Error("readiness check failed", "error", err)
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"error":  "redis connection failed",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status": "ready",
	})
}

// handleStatus returns the current server status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"status":  "running",
		"version": "0.1.0",
	})
}

// handleListLocks returns all current locks.
func (s *Server) handleListLocks(w http.ResponseWriter, r *http.Request) {
	locks, err := s.lockManager.ListAll(r.Context())
	if err != nil {
		s.logger.Error("failed to list locks", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to list locks",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"locks": locks,
		"count": len(locks),
	})
}

// handleDeleteLock forcibly removes a lock.
func (s *Server) handleDeleteLock(w http.ResponseWriter, r *http.Request) {
	app := chi.URLParam(r, "app")
	if app == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "application name required",
		})
		return
	}

	if err := s.lockManager.ForceUnlock(r.Context(), app); err != nil {
		s.logger.Error("failed to delete lock", "app", app, "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to delete lock",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":      "unlocked",
		"application": app,
	})
}

// respondJSON writes a JSON response.
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Warn("failed to encode JSON response", "error", err)
	}
}

// ============================================================================
// Auth Handlers
// ============================================================================

// handleAuthProviders returns available authentication providers.
func (s *Server) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	var providers []models.AuthProvider

	if s.githubOAuthProvider != nil {
		providers = append(providers, models.AuthProvider{
			ID:       s.githubOAuthProvider.Name(),
			Name:     s.githubOAuthProvider.DisplayName(),
			LoginURL: "/auth/github/login",
		})
	}

	if s.gitlabOAuthProvider != nil {
		providers = append(providers, models.AuthProvider{
			ID:       s.gitlabOAuthProvider.Name(),
			Name:     s.gitlabOAuthProvider.DisplayName(),
			LoginURL: "/auth/gitlab/login",
		})
	}

	if s.oidcProvider != nil {
		providers = append(providers, models.AuthProvider{
			ID:       s.oidcProvider.Name(),
			Name:     s.oidcProvider.DisplayName(),
			LoginURL: "/auth/oidc/login",
		})
	}

	if s.basicAuthProvider != nil {
		providers = append(providers, models.AuthProvider{
			ID:       s.basicAuthProvider.Name(),
			Name:     s.basicAuthProvider.DisplayName(),
			LoginURL: "/auth/basic/login",
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"providers":    providers,
		"auth_enabled": s.authMiddleware != nil,
	})
}

// handleGitHubLogin initiates GitHub OAuth flow.
func (s *Server) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" {
		redirectURL = "/"
	}

	state, err := s.sessionStore.CreateState(r.Context(), redirectURL)
	if err != nil {
		s.logger.Error("failed to create OAuth state", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to initiate login",
		})
		return
	}

	authURL := s.githubOAuthProvider.AuthURL(state.State)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleGitHubCallback handles GitHub OAuth callback.
func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateParam := r.URL.Query().Get("state")

	if code == "" {
		s.logger.Error("GitHub callback missing code")
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing authorization code",
		})
		return
	}

	// Validate state
	state, err := s.sessionStore.ValidateState(r.Context(), stateParam)
	if err != nil {
		s.logger.Error("invalid OAuth state", "error", err)
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid or expired state",
		})
		return
	}

	// Exchange code for user
	user, err := s.githubOAuthProvider.Exchange(r.Context(), code)
	if err != nil {
		s.logger.Error("GitHub OAuth exchange failed", "error", err)
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "authentication failed: " + err.Error(),
		})
		return
	}

	// Create session
	_, err = s.authMiddleware.CreateSession(r.Context(), w, user)
	if err != nil {
		s.logger.Error("failed to create session", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to create session",
		})
		return
	}

	s.logger.Info("user logged in via GitHub", "user", user.Login, "email", user.Email)

	// Redirect to original URL
	redirectURL := state.RedirectURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// handleGitLabLogin initiates GitLab OAuth flow.
func (s *Server) handleGitLabLogin(w http.ResponseWriter, r *http.Request) {
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" {
		redirectURL = "/"
	}

	state, err := s.sessionStore.CreateState(r.Context(), redirectURL)
	if err != nil {
		s.logger.Error("failed to create OAuth state", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to initiate login",
		})
		return
	}

	authURL := s.gitlabOAuthProvider.AuthURL(state.State)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleGitLabCallback handles GitLab OAuth callback.
func (s *Server) handleGitLabCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateParam := r.URL.Query().Get("state")

	if code == "" {
		s.logger.Error("GitLab callback missing code")
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing authorization code",
		})
		return
	}

	// Validate state
	state, err := s.sessionStore.ValidateState(r.Context(), stateParam)
	if err != nil {
		s.logger.Error("invalid OAuth state", "error", err)
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid or expired state",
		})
		return
	}

	// Exchange code for user
	user, err := s.gitlabOAuthProvider.Exchange(r.Context(), code)
	if err != nil {
		s.logger.Error("GitLab OAuth exchange failed", "error", err)
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "authentication failed: " + err.Error(),
		})
		return
	}

	// Create session
	_, err = s.authMiddleware.CreateSession(r.Context(), w, user)
	if err != nil {
		s.logger.Error("failed to create session", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to create session",
		})
		return
	}

	s.logger.Info("user logged in via GitLab", "user", user.Login, "email", user.Email)

	// Redirect to original URL
	redirectURL := state.RedirectURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// handleOIDCLogin initiates OIDC flow.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" {
		redirectURL = "/"
	}

	state, err := s.sessionStore.CreateState(r.Context(), redirectURL)
	if err != nil {
		s.logger.Error("failed to create OAuth state", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to initiate login",
		})
		return
	}

	authURL := s.oidcProvider.AuthURL(state.State)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOIDCCallback handles OIDC callback.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateParam := r.URL.Query().Get("state")

	if code == "" {
		errorDesc := r.URL.Query().Get("error_description")
		if errorDesc == "" {
			errorDesc = r.URL.Query().Get("error")
		}
		s.logger.Error("OIDC callback error", "error", errorDesc)
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "authentication failed: " + errorDesc,
		})
		return
	}

	// Validate state
	state, err := s.sessionStore.ValidateState(r.Context(), stateParam)
	if err != nil {
		s.logger.Error("invalid OAuth state", "error", err)
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid or expired state",
		})
		return
	}

	// Exchange code for user
	user, err := s.oidcProvider.Exchange(r.Context(), code)
	if err != nil {
		s.logger.Error("OIDC exchange failed", "error", err)
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "authentication failed: " + err.Error(),
		})
		return
	}

	// Create session
	_, err = s.authMiddleware.CreateSession(r.Context(), w, user)
	if err != nil {
		s.logger.Error("failed to create session", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to create session",
		})
		return
	}

	s.logger.Info("user logged in via OIDC", "user", user.Login, "email", user.Email)

	// Redirect to original URL
	redirectURL := state.RedirectURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// handleBasicLogin handles basic username/password authentication.
func (s *Server) handleBasicLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	if req.Username == "" || req.Password == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "username and password required",
		})
		return
	}

	// Authenticate user
	user, err := s.basicAuthProvider.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		s.logger.Warn("basic auth failed", "username", req.Username, "error", err)
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid username or password",
		})
		return
	}

	// Create session
	_, err = s.authMiddleware.CreateSession(r.Context(), w, user)
	if err != nil {
		s.logger.Error("failed to create session", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to create session",
		})
		return
	}

	s.logger.Info("user logged in via basic auth", "user", user.Login)

	respondJSON(w, http.StatusOK, map[string]any{
		"status": "authenticated",
		"user":   user,
	})
}

// handleLogout logs out the current user.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromRequest(r)
	if user != nil {
		s.logger.Info("user logged out", "user", user.Login)
	}

	if err := s.authMiddleware.DestroySession(r.Context(), w, r); err != nil {
		s.logger.Error("failed to destroy session", "error", err)
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status": "logged out",
	})
}

// handleMe returns the current user's information.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromRequest(r)
	if user == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "not authenticated",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}

// handleListUsers returns all active users (admin only).
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessionStore.ListAll(r.Context())
	if err != nil {
		s.logger.Error("failed to list sessions", "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to list users",
		})
		return
	}

	// Extract unique users from sessions
	usersMap := make(map[string]*models.User)
	for _, session := range sessions {
		if session.User != nil {
			usersMap[session.User.ID] = session.User
		}
	}

	users := make([]*models.User, 0, len(usersMap))
	for _, user := range usersMap {
		users = append(users, user)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"users": users,
		"count": len(users),
	})
}

// handleUpdateUserRole updates a user's role (admin only).
func (s *Server) handleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "user ID required",
		})
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return
	}

	role := models.Role(req.Role)
	if !role.Valid() {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid role, must be 'admin' or 'user'",
		})
		return
	}

	if err := s.roleResolver.SetUserRole(r.Context(), userID, role); err != nil {
		s.logger.Error("failed to set user role", "user_id", userID, "error", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to update role",
		})
		return
	}

	currentUser := auth.UserFromRequest(r)
	s.logger.Info("user role updated",
		"target_user", userID,
		"new_role", role,
		"by_user", currentUser.Login,
	)

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "updated",
		"user_id": userID,
		"role":    string(role),
	})
}
