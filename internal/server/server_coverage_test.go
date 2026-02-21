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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/auth"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
	"github.com/org/lemuria/internal/queue"
)

// ============================================================================
// Helpers
// ============================================================================

// newTestQueueClient creates a queue.Client backed by the given address.
func newTestQueueClient(t *testing.T, addr string) *queue.Client {
	t.Helper()
	return queue.NewClient(config.RedisConfig{
		Address: addr,
	})
}

// newTestServerWithAuth creates a Server with real miniredis-backed auth components.
func newTestServerWithAuth(t *testing.T) (*Server, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = redisClient.Close() })

	sessionStore := auth.NewRedisSessionStore(redisClient, 24*time.Hour)
	roleResolver := auth.NewConfigRoleResolver(nil, models.RoleUser, sessionStore)
	authMiddleware := auth.NewMiddleware(sessionStore, roleResolver, "", false)

	s := &Server{
		config:           &config.Config{},
		loginRateLimiter: NewRateLimiter(5, 1*time.Minute),
		lockManager:      &mockLockManager{},
		redisClient:      redisClient,
		sessionStore:     sessionStore,
		roleResolver:     roleResolver,
		authMiddleware:   authMiddleware,
	}
	return s, mr
}

// createSession creates a real session in the store and returns the session cookie value.
func createSession(t *testing.T, store *auth.RedisSessionStore, user *models.User) *models.Session {
	t.Helper()
	sess, err := store.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	return sess
}

// ============================================================================
// handleLogout tests
// ============================================================================

func TestHandleLogoutWithAuthMiddleware(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	testUser := &models.User{
		ID:       "user-1",
		Login:    "testuser",
		Email:    "test@example.com",
		Provider: "github",
		Role:     models.RoleUser,
	}

	sess := createSession(t, s.sessionStore, testUser)

	tests := []struct {
		name           string
		sessionID      string
		hasUserContext bool
		wantStatus     int
		wantBody       string
	}{
		{
			name:           "logout with valid session",
			sessionID:      sess.ID,
			hasUserContext: true,
			wantStatus:     http.StatusOK,
			wantBody:       "logged out",
		},
		{
			name:           "logout without user context",
			sessionID:      "",
			hasUserContext: false,
			wantStatus:     http.StatusOK,
			wantBody:       "logged out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)

			if tt.hasUserContext {
				ctx := auth.WithUser(req.Context(), testUser)
				ctx = auth.WithSession(ctx, sess)
				req = req.WithContext(ctx)
				req.AddCookie(&http.Cookie{
					Name:  "lemuria_session",
					Value: tt.sessionID,
				})
			}

			w := httptest.NewRecorder()
			s.handleLogout(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if !strings.Contains(w.Body.String(), tt.wantBody) {
				t.Errorf("body = %q, want to contain %q", w.Body.String(), tt.wantBody)
			}
		})
	}
}

// ============================================================================
// handleListUsers tests
// ============================================================================

func TestHandleListUsers(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Create some sessions
	user1 := &models.User{
		ID:       "user-1",
		Login:    "alice",
		Email:    "alice@example.com",
		Provider: "github",
		Role:     models.RoleAdmin,
	}
	user2 := &models.User{
		ID:       "user-2",
		Login:    "bob",
		Email:    "bob@example.com",
		Provider: "github",
		Role:     models.RoleUser,
	}

	createSession(t, s.sessionStore, user1)
	createSession(t, s.sessionStore, user2)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()

	s.handleListUsers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	count, ok := resp["count"].(float64)
	if !ok {
		t.Fatal("missing count field")
	}
	if int(count) != 2 {
		t.Errorf("count = %d, want 2", int(count))
	}
}

func TestHandleListUsersEmpty(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()

	s.handleListUsers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	count := resp["count"].(float64)
	if int(count) != 0 {
		t.Errorf("count = %d, want 0", int(count))
	}
}

// ============================================================================
// handleUpdateUserRole additional tests
// ============================================================================

