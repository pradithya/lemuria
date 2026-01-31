package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes() {
	// Health check endpoints
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/healthz", s.handleHealth)
	s.router.Get("/ready", s.handleReady)

	// GitHub webhook endpoint
	s.router.Post("/webhook", s.webhookHandler.Handle)

	// API endpoints
	s.router.Route("/api/v1", func(r chi.Router) {
		r.Get("/status", s.handleStatus)
		r.Get("/locks", s.handleListLocks)
		r.Delete("/locks/{app}", s.handleDeleteLock)
	})
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
	respondJSON(w, http.StatusOK, map[string]interface{}{
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

	respondJSON(w, http.StatusOK, map[string]interface{}{
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
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
