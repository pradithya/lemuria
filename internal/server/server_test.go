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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/org/lemuria/internal/auth"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// mockLockManager implements lock.Manager for testing.
type mockLockManager struct {
	pingErr       error
	listAllResult []models.Lock
	listAllErr    error
	forceUnlockFn func(ctx context.Context, application string) error
}

func (m *mockLockManager) Lock(_ context.Context, _ models.LockRequest) (*models.LockResult, error) {
	return nil, nil
}

func (m *mockLockManager) Unlock(_ context.Context, _, _ string, _ int) error {
	return nil
}

func (m *mockLockManager) ForceUnlock(ctx context.Context, application string) error {
	if m.forceUnlockFn != nil {
		return m.forceUnlockFn(ctx, application)
	}
	return nil
}

func (m *mockLockManager) Get(_ context.Context, _ string) (*models.Lock, error) {
	return nil, nil
}

func (m *mockLockManager) ListByPR(_ context.Context, _ string, _ int) ([]models.Lock, error) {
	return nil, nil
}

func (m *mockLockManager) ListAll(_ context.Context) ([]models.Lock, error) {
	return m.listAllResult, m.listAllErr
}

func (m *mockLockManager) StorePlan(_ context.Context, _ string, _ int, _, _, _ string, _ []models.PlanDiffEntry) error {
	return nil
}

func (m *mockLockManager) GetPlan(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}

func (m *mockLockManager) Ping(_ context.Context) error {
	return m.pingErr
}

func (m *mockLockManager) Close() error {
	return nil
}