func TestHandleUpdateUserRoleSuccess(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	adminUser := &models.User{
		ID:       "admin-1",
		Login:    "admin",
		Email:    "admin@example.com",
		Provider: "basic",
		Role:     models.RoleAdmin,
	}

	body := `{"role":"admin"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/user-2/role", strings.NewReader(body))

	// Set URL param via chi context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "user-2")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithUser(ctx, adminUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleUpdateUserRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["status"] != "updated" {
		t.Errorf("status = %q, want %q", resp["status"], "updated")
	}
	if resp["role"] != "admin" {
		t.Errorf("role = %q, want %q", resp["role"], "admin")
	}
}

// ============================================================================
// OAuth login handler tests (GitHub)
// ============================================================================

func TestHandleGitHubLogin(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Create a GitHub OAuth provider
	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	tests := []struct {
		name        string
		redirect    string
		wantStatus  int
		wantContain string
	}{
		{
			name:        "login with valid redirect",
			redirect:    "/dashboard",
			wantStatus:  http.StatusFound,
			wantContain: "github.com",
		},
		{
			name:        "login with invalid redirect falls back to /",
			redirect:    "//evil.com",
			wantStatus:  http.StatusFound,
			wantContain: "github.com",
		},
		{
			name:        "login without redirect",
			redirect:    "",
			wantStatus:  http.StatusFound,
			wantContain: "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/auth/github/login"
			if tt.redirect != "" {
				url += "?redirect=" + tt.redirect
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			s.handleGitHubLogin(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			loc := w.Header().Get("Location")
			if !strings.Contains(loc, tt.wantContain) {
				t.Errorf("Location = %q, want to contain %q", loc, tt.wantContain)
			}
		})
	}
}

// ============================================================================
// OAuth callback handler tests (GitHub)
// ============================================================================

func TestHandleGitHubCallbackMissingCode(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback", nil)
	w := httptest.NewRecorder()

	s.handleGitHubCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !strings.Contains(resp["error"], "missing authorization code") {
		t.Errorf("error = %q, want to contain 'missing authorization code'", resp["error"])
	}
}

func TestHandleGitHubCallbackInvalidState(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=testcode&state=invalid-state", nil)
	w := httptest.NewRecorder()

	s.handleGitHubCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !strings.Contains(resp["error"], "invalid or expired state") {
		t.Errorf("error = %q, want to contain 'invalid or expired state'", resp["error"])
	}
}

// ============================================================================
// OAuth login handler tests (GitLab)
// ============================================================================

func TestHandleGitLabLogin(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		URL:          "https://gitlab.example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	tests := []struct {
		name        string
		redirect    string
		wantStatus  int
		wantContain string
	}{
		{
			name:        "login with valid redirect",
			redirect:    "/dashboard",
			wantStatus:  http.StatusFound,
			wantContain: "gitlab.example.com",
		},
		{
			name:        "login with invalid redirect falls back to /",
			redirect:    "//evil.com",
			wantStatus:  http.StatusFound,
			wantContain: "gitlab.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/auth/gitlab/login"
			if tt.redirect != "" {
				url += "?redirect=" + tt.redirect
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			s.handleGitLabLogin(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			loc := w.Header().Get("Location")
			if !strings.Contains(loc, tt.wantContain) {
				t.Errorf("Location = %q, want to contain %q", loc, tt.wantContain)
			}
		})
	}
}

func TestHandleGitLabCallbackMissingCode(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab/callback", nil)
	w := httptest.NewRecorder()

	s.handleGitLabCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGitLabCallbackInvalidState(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab/callback?code=testcode&state=invalid", nil)
	w := httptest.NewRecorder()

	s.handleGitLabCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// OIDC handler tests
// ============================================================================

func TestHandleOIDCCallbackMissingCode(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Test with error parameter
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?error=access_denied&error_description=User+denied+access", nil)
	w := httptest.NewRecorder()

	s.handleOIDCCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !strings.Contains(resp["error"], "User denied access") {
		t.Errorf("error = %q, want to contain 'User denied access'", resp["error"])
	}
}

func TestHandleOIDCCallbackMissingCodeNoDescription(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Test with error parameter but no description
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?error=access_denied", nil)
	w := httptest.NewRecorder()

	s.handleOIDCCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !strings.Contains(resp["error"], "access_denied") {
		t.Errorf("error = %q, want to contain 'access_denied'", resp["error"])
	}
}

func TestHandleOIDCCallbackInvalidState(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=testcode&state=invalid", nil)
	w := httptest.NewRecorder()

	s.handleOIDCCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// OIDC login handler (needs oidcProvider set — we can't easily create one
// without a real OIDC issuer, but we can test the CreateState error path
// by closing the redis)
// ============================================================================

func TestHandleOIDCLoginCreateStateError(t *testing.T) {
	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// We need an oidcProvider to test the login handler.
	// We can't create a real one, but we can set the field to a non-nil value
	// using an OIDC provider struct directly (it only calls AuthURL after CreateState).
	// Since we close miniredis first, CreateState will fail.
	mr.Close()

	// We need to use a minimal setup — since oidcProvider is concrete,
	// we'll test GitHub login CreateState error instead (same code path).
	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	}, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/auth/github/login", nil)
	w := httptest.NewRecorder()

	s.handleGitHubLogin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandleGitLabLoginCreateStateError(t *testing.T) {
	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	mr.Close()

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	}, "http://localhost:8080")

	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab/login", nil)
	w := httptest.NewRecorder()

	s.handleGitLabLogin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ============================================================================
// handleAuthProviders with all providers
// ============================================================================

func TestHandleAuthProvidersAllProviders(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "gh-id",
		ClientSecret: "gh-secret",
	}, "http://localhost:8080")

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "gl-id",
		ClientSecret: "gl-secret",
	}, "http://localhost:8080")

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "pass", Role: "admin"},
		},
	})

	// OIDC provider can't be set without a real issuer, so we skip it.

	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	w := httptest.NewRecorder()

	s.handleAuthProviders(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	providers, ok := resp["providers"].([]any)
	if !ok {
		t.Fatal("missing providers field")
	}

	// Should have 3 providers (github, gitlab, basic)
	if len(providers) != 3 {
		t.Errorf("got %d providers, want 3", len(providers))
	}

	authEnabled, ok := resp["auth_enabled"].(bool)
	if !ok || !authEnabled {
		t.Error("auth_enabled should be true")
	}
}

func TestHandleAuthProvidersNoProviders(t *testing.T) {
	s := newTestServer()
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	w := httptest.NewRecorder()

	s.handleAuthProviders(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Should be nil/null (no providers)
	if resp["providers"] != nil {
		t.Errorf("providers should be nil when no providers configured, got %v", resp["providers"])
	}

	authEnabled, ok := resp["auth_enabled"].(bool)
	if !ok || authEnabled {
		t.Error("auth_enabled should be false")
	}
}

// ============================================================================
// handleBasicLogin additional coverage
// ============================================================================

func TestHandleBasicLoginEmptyFields(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "pass", Role: "admin"},
		},
	})

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "empty username",
			body:       `{"username":"","password":"pass"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "username and password required",
		},
		{
			name:       "empty password",
			body:       `{"username":"admin","password":""}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "username and password required",
		},
		{
			name:       "invalid JSON",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			s.handleBasicLogin(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
			if !strings.Contains(resp["error"], tt.wantError) {
				t.Errorf("error = %q, want to contain %q", resp["error"], tt.wantError)
			}
		})
	}
}

func TestHandleBasicLoginSuccessful(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "testuser", Password: "testpass", Role: "user"},
		},
	})

	body := `{"username":"testuser","password":"testpass"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleBasicLogin(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["status"] != "authenticated" {
		t.Errorf("status = %q, want %q", resp["status"], "authenticated")
	}
}

func TestHandleBasicLoginCreateSessionError(t *testing.T) {
	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "testuser", Password: "testpass", Role: "user"},
		},
	})

	// Close miniredis to force session creation failure
	mr.Close()

	body := `{"username":"testuser","password":"testpass"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleBasicLogin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ============================================================================
// Close tests with more branches
// ============================================================================

func TestCloseWithAllComponents(t *testing.T) {
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	s := &Server{
		config:           &config.Config{},
		loginRateLimiter: NewRateLimiter(5, 1*time.Minute),
		lockManager:      &mockLockManager{},
		redisClient:      redisClient,
	}

	err := s.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestCloseNilComponents(t *testing.T) {
	s := &Server{
		config: &config.Config{},
	}

	// Should not panic with nil components
	err := s.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// ============================================================================
// cleanupStaleTempApps tests
// ============================================================================

func TestCleanupStaleTempApps(t *testing.T) {
	// Create a mock ArgoCD server that returns empty list
	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
		Insecure:  true,
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	s := &Server{
		config:     &config.Config{},
		argoClient: argoClient,
	}

	// Should not panic
	s.cleanupStaleTempApps()
}

func TestCleanupStaleTempAppsWithError(t *testing.T) {
	// Create a mock ArgoCD server that returns an error
	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
		Insecure:  true,
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	s := &Server{
		config:     &config.Config{},
		argoClient: argoClient,
	}

	// Should not panic, just log warning
	s.cleanupStaleTempApps()
}

// ============================================================================
// setupRoutes with auth enabled
// ============================================================================

func TestSetupRoutesWithAuth(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.router = chi.NewRouter()

	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "gh-id",
		ClientSecret: "gh-secret",
	}, "http://localhost:8080")

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "gl-id",
		ClientSecret: "gl-secret",
	}, "http://localhost:8080")

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "pass", Role: "admin"},
		},
	})

	s.setupMiddleware()
	s.setupRoutes()

	// Test that routes are registered by walking the router
	ts := httptest.NewServer(s.router)
	defer ts.Close()

	// Health endpoints should work
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Auth providers endpoint should work
	resp, err = http.Get(ts.URL + "/auth/providers")
	if err != nil {
		t.Fatalf("auth providers request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("auth providers status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// API status endpoint should require auth (redirect or 401)
	resp, err = http.Get(ts.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("status request failed: %v", err)
	}
	_ = resp.Body.Close()
	// Without auth, should get redirected or 401
	if resp.StatusCode == http.StatusOK {
		t.Error("status endpoint should require auth, got 200")
	}
}

// ============================================================================
// setupStaticFiles tests
// ============================================================================

func TestSetupStaticFiles(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.router = chi.NewRouter()
	s.setupStaticFiles()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "serve index.html at root",
			path:       "/",
			wantStatus: http.StatusOK,
		},
		{
			name:       "serve favicon",
			path:       "/favicon.svg",
			wantStatus: http.StatusOK,
		},
		{
			name:       "SPA fallback for unknown path",
			path:       "/locks",
			wantStatus: http.StatusOK,
		},
		{
			name:       "skip API paths",
			path:       "/api/something",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "skip auth paths",
			path:       "/auth/something",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "skip webhook paths",
			path:       "/webhook/github",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "skip health paths",
			path:       "/health",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "skip ready paths",
			path:       "/ready",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			_ = resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// ============================================================================
// NewFromDeps with auth-enabled config
// ============================================================================

func TestNewFromDepsWithAuthEnabled(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret-key-at-least-32-chars!",
			SessionTTL:    24 * time.Hour,
			DefaultRole:   "user",
			Basic: &config.BasicAuthConfig{
				Users: []config.BasicAuthUser{
					{Username: "admin", Password: "pass", Role: "admin"},
				},
			},
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.authMiddleware == nil {
		t.Error("authMiddleware should not be nil when auth is enabled")
	}
	if s.sessionStore == nil {
		t.Error("sessionStore should not be nil when auth is enabled")
	}
	if s.basicAuthProvider == nil {
		t.Error("basicAuthProvider should not be nil when basic auth is configured")
	}
}

func TestNewFromDepsWithGitHubOAuth(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret-key-at-least-32-chars!",
			SessionTTL:    24 * time.Hour,
			DefaultRole:   "user",
			GitHub: &config.GitHubOAuthConfig{
				ClientID:     "gh-client-id",
				ClientSecret: "gh-client-secret",
			},
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.githubOAuthProvider == nil {
		t.Error("githubOAuthProvider should not be nil when GitHub OAuth is configured")
	}
}

func TestNewFromDepsWithGitLabOAuth(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret-key-at-least-32-chars!",
			SessionTTL:    24 * time.Hour,
			DefaultRole:   "user",
			GitLab: &config.GitLabOAuthConfig{
				ClientID:     "gl-client-id",
				ClientSecret: "gl-client-secret",
			},
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.gitlabOAuthProvider == nil {
		t.Error("gitlabOAuthProvider should not be nil when GitLab OAuth is configured")
	}
}

func TestNewFromDepsAuthRedisError(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: "localhost:99999", // invalid address
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret",
			SessionTTL:    24 * time.Hour,
		},
	}

	deps := &Dependencies{
		ArgoClient:  nil,
		LockManager: &mockLockManager{},
	}

	_, err := NewFromDeps(cfg, deps)
	if err == nil {
		t.Fatal("expected error when Redis is unreachable")
	}
	if !strings.Contains(err.Error(), "setting up auth") {
		t.Errorf("error = %q, want to contain 'setting up auth'", err.Error())
	}
}

func TestNewFromDepsWithInvalidDefaultRole(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret-key-at-least-32-chars!",
			SessionTTL:    24 * time.Hour,
			DefaultRole:   "invalid_role",
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	// Should fallback to "user" role
	if s.roleResolver == nil {
		t.Fatal("roleResolver should not be nil")
	}
}

func TestNewFromDepsWithRoleAssignments(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret-key-at-least-32-chars!",
			SessionTTL:    24 * time.Hour,
			DefaultRole:   "user",
			RoleAssignments: []config.RoleAssignment{
				{Pattern: "admin@example.com", Role: "admin", Provider: "github"},
			},
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.roleResolver == nil {
		t.Fatal("roleResolver should not be nil")
	}
}

// ============================================================================
// handleListUsers error path (redis down)
// ============================================================================

func TestHandleListUsersError(t *testing.T) {
	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Close redis to force error
	mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()

	s.handleListUsers(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ============================================================================
// handleUpdateUserRole error path (redis down for SetUserRole)
// ============================================================================

func TestHandleUpdateUserRoleSetRoleError(t *testing.T) {
	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	adminUser := &models.User{
		ID:       "admin-1",
		Login:    "admin",
		Email:    "admin@example.com",
		Provider: "basic",
		Role:     models.RoleAdmin,
	}

	// Close redis to force SetUserRole error
	mr.Close()

	body := `{"role":"admin"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/user-2/role", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "user-2")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithUser(ctx, adminUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleUpdateUserRole(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ============================================================================
// Full route integration tests (with auth)
// ============================================================================

func TestRoutesAdminEndpointsRegisteredWithAuth(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.router = chi.NewRouter()
	s.setupMiddleware()
	s.setupRoutes()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	// Admin endpoints should return 401/302 without auth, not 404/405
	adminPaths := []struct {
		method string
		path   string
	}{
		{http.MethodDelete, "/api/v1/locks/myapp"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPut, "/api/v1/users/user-1/role"},
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects
		},
	}

	for _, tt := range adminPaths {
		t.Run(fmt.Sprintf("%s %s", tt.method, tt.path), func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			_ = resp.Body.Close()

			// Should get 401 or 302 (redirect to login), not 404 (route not found) or 405
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("status = %d, expected 401 or 302 (route should be registered)", resp.StatusCode)
			}
		})
	}
}

