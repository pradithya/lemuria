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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	sessionStore  *RedisSessionStore
	roleResolver  *ConfigRoleResolver
	sessionSecret []byte
	cookieDomain  string
	cookieSecure  bool
	logger        *slog.Logger
}

// NewMiddleware creates a new auth middleware.
func NewMiddleware(
	sessionStore *RedisSessionStore,
	roleResolver *ConfigRoleResolver,
	sessionSecret string,
	cookieDomain string,
	cookieSecure bool,
	logger *slog.Logger,
) *Middleware {
	return &Middleware{
		sessionStore:  sessionStore,
		roleResolver:  roleResolver,
		sessionSecret: []byte(sessionSecret),
		cookieDomain:  cookieDomain,
		cookieSecure:  cookieSecure,
		logger:        logger,
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
		sessionID, err := verifyAndExtractSessionID(m.sessionSecret, cookie.Value)
		if err != nil {
			m.logger.Debug("invalid session cookie signature", "error", err)
			return nil
		}
		session, err := m.sessionStore.Get(r.Context(), sessionID)
		if err != nil {
			m.logger.Debug("failed to get session from cookie", "error", err)
			return nil
		}
		return session
	}

	// Try Authorization header (Bearer token)
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		sessionID, err := verifyAndExtractSessionID(m.sessionSecret, token)
		if err != nil {
			m.logger.Debug("invalid bearer token signature", "error", err)
			return nil
		}
		session, err := m.sessionStore.Get(r.Context(), sessionID)
		if err != nil {
			m.logger.Debug("failed to get session from bearer token", "error", err)
			return nil
		}
		return session
	}

	return nil
}

// SetSessionCookie sets the session cookie on the response.
// The cookie value is signed using HMAC-SHA256: sessionID.signature
func (m *Middleware) SetSessionCookie(w http.ResponseWriter, session *models.Session) {
	signedValue := signSessionID(m.sessionSecret, session.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    signedValue,
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
			m.logger.Warn("failed to encode unauthorized response", "error", err)
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
		m.logger.Warn("failed to encode forbidden response", "error", err)
	}
}

// signSessionID signs a session ID using HMAC-SHA256 and returns "sessionID.signature".
func signSessionID(secret []byte, sessionID string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sessionID))
	signature := hex.EncodeToString(mac.Sum(nil))
	return sessionID + "." + signature
}

// verifyAndExtractSessionID verifies the HMAC signature and extracts the session ID.
func verifyAndExtractSessionID(secret []byte, cookieValue string) (string, error) {
	parts := strings.SplitN(cookieValue, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid signed cookie format")
	}

	sessionID := parts[0]
	providedSig := parts[1]

	// Decode the provided signature from hex to raw bytes.
	// This validates that the signature is well-formed hex of the correct length,
	// preventing timing leaks from hmac.Equal short-circuiting on length mismatch.
	providedMAC, err := hex.DecodeString(providedSig)
	if err != nil {
		return "", fmt.Errorf("invalid session cookie signature: malformed hex")
	}
	if len(providedMAC) != sha256.Size {
		return "", fmt.Errorf("invalid session cookie signature: wrong length")
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sessionID))
	expectedMAC := mac.Sum(nil)

	if !hmac.Equal(providedMAC, expectedMAC) {
		return "", fmt.Errorf("invalid session cookie signature")
	}

	return sessionID, nil
}

// wantsJSON returns true if the request expects a JSON response.
func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") ||
		strings.HasPrefix(r.URL.Path, "/api/")
}