// newTestServer creates a minimal Server suitable for handler unit tests.
func newTestServer(opts ...func(*Server)) *Server {
	s := &Server{
		config:           &config.Config{},
		loginRateLimiter: NewRateLimiter(5, 1*time.Minute),
		lockManager:      &mockLockManager{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ============================================================================
// handleHealth tests
// ============================================================================

func TestHandleHealth(t *testing.T) {
	s := newTestServer()
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("status = %q, want %q", body["status"], "healthy")
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// ============================================================================
// handleReady tests
// ============================================================================

func TestHandleReady(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "ready when ping succeeds",
			pingErr:    nil,
			wantStatus: http.StatusOK,
			wantBody:   "ready",
		},
		{
			name:       "not ready when ping fails",
			pingErr:    errors.New("connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(func(s *Server) {
				s.lockManager = &mockLockManager{pingErr: tt.pingErr}
			})
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			w := httptest.NewRecorder()

			s.handleReady(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if body["status"] != tt.wantBody {
				t.Errorf("status = %q, want %q", body["status"], tt.wantBody)
			}
		})
	}
}

// ============================================================================
// handleStatus tests
// ============================================================================

func TestHandleStatus(t *testing.T) {
	s := newTestServer()
	defer s.loginRateLimiter.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()

	s.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "running" {
		t.Errorf("status = %q, want %q", body["status"], "running")
	}

	if body["version"] != "0.1.0" {
		t.Errorf("version = %q, want %q", body["version"], "0.1.0")
	}
}

// ============================================================================
// isValidRedirectURL tests
// ============================================================================

func TestIsValidRedirectURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "valid relative path",
			url:  "/dashboard",
			want: true,
		},
		{
			name: "valid root path",
			url:  "/",
			want: true,
		},
		{
			name: "valid nested path",
			url:  "/admin/users",
			want: true,
		},
		{
			name: "valid path with query string",
			url:  "/locks?app=myapp",
			want: true,
		},
		{
			name: "protocol-relative URL is rejected",
			url:  "//evil.com",
			want: false,
		},
		{
			name: "absolute URL is rejected",
			url:  "https://evil.com",
			want: false,
		},
		{
			name: "empty string is rejected",
			url:  "",
			want: false,
		},
		{
			name: "relative path without leading slash is rejected",
			url:  "dashboard",
			want: false,
		},
		{
			name: "protocol-relative with path is rejected",
			url:  "//evil.com/callback",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidRedirectURL(tt.url)
			if got != tt.want {
				t.Errorf("isValidRedirectURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// ============================================================================
// handleAuthProviders tests
// ============================================================================

func TestHandleAuthProviders(t *testing.T) {
	tests := []struct {
		name              string
		setupServer       func(*Server)
		wantAuthEnabled   bool
		wantProviderCount int
		wantProviderIDs   []string
	}{
		{
			name:              "no providers configured",
			setupServer:       func(_ *Server) {},
			wantAuthEnabled:   false,
			wantProviderCount: 0,
		},
		{
			name: "basic auth provider configured",
			setupServer: func(s *Server) {
				s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
					Users: []config.BasicAuthUser{
						{Username: "admin", Password: "pass", Role: "admin"},
					},
				})
			},
			wantAuthEnabled:   false, // authMiddleware is still nil
			wantProviderCount: 1,
			wantProviderIDs:   []string{"basic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(tt.setupServer)
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
			w := httptest.NewRecorder()

			s.handleAuthProviders(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}

			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			authEnabled, ok := body["auth_enabled"].(bool)
			if !ok {
				t.Fatal("auth_enabled not a boolean")
			}
			if authEnabled != tt.wantAuthEnabled {
				t.Errorf("auth_enabled = %v, want %v", authEnabled, tt.wantAuthEnabled)
			}

			providers, ok := body["providers"].([]any)
			if !ok {
				// When providers is null/nil in JSON, it will be nil
				if tt.wantProviderCount > 0 {
					t.Fatalf("providers is not an array")
				}
				return
			}

			if len(providers) != tt.wantProviderCount {
				t.Errorf("provider count = %d, want %d", len(providers), tt.wantProviderCount)
			}

			for i, wantID := range tt.wantProviderIDs {
				if i >= len(providers) {
					break
				}
				p := providers[i].(map[string]any)
				if p["id"] != wantID {
					t.Errorf("provider[%d].id = %q, want %q", i, p["id"], wantID)
				}
			}
		})
	}
}

// ============================================================================
// handleListLocks tests
// ============================================================================

func TestHandleListLocks(t *testing.T) {
	tests := []struct {
		name       string
		locks      []models.Lock
		listErr    error
		wantStatus int
		wantCount  float64
	}{
		{
			name:       "empty locks",
			locks:      []models.Lock{},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name: "has locks",
			locks: []models.Lock{
				{Application: "app1", PRNumber: 1, Repo: "org/repo", User: "user1"},
				{Application: "app2", PRNumber: 2, Repo: "org/repo", User: "user2"},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "list error",
			listErr:    errors.New("redis unavailable"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(func(s *Server) {
				s.lockManager = &mockLockManager{
					listAllResult: tt.locks,
					listAllErr:    tt.listErr,
				}
			})
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/locks", nil)
			w := httptest.NewRecorder()

			s.handleListLocks(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.listErr != nil {
				if _, ok := body["error"]; !ok {
					t.Error("expected error in response body")
				}
				return
			}

			count, ok := body["count"].(float64)
			if !ok {
				t.Fatal("count not a number")
			}
			if count != tt.wantCount {
				t.Errorf("count = %v, want %v", count, tt.wantCount)
			}
		})
	}
}

// ============================================================================
// handleDeleteLock tests
// ============================================================================

func TestHandleDeleteLock(t *testing.T) {
	tests := []struct {
		name           string
		appParam       string
		forceUnlockErr error
		wantStatus     int
		wantBody       string
	}{
		{
			name:       "successful unlock",
			appParam:   "my-app",
			wantStatus: http.StatusOK,
			wantBody:   "unlocked",
		},
		{
			name:       "empty app name",
			appParam:   "",
			wantStatus: http.StatusBadRequest,
			wantBody:   "application name required",
		},
		{
			name:           "unlock error",
			appParam:       "my-app",
			forceUnlockErr: errors.New("redis error"),
			wantStatus:     http.StatusInternalServerError,
			wantBody:       "failed to delete lock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(func(s *Server) {
				s.lockManager = &mockLockManager{
					forceUnlockFn: func(_ context.Context, _ string) error {
						return tt.forceUnlockErr
					},
				}
			})
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/locks/"+tt.appParam, nil)

			// chi URL params need a chi context
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("app", tt.appParam)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			s.handleDeleteLock(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.wantStatus == http.StatusOK {
				if body["status"] != tt.wantBody {
					t.Errorf("status = %q, want %q", body["status"], tt.wantBody)
				}
				if body["application"] != tt.appParam {
					t.Errorf("application = %q, want %q", body["application"], tt.appParam)
				}
			} else {
				if body["error"] != tt.wantBody {
					t.Errorf("error = %q, want %q", body["error"], tt.wantBody)
				}
			}
		})
	}
}

// ============================================================================
// handleMe tests
// ============================================================================

func TestHandleMe(t *testing.T) {
	tests := []struct {
		name       string
		user       *models.User
		wantStatus int
	}{
		{
			name: "authenticated user",
			user: &models.User{
				ID:       "user-123",
				Login:    "testuser",
				Email:    "test@example.com",
				Provider: "github",
				Role:     models.RoleUser,
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unauthenticated request",
			user:       nil,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
			if tt.user != nil {
				ctx := auth.WithUser(req.Context(), tt.user)
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()

			s.handleMe(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.user != nil {
				userMap, ok := body["user"].(map[string]any)
				if !ok {
					t.Fatal("user not present in response")
				}
				if userMap["login"] != tt.user.Login {
					t.Errorf("login = %q, want %q", userMap["login"], tt.user.Login)
				}
			} else {
				if _, ok := body["error"]; !ok {
					t.Error("expected error in response for unauthenticated request")
				}
			}
		})
	}
}

// ============================================================================
// handleUpdateUserRole tests
// ============================================================================

func TestHandleUpdateUserRole(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		reqBody    string
		ctxUser    *models.User
		wantStatus int
		wantError  string
	}{
		{
			name:       "empty user ID",
			userID:     "",
			reqBody:    `{"role":"admin"}`,
			ctxUser:    &models.User{ID: "admin-1", Login: "admin"},
			wantStatus: http.StatusBadRequest,
			wantError:  "user ID required",
		},
		{
			name:       "invalid request body",
			userID:     "user-1",
			reqBody:    `not json`,
			ctxUser:    &models.User{ID: "admin-1", Login: "admin"},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "invalid role",
			userID:     "user-1",
			reqBody:    `{"role":"superadmin"}`,
			ctxUser:    &models.User{ID: "admin-1", Login: "admin"},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid role, must be 'admin' or 'user'",
		},
		{
			name:       "cannot modify own role",
			userID:     "admin-1",
			reqBody:    `{"role":"user"}`,
			ctxUser:    &models.User{ID: "admin-1", Login: "admin"},
			wantStatus: http.StatusBadRequest,
			wantError:  "cannot modify your own role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+tt.userID+"/role",
				strings.NewReader(tt.reqBody))
			req.Header.Set("Content-Type", "application/json")

			// Set chi URL param
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.userID)
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)

			// Set authenticated user in context
			if tt.ctxUser != nil {
				ctx = auth.WithUser(ctx, tt.ctxUser)
			}
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			s.handleUpdateUserRole(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.wantError != "" {
				if body["error"] != tt.wantError {
					t.Errorf("error = %q, want %q", body["error"], tt.wantError)
				}
			}
		})
	}
}