func TestRoutesOAuthEndpointsRegisteredWithAuth(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.router = chi.NewRouter()

	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "gh-id",
		ClientSecret: "gh-secret",
	}, "http://localhost:8080")

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "gl-id",
		ClientSecret: "gl-secret",
	}, "http://localhost:8080")

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "pass", Role: "admin"},
		},
	})

	s.setupMiddleware()
	s.setupRoutes()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// GitHub login should redirect to GitHub
	resp, err := client.Get(ts.URL + "/auth/github/login")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("github login status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	// GitLab login should redirect to GitLab
	resp, err = client.Get(ts.URL + "/auth/gitlab/login")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("gitlab login status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	// Basic login endpoint should accept POST
	resp, err = client.Post(ts.URL+"/auth/basic/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	// Should get 400 (bad request for empty body), not 404/405
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		t.Errorf("basic login status = %d, expected route to be registered", resp.StatusCode)
	}
}

// ============================================================================
// Dependencies.Close tests
// ============================================================================

func TestDependenciesCloseWithError(t *testing.T) {
	deps := &Dependencies{
		LockManager: &mockLockManager{
			pingErr: errors.New("close error"),
		},
	}

	// Close should not return error (it logs it)
	err := deps.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// ============================================================================
// handleMe with user context (additional coverage)
// ============================================================================

func TestHandleMeWithUserContext(t *testing.T) {
	s := newTestServer()
	defer s.loginRateLimiter.Stop()

	testUser := &models.User{
		ID:       "user-1",
		Login:    "testuser",
		Email:    "test@example.com",
		Provider: "github",
		Role:     models.RoleAdmin,
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := auth.WithUser(req.Context(), testUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleMe(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	userMap, ok := resp["user"].(map[string]any)
	if !ok {
		t.Fatal("missing user field")
	}
	if userMap["login"] != "testuser" {
		t.Errorf("login = %q, want %q", userMap["login"], "testuser")
	}
}

// ============================================================================
// Handler returns the router
// ============================================================================

func TestHandlerReturnsRouter(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	handler := s.Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}
	if handler != s.router {
		t.Error("Handler() should return the router")
	}
}

// ============================================================================
// Rate limiter cleanup integration
// ============================================================================

func TestRateLimiterCleanup(t *testing.T) {
	// Use a very short window to trigger cleanup
	rl := NewRateLimiter(2, 50*time.Millisecond)
	defer rl.Stop()

	// Add some entries
	rl.Allow("key1")
	rl.Allow("key2")

	// Wait for cleanup cycle
	time.Sleep(200 * time.Millisecond)

	// After cleanup, entries should be removed and allow again
	if !rl.Allow("key1") {
		t.Error("key1 should be allowed after cleanup")
	}
	if !rl.Allow("key2") {
		t.Error("key2 should be allowed after cleanup")
	}
}

// ============================================================================
// handleBasicLogin rate limiting with RemoteAddr formats
// ============================================================================

func TestHandleBasicLoginRateLimitingIPv6(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Override rate limiter with very low limit
	s.loginRateLimiter.Stop()
	s.loginRateLimiter = NewRateLimiter(1, 1*time.Minute)

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "pass", Role: "admin"},
		},
	})

	// First request should succeed (even with wrong password, it gets past rate limit)
	body := `{"username":"admin","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
	req.RemoteAddr = "[::1]:12345"
	w := httptest.NewRecorder()
	s.handleBasicLogin(w, req)

	// Second request should be rate limited
	req = httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
	req.RemoteAddr = "[::1]:12345"
	w = httptest.NewRecorder()
	s.handleBasicLogin(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "60")
	}
}

// ============================================================================
// handleDeleteLock with empty app parameter
// ============================================================================

func TestHandleDeleteLockEmptyApp(t *testing.T) {
	s := newTestServer()
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/locks/", nil)
	// Set up chi context with empty app param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("app", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	s.handleDeleteLock(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// handleUpdateUserRole with self-modification attempt
// ============================================================================

func TestHandleUpdateUserRoleSelfModification(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	adminUser := &models.User{
		ID:       "admin-1",
		Login:    "admin",
		Email:    "admin@example.com",
		Provider: "basic",
		Role:     models.RoleAdmin,
	}

	body := `{"role":"user"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/admin-1/role", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "admin-1") // Same as current user
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithUser(ctx, adminUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleUpdateUserRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !strings.Contains(resp["error"], "cannot modify your own role") {
		t.Errorf("error = %q, want to contain 'cannot modify your own role'", resp["error"])
	}
}

// ============================================================================
// handleUpdateUserRole with invalid role
// ============================================================================

func TestHandleUpdateUserRoleInvalidRole(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	adminUser := &models.User{
		ID:       "admin-1",
		Login:    "admin",
		Provider: "basic",
		Role:     models.RoleAdmin,
	}

	body := `{"role":"superadmin"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/user-2/role", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "user-2")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithUser(ctx, adminUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleUpdateUserRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// handleUpdateUserRole with invalid JSON
// ============================================================================

func TestHandleUpdateUserRoleInvalidJSON(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	adminUser := &models.User{
		ID:       "admin-1",
		Login:    "admin",
		Provider: "basic",
		Role:     models.RoleAdmin,
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/user-2/role", strings.NewReader("{invalid"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "user-2")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithUser(ctx, adminUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleUpdateUserRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// handleUpdateUserRole with empty user ID
// ============================================================================

func TestHandleUpdateUserRoleEmptyID(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users//role", strings.NewReader(`{"role":"admin"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	s.handleUpdateUserRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// handleBasicLogin with nil rate limiter
// ============================================================================

func TestHandleBasicLoginWithNilRateLimiter(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	s.loginRateLimiter.Stop()
	s.loginRateLimiter = nil

	s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "pass", Role: "admin"},
		},
	})

	body := `{"username":"admin","password":"pass"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleBasicLogin(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ============================================================================
// handleGitHubCallback with valid state but exchange error
// ============================================================================

func TestHandleGitHubCallbackExchangeError(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.githubOAuthProvider = auth.NewGitHubProvider(&config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	// Create a valid state
	state, err := s.sessionStore.CreateState(context.Background(), "/")
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	// The exchange will fail because the code is not valid (no real GitHub server)
	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=badcode&state="+state.State, nil)
	w := httptest.NewRecorder()

	s.handleGitHubCallback(w, req)

	// Should get 401 (exchange fails)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ============================================================================
// handleGitLabCallback with valid state but exchange error
// ============================================================================

func TestHandleGitLabCallbackExchangeError(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	s.gitlabOAuthProvider = auth.NewGitLabProvider(&config.GitLabOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")

	// Create a valid state
	state, err := s.sessionStore.CreateState(context.Background(), "/dashboard")
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab/callback?code=badcode&state="+state.State, nil)
	w := httptest.NewRecorder()

	s.handleGitLabCallback(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ============================================================================
// handleOIDCCallback with valid state but exchange error
// (requires oidcProvider to be set; skip if can't instantiate)
// ============================================================================

func TestHandleOIDCCallbackWithCodeAndInvalidState(t *testing.T) {
	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// OIDC callback with code but invalid state
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=testcode&state=badstate", nil)
	w := httptest.NewRecorder()

	s.handleOIDCCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ============================================================================
// Close with queue client
// ============================================================================

func TestCloseWithQueueClientError(t *testing.T) {
	mr := miniredis.RunT(t)

	// Create a real queue client against miniredis
	qClient := newTestQueueClient(t, mr.Addr())

	s := &Server{
		config:           &config.Config{},
		loginRateLimiter: NewRateLimiter(1, 1*time.Minute),
		lockManager:      &mockLockManager{},
		queueClient:      qClient,
	}

	err := s.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// ============================================================================
// NewFromDeps with queue enabled
// ============================================================================

func TestNewFromDepsWithQueueEnabled(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 8080,
			Host: "0.0.0.0",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Queue: config.QueueConfig{
			Enabled:     true,
			Concurrency: 5,
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.queueClient == nil {
		t.Error("queueClient should not be nil when queue is enabled")
	}
}

// ============================================================================
// Run: test server starts and can be shut down
// ============================================================================

func TestServerHTTPServerConfigured(t *testing.T) {
	mr := miniredis.RunT(t)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 9999,
			Host: "127.0.0.1",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
	}

	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	// Verify server is configured with correct address
	if s.httpServer == nil {
		t.Fatal("httpServer should not be nil")
	}
	if s.httpServer.Addr != "127.0.0.1:9999" {
		t.Errorf("httpServer.Addr = %q, want %q", s.httpServer.Addr, "127.0.0.1:9999")
	}
	if s.httpServer.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %v, want %v", s.httpServer.ReadTimeout, 15*time.Second)
	}
	if s.httpServer.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %v, want %v", s.httpServer.WriteTimeout, 60*time.Second)
	}
}

// ============================================================================
// handleLogout DestroySession error path
// ============================================================================

func TestHandleLogoutDestroySessionError(t *testing.T) {
	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	testUser := &models.User{
		ID:       "user-1",
		Login:    "testuser",
		Provider: "github",
		Role:     models.RoleUser,
	}

	sess := createSession(t, s.sessionStore, testUser)

	// Close miniredis to force DestroySession error
	mr.Close()

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	ctx := auth.WithUser(req.Context(), testUser)
	ctx = auth.WithSession(ctx, sess)
	req = req.WithContext(ctx)
	req.AddCookie(&http.Cookie{
		Name:  "lemuria_session",
		Value: sess.ID,
	})

	w := httptest.NewRecorder()
	s.handleLogout(w, req)

	// Should still return 200 even when DestroySession fails
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ============================================================================
// cleanupStaleTempApps with actual deletions
// ============================================================================

func TestCleanupStaleTempAppsWithDeletions(t *testing.T) {
	// Create a mock ArgoCD server that returns old temp apps
	argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet {
			// Return an old temp app
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"lemuria-tmp-test","namespace":"argocd","labels":{"lemuria.io/temp-app":"true"},"creationTimestamp":"2020-01-01T00:00:00Z"}}]}`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer argoServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoServer.URL,
		Token:     "test-token",
		Insecure:  true,
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	s := &Server{
		config:     &config.Config{},
		argoClient: argoClient,
	}

	// Should not panic and should log deletions
	s.cleanupStaleTempApps()
}

