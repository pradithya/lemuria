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

package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/org/lemuria/internal/models"
)

const (
	// SessionCookieName is the name of the session cookie.
	SessionCookieName = "lemuria_session"
)

// Middleware provides HTTP middleware for authentication and authorization.
type Middleware struct {
	sessionStore *RedisSessionStore
	roleResolver *ConfigRoleResolver
	cookieDomain string
	cookieSecure bool
}

// NewMiddleware creates a new auth middleware.
func NewMiddleware(
	sessionStore *RedisSessionStore,
	roleResolver *ConfigRoleResolver,
	cookieDomain string,
	cookieSecure bool,
) *Middleware {
	return &Middleware{
		sessionStore: sessionStore,
		roleResolver: roleResolver,
		cookieDomain: cookieDomain,
		cookieSecure: cookieSecure,
	}
}

// Authenticate is middleware that validates session and adds user to context.
// It does NOT require authentication - use RequireAuth for that.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := m.getSessionFromRequest(r)
		if session != nil && session.IsValid() {
			// Update role from resolver (may have changed)
			session.User.Role = m.roleResolver.ResolveRole(r.Context(), session.User)

			ctx := WithUser(r.Context(), session.User)
			ctx = WithSession(ctx, session)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth is middleware that requires an authenticated user.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromRequest(r)
		if user == nil {
			m.respondUnauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole is middleware that requires a specific role.
func (m *Middleware) RequireRole(role models.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromRequest(r)
			if user == nil {
				m.respondUnauthorized(w, r)
				return
			}
			if user.Role != role && user.Role != models.RoleAdmin {
				m.respondForbidden(w, r, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin is middleware that requires admin role.
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return m.RequireRole(models.RoleAdmin)(next)
}

// RequirePermission is middleware that requires a specific permission.
func (m *Middleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromRequest(r)
			if user == nil {
				m.respondUnauthorized(w, r)
				return
			}
			if !user.Role.HasPermission(permission) {
				m.respondForbidden(w, r, "permission denied: "+permission)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getSessionFromRequest extracts and validates the session from request.
func (m *Middleware) getSessionFromRequest(r *http.Request) *models.Session {
	// Try cookie first
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil && cookie.Value != "" {
		session, err := m.sessionStore.Get(r.Context(), cookie.Value)
		if err != nil {
			slog.Debug("failed to get session from cookie", "error", err)
			return nil
		}
		return session
	}

	// Try Authorization header (Bearer token)
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		session, err := m.sessionStore.Get(r.Context(), token)
		if err != nil {
			slog.Debug("failed to get session from bearer token", "error", err)
			return nil
		}
		return session
	}

	return nil
}

// SetSessionCookie sets the session cookie on the response.
func (m *Middleware) SetSessionCookie(w http.ResponseWriter, session *models.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.ID,
		Path:     "/",
		Domain:   m.cookieDomain,
		Secure:   m.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(session.ExpiresAt.Sub(session.CreatedAt).Seconds()),
	})
}

// ClearSessionCookie clears the session cookie.
func (m *Middleware) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   m.cookieDomain,
		Secure:   m.cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// CreateSession creates a new session for the user and sets the cookie.
func (m *Middleware) CreateSession(ctx context.Context, w http.ResponseWriter, user *models.User) (*models.Session, error) {
	// Resolve role
	user.Role = m.roleResolver.ResolveRole(ctx, user)

	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	m.SetSessionCookie(w, session)
	return session, nil
}

// DestroySession destroys the current session and clears the cookie.
func (m *Middleware) DestroySession(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	session := SessionFromRequest(r)
	if session != nil {
		if err := m.sessionStore.Delete(ctx, session.ID); err != nil {
			return err
		}
	}
	m.ClearSessionCookie(w)
	return nil
}

// respondUnauthorized sends a 401 Unauthorized response.
func (m *Middleware) respondUnauthorized(w http.ResponseWriter, r *http.Request) {
	// Check if request expects JSON
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error":    "unauthorized",
			"message":  "Authentication required",
			"loginUrl": "/auth/providers",
		}); err != nil {
			slog.Warn("failed to encode unauthorized response", "error", err)
		}
		return
	}

	// Redirect to login for browser requests
	http.Redirect(w, r, "/auth/providers", http.StatusFound)
}

// respondForbidden sends a 403 Forbidden response.
func (m *Middleware) respondForbidden(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error":   "forbidden",
		"message": message,
	}); err != nil {
		slog.Warn("failed to encode forbidden response", "error", err)
	}
}

// wantsJSON returns true if the request expects a JSON response.
func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") ||
		strings.HasPrefix(r.URL.Path, "/api/")
}