// ============================================================================
// handleBasicLogin tests
// ============================================================================

func TestHandleBasicLogin(t *testing.T) {
	tests := []struct {
		name       string
		reqBody    string
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid request body",
			reqBody:    `not json`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "missing username",
			reqBody:    `{"username":"","password":"pass"}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "username and password required",
		},
		{
			name:       "missing password",
			reqBody:    `{"username":"user","password":""}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "username and password required",
		},
		{
			name:       "missing both",
			reqBody:    `{}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "username and password required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(func(s *Server) {
				s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
					Users: []config.BasicAuthUser{
						{Username: "admin", Password: "secret", Role: "admin"},
					},
				})
			})
			defer s.loginRateLimiter.Stop()

			req := httptest.NewRequest(http.MethodPost, "/auth/basic/login",
				strings.NewReader(tt.reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "192.168.1.1:12345"

			w := httptest.NewRecorder()
			s.handleBasicLogin(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if errMsg, ok := body["error"].(string); ok {
				if errMsg != tt.wantError {
					t.Errorf("error = %q, want %q", errMsg, tt.wantError)
				}
			} else if tt.wantError != "" {
				t.Errorf("expected error %q but none found", tt.wantError)
			}
		})
	}
}

func TestHandleBasicLoginRateLimiting(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.loginRateLimiter = NewRateLimiter(2, 1*time.Minute)
		s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
			Users: []config.BasicAuthUser{
				{Username: "admin", Password: "secret", Role: "admin"},
			},
		})
	})
	defer s.loginRateLimiter.Stop()

	makeReq := func() *httptest.ResponseRecorder {
		body := `{"username":"wrong","password":"wrong"}`
		req := httptest.NewRequest(http.MethodPost, "/auth/basic/login",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:9999"
		w := httptest.NewRecorder()
		s.handleBasicLogin(w, req)
		return w
	}

	// First two should get through (may return 401 for bad creds, not 429)
	w1 := makeReq()
	if w1.Code == http.StatusTooManyRequests {
		t.Error("1st request should not be rate limited")
	}
	w2 := makeReq()
	if w2.Code == http.StatusTooManyRequests {
		t.Error("2nd request should not be rate limited")
	}

	// Third request should be rate limited
	w3 := makeReq()
	if w3.Code != http.StatusTooManyRequests {
		t.Errorf("3rd request status = %d, want %d", w3.Code, http.StatusTooManyRequests)
	}

	retryAfter := w3.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "60")
	}
}

func TestHandleBasicLoginBadCredentials(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
			Users: []config.BasicAuthUser{
				{Username: "admin", Password: "correct-password", Role: "admin"},
			},
		})
	})
	defer s.loginRateLimiter.Stop()

	body := `{"username":"admin","password":"wrong-password"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/basic/login",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.2:9999"
	w := httptest.NewRecorder()

	s.handleBasicLogin(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	var respBody map[string]string
	if err := json.NewDecoder(w.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if respBody["error"] != "invalid username or password" {
		t.Errorf("error = %q, want %q", respBody["error"], "invalid username or password")
	}
}

// ============================================================================
// handleLogout tests
// ============================================================================

func TestHandleLogout(t *testing.T) {
	// We can test the response format without a real auth middleware,
	// since DestroySession will fail but the handler still returns 200.
	tests := []struct {
		name       string
		user       *models.User
		wantStatus int
	}{
		{
			name:       "logout without user context still returns 200",
			user:       nil,
			wantStatus: http.StatusOK,
		},
		{
			name: "logout with user context returns 200",
			user: &models.User{
				ID:    "user-1",
				Login: "testuser",
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The handler calls s.authMiddleware.DestroySession which will
			// panic if authMiddleware is nil. We only test the response path
			// that matters: the user-logging part and JSON response.
			// Since we cannot easily mock authMiddleware here, we skip
			// the DestroySession path test and focus on handleMe instead.
			t.Skip("requires auth middleware mock")
		})
	}
}

// ============================================================================
// requestLogger middleware tests
// ============================================================================

func TestRequestLoggerSkipsHealthEndpoints(t *testing.T) {
	healthPaths := []string{"/health", "/healthz", "/ready"}

	for _, path := range healthPaths {
		t.Run(path, func(t *testing.T) {
			s := newTestServer()
			defer s.loginRateLimiter.Stop()

			called := false
			handler := s.requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if !called {
				t.Error("next handler was not called")
			}
			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
		})
	}
}

func TestRequestLoggerPassesThroughNonHealthRequests(t *testing.T) {
	s := newTestServer()
	defer s.loginRateLimiter.Stop()

	called := false
	handler := s.requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Error("next handler was not called")
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

// ============================================================================
// NewFromDeps tests
// ============================================================================

func TestNewFromDeps(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
	}

	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.config != cfg {
		t.Error("config not set correctly")
	}

	if s.router == nil {
		t.Error("router should not be nil")
	}

	if s.httpServer == nil {
		t.Error("httpServer should not be nil")
	}

	if s.httpServer.Addr != "0.0.0.0:8080" {
		t.Errorf("httpServer.Addr = %q, want %q", s.httpServer.Addr, "0.0.0.0:8080")
	}

	if s.loginRateLimiter == nil {
		t.Error("loginRateLimiter should not be nil")
	}

	if s.lockManager != deps.LockManager {
		t.Error("lockManager not set correctly")
	}
}

func TestNewFromDepsWithQueueDisabled(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 9090,
		},
		Queue: config.QueueConfig{
			Enabled: false,
		},
	}

	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.queueClient != nil {
		t.Error("queueClient should be nil when queue is disabled")
	}
}

func TestNewFromDepsWebhookHandlersNotSetWithoutVCS(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	deps := &Dependencies{
		LockManager: &mockLockManager{},
		// No VCS clients
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.githubWebhookHandler != nil {
		t.Error("githubWebhookHandler should be nil when no GitHub VCS configured")
	}
	if s.gitlabWebhookHandler != nil {
		t.Error("gitlabWebhookHandler should be nil when no GitLab VCS configured")
	}
}

// ============================================================================
// Handler() test
// ============================================================================

func TestHandler(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	h := s.Handler()
	if h == nil {
		t.Fatal("Handler() should not return nil")
	}

	// Verify the handler actually works by hitting the health endpoint
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ============================================================================
// Integration-style route tests (via full router)
// ============================================================================

func TestRoutesHealthEndpoints(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	endpoints := []string{"/health", "/healthz"}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep, nil)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if body["status"] != "healthy" {
				t.Errorf("status = %q, want %q", body["status"], "healthy")
			}
		})
	}
}