// ============================================================================
// OIDC Login and Callback tests with mock OIDC server
// ============================================================================

func TestHandleOIDCLoginWithMockProvider(t *testing.T) {
	// Create a mock OIDC discovery server
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			discovery := fmt.Sprintf(`{
				"issuer": "%s",
				"authorization_endpoint": "%s/authorize",
				"token_endpoint": "%s/token",
				"jwks_uri": "%s/keys",
				"userinfo_endpoint": "%s/userinfo"
			}`, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
			_, _ = w.Write([]byte(discovery))
			return
		}
		if r.URL.Path == "/keys" {
			_, _ = w.Write([]byte(`{"keys":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer oidcServer.Close()

	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	// Create OIDC provider with mock server
	oidcProvider, err := auth.NewOIDCProvider(context.Background(), &config.OIDCConfig{
		Name:         "TestSSO",
		IssuerURL:    oidcServer.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("failed to create OIDC provider: %v", err)
	}

	s.oidcProvider = oidcProvider

	tests := []struct {
		name       string
		redirect   string
		wantStatus int
	}{
		{
			name:       "login with valid redirect",
			redirect:   "/dashboard",
			wantStatus: http.StatusFound,
		},
		{
			name:       "login with invalid redirect",
			redirect:   "//evil.com",
			wantStatus: http.StatusFound,
		},
		{
			name:       "login without redirect",
			redirect:   "",
			wantStatus: http.StatusFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/auth/oidc/login"
			if tt.redirect != "" {
				url += "?redirect=" + tt.redirect
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			s.handleOIDCLogin(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			loc := w.Header().Get("Location")
			if loc == "" {
				t.Error("expected Location header")
			}
		})
	}
}

func TestHandleOIDCLoginCreateStateErrorWithProvider(t *testing.T) {
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			discovery := fmt.Sprintf(`{
				"issuer": "%s",
				"authorization_endpoint": "%s/authorize",
				"token_endpoint": "%s/token",
				"jwks_uri": "%s/keys",
				"userinfo_endpoint": "%s/userinfo"
			}`, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
			_, _ = w.Write([]byte(discovery))
			return
		}
		if r.URL.Path == "/keys" {
			_, _ = w.Write([]byte(`{"keys":[]}`))
			return
		}
	}))
	defer oidcServer.Close()

	s, mr := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	oidcProvider, err := auth.NewOIDCProvider(context.Background(), &config.OIDCConfig{
		IssuerURL:    oidcServer.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("failed to create OIDC provider: %v", err)
	}
	s.oidcProvider = oidcProvider

	// Close redis to force CreateState error
	mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil)
	w := httptest.NewRecorder()

	s.handleOIDCLogin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandleOIDCCallbackExchangeError(t *testing.T) {
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			discovery := fmt.Sprintf(`{
				"issuer": "%s",
				"authorization_endpoint": "%s/authorize",
				"token_endpoint": "%s/token",
				"jwks_uri": "%s/keys",
				"userinfo_endpoint": "%s/userinfo"
			}`, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
			_, _ = w.Write([]byte(discovery))
			return
		}
		if r.URL.Path == "/keys" {
			_, _ = w.Write([]byte(`{"keys":[]}`))
			return
		}
		if r.URL.Path == "/token" {
			// Return an error for token exchange
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
	}))
	defer oidcServer.Close()

	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	oidcProvider, err := auth.NewOIDCProvider(context.Background(), &config.OIDCConfig{
		IssuerURL:    oidcServer.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("failed to create OIDC provider: %v", err)
	}
	s.oidcProvider = oidcProvider

	// Create valid state
	state, err := s.sessionStore.CreateState(context.Background(), "/")
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=badcode&state="+state.State, nil)
	w := httptest.NewRecorder()

	s.handleOIDCCallback(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ============================================================================
// setupRoutes with OIDC provider
// ============================================================================

func TestSetupRoutesWithOIDCProvider(t *testing.T) {
	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			discovery := fmt.Sprintf(`{
				"issuer": "%s",
				"authorization_endpoint": "%s/authorize",
				"token_endpoint": "%s/token",
				"jwks_uri": "%s/keys",
				"userinfo_endpoint": "%s/userinfo"
			}`, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
			_, _ = w.Write([]byte(discovery))
			return
		}
		if r.URL.Path == "/keys" {
			_, _ = w.Write([]byte(`{"keys":[]}`))
			return
		}
	}))
	defer oidcServer.Close()

	s, _ := newTestServerWithAuth(t)
	defer s.loginRateLimiter.Stop()

	oidcProvider, err := auth.NewOIDCProvider(context.Background(), &config.OIDCConfig{
		Name:         "TestSSO",
		IssuerURL:    oidcServer.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}, "http://localhost:8080")
	if err != nil {
		t.Fatalf("failed to create OIDC provider: %v", err)
	}
	s.oidcProvider = oidcProvider

	s.router = chi.NewRouter()
	s.setupMiddleware()
	s.setupRoutes()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// OIDC login should redirect
	resp, err := client.Get(ts.URL + "/auth/oidc/login")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("oidc login status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	// Auth providers should include OIDC
	resp, err = http.Get(ts.URL + "/auth/providers")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var provResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&provResp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	providers := provResp["providers"].([]any)
	found := false
	for _, p := range providers {
		pm := p.(map[string]any)
		if pm["id"] == "oidc" {
			found = true
			if pm["name"] != "TestSSO" {
				t.Errorf("OIDC provider name = %q, want %q", pm["name"], "TestSSO")
			}
		}
	}
	if !found {
		t.Error("OIDC provider not found in providers list")
	}
}

// ============================================================================
// NewFromDeps with OIDC auth (integration with auth setup)
// ============================================================================

func TestNewFromDepsWithOIDCAuth(t *testing.T) {
	mr := miniredis.RunT(t)

	oidcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			discovery := fmt.Sprintf(`{
				"issuer": "%s",
				"authorization_endpoint": "%s/authorize",
				"token_endpoint": "%s/token",
				"jwks_uri": "%s/keys",
				"userinfo_endpoint": "%s/userinfo"
			}`, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host, "http://"+r.Host)
			_, _ = w.Write([]byte(discovery))
			return
		}
		if r.URL.Path == "/keys" {
			_, _ = w.Write([]byte(`{"keys":[]}`))
			return
		}
	}))
	defer oidcServer.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:    8080,
			Host:    "0.0.0.0",
			BaseURL: "http://localhost:8080",
		},
		Redis: config.RedisConfig{
			Address: mr.Addr(),
		},
		Auth: config.AuthConfig{
			Enabled:       true,
			SessionSecret: "test-secret-key-at-least-32-chars!",
			SessionTTL:    24 * time.Hour,
			DefaultRole:   "user",
			OIDC: &config.OIDCConfig{
				Name:         "TestSSO",
				IssuerURL:    oidcServer.URL,
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			},
		},
	}

	argoMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer argoMockServer.Close()

	argoClient, err := argocd.NewClient(config.ArgoCDConfig{
		ServerURL: argoMockServer.URL,
		Token:     "test-token",
	})
	if err != nil {
		t.Fatalf("failed to create argo client: %v", err)
	}

	deps := &Dependencies{
		ArgoClient:  argoClient,
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.oidcProvider == nil {
		t.Error("oidcProvider should not be nil when OIDC is configured")
	}
}