func TestRoutesReadyEndpoint(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRoutesStatusEndpointNoAuth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "running" {
		t.Errorf("status = %q, want %q", body["status"], "running")
	}
}

func TestRoutesLocksEndpointNoAuth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{
			listAllResult: []models.Lock{
				{Application: "app1", PRNumber: 42, Repo: "org/repo"},
			},
		},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	count := body["count"].(float64)
	if count != 1 {
		t.Errorf("count = %v, want 1", count)
	}
}

func TestRoutesAdminEndpointsNotRegisteredWithoutAuth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	// DELETE /api/v1/locks/{app} should not be registered when auth is disabled
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/locks/my-app", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	// Should get 404 Not Found since the DELETE route is not registered when auth is disabled
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRoutesAuthProvidersEndpoint(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}

	s, err := NewFromDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewFromDeps() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["auth_enabled"] != false {
		t.Errorf("auth_enabled = %v, want false", body["auth_enabled"])
	}
}

// ============================================================================
// Close tests
// ============================================================================

func TestClose(t *testing.T) {
	s := newTestServer()
	// Close should not panic even with minimal setup
	if err := s.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestCloseWithRateLimiter(t *testing.T) {
	s := newTestServer()
	// Should be safe to close - rate limiter gets stopped
	if err := s.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// ============================================================================
// respondJSON edge cases
// ============================================================================

func TestRespondJSONWithComplexData(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]any{
		"locks": []models.Lock{
			{Application: "app1", PRNumber: 1},
		},
		"count":  1,
		"nested": map[string]string{"key": "value"},
	}

	respondJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", body["count"])
	}
}

func TestRespondJSONWithNilData(t *testing.T) {
	w := httptest.NewRecorder()
	respondJSON(w, http.StatusOK, nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := strings.TrimSpace(w.Body.String())
	if body != "null" {
		t.Errorf("body = %q, want %q", body, "null")
	}
}

func TestRespondJSONWithUnencodableData(t *testing.T) {
	w := httptest.NewRecorder()
	// channels cannot be JSON-encoded
	respondJSON(w, http.StatusOK, map[string]any{
		"bad": make(chan int),
	})

	// Status is still written even though encoding fails
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ============================================================================
// handleBasicLogin with RemoteAddr edge cases
// ============================================================================

func TestHandleBasicLoginRemoteAddrWithoutPort(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.loginRateLimiter = NewRateLimiter(1, 1*time.Minute)
		s.basicAuthProvider = auth.NewBasicProvider(&config.BasicAuthConfig{
			Users: []config.BasicAuthUser{
				{Username: "admin", Password: "secret", Role: "admin"},
			},
		})
	})
	defer s.loginRateLimiter.Stop()

	// RemoteAddr without port (unusual but possible)
	body := bytes.NewBufferString(`{"username":"wrong","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/basic/login", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.1" // no port

	w := httptest.NewRecorder()
	s.handleBasicLogin(w, req)

	// Should still work (uses full RemoteAddr as key)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ============================================================================
// Deps Close test
// ============================================================================

func TestDependenciesClose(t *testing.T) {
	deps := &Dependencies{
		LockManager: &mockLockManager{},
	}
	if err := deps.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestDependenciesCloseNilLockManager(t *testing.T) {
	deps := &Dependencies{}
	if err := deps.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
