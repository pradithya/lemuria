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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/org/lemuria/internal/models"
)

func TestWantsJSON(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		path   string
		want   bool
	}{
		{
			name:   "accept application/json",
			accept: "application/json",
			path:   "/some/path",
			want:   true,
		},
		{
			name:   "accept contains application/json among others",
			accept: "text/html, application/json",
			path:   "/some/path",
			want:   true,
		},
		{
			name:   "api path prefix",
			accept: "text/html",
			path:   "/api/v1/users",
			want:   true,
		},
		{
			name:   "html accept, non-api path",
			accept: "text/html",
			path:   "/dashboard",
			want:   false,
		},
		{
			name:   "no accept header, non-api path",
			accept: "",
			path:   "/login",
			want:   false,
		},
		{
			name:   "api subpath",
			accept: "",
			path:   "/api/applications",
			want:   true,
		},
		{
			name:   "path starting with /api/ is JSON",
			accept: "",
			path:   "/api/",
			want:   true,
		},
		{
			name:   "path /apis is not API path",
			accept: "",
			path:   "/apis/something",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.path, nil)
			if tt.accept != "" {
				r.Header.Set("Accept", tt.accept)
			}
			got := wantsJSON(r)
			if got != tt.want {
				t.Errorf("wantsJSON() = %v, want %v (accept=%q, path=%q)", got, tt.want, tt.accept, tt.path)
			}
		})
	}
}

func TestMiddleware_RequireAuth_Authenticated(t *testing.T) {
	m := &Middleware{}

	handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// Create request with user in context
	user := &models.User{ID: "u1", Login: "testuser", Role: models.RoleUser}
	ctx := WithUser(context.Background(), user)
	r := httptest.NewRequest("GET", "/dashboard", nil)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_RequireAuth_NotAuthenticated_JSON(t *testing.T) {
	m := &Middleware{}

	handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest("GET", "/api/data", nil)
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "unauthorized" {
		t.Errorf("error = %q, want %q", resp["error"], "unauthorized")
	}
}

func TestMiddleware_RequireAuth_NotAuthenticated_Browser(t *testing.T) {
	m := &Middleware{}

	handler := m.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest("GET", "/dashboard", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/auth/providers" {
		t.Errorf("Location = %q, want %q", loc, "/auth/providers")
	}
}

func TestMiddleware_RequireRole(t *testing.T) {
	tests := []struct {
		name         string
		user         *models.User
		requiredRole models.Role
		wantStatus   int
		acceptJSON   bool
	}{
		{
			name:         "no user - unauthorized JSON",
			user:         nil,
			requiredRole: models.RoleAdmin,
			wantStatus:   http.StatusUnauthorized,
			acceptJSON:   true,
		},
		{
			name:         "user has exact role",
			user:         &models.User{ID: "u1", Role: models.RoleUser},
			requiredRole: models.RoleUser,
			wantStatus:   http.StatusOK,
			acceptJSON:   true,
		},
		{
			name:         "admin has access to any role",
			user:         &models.User{ID: "u1", Role: models.RoleAdmin},
			requiredRole: models.RoleUser,
			wantStatus:   http.StatusOK,
			acceptJSON:   true,
		},
		{
			name:         "user does not have required role",
			user:         &models.User{ID: "u1", Role: models.RoleUser},
			requiredRole: models.RoleAdmin,
			wantStatus:   http.StatusForbidden,
			acceptJSON:   true,
		},
		{
			name:         "no user - unauthorized browser redirect",
			user:         nil,
			requiredRole: models.RoleAdmin,
			wantStatus:   http.StatusFound,
			acceptJSON:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Middleware{}
			handler := m.RequireRole(tt.requiredRole)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			r := httptest.NewRequest("GET", "/admin", nil)
			if tt.acceptJSON {
				r.Header.Set("Accept", "application/json")
			} else {
				r.Header.Set("Accept", "text/html")
			}

			if tt.user != nil {
				ctx := WithUser(r.Context(), tt.user)
				r = r.WithContext(ctx)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestMiddleware_RequireAdmin(t *testing.T) {
	tests := []struct {
		name       string
		user       *models.User
		wantStatus int
	}{
		{
			name:       "no user - unauthorized",
			user:       nil,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "non-admin user - forbidden",
			user:       &models.User{ID: "u1", Role: models.RoleUser},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "admin user - allowed",
			user:       &models.User{ID: "u1", Role: models.RoleAdmin},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Middleware{}
			handler := m.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			r := httptest.NewRequest("GET", "/api/admin", nil)
			r.Header.Set("Accept", "application/json")

			if tt.user != nil {
				ctx := WithUser(r.Context(), tt.user)
				r = r.WithContext(ctx)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestMiddleware_RequirePermission(t *testing.T) {
	tests := []struct {
		name       string
		user       *models.User
		permission string
		wantStatus int
	}{
		{
			name:       "no user - unauthorized",
			user:       nil,
			permission: "read",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "user has permission",
			user:       &models.User{ID: "u1", Role: models.RoleUser},
			permission: "read",
			wantStatus: http.StatusOK,
		},
		{
			name:       "user lacks permission",
			user:       &models.User{ID: "u1", Role: models.RoleUser},
			permission: "write",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "admin has all permissions",
			user:       &models.User{ID: "u1", Role: models.RoleAdmin},
			permission: "write",
			wantStatus: http.StatusOK,
		},
		{
			name:       "admin has delete permission",
			user:       &models.User{ID: "u1", Role: models.RoleAdmin},
			permission: "delete",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Middleware{}
			handler := m.RequirePermission(tt.permission)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			r := httptest.NewRequest("GET", "/api/resource", nil)
			r.Header.Set("Accept", "application/json")

			if tt.user != nil {
				ctx := WithUser(r.Context(), tt.user)
				r = r.WithContext(ctx)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestMiddleware_RequirePermission_ForbiddenResponse(t *testing.T) {
	m := &Middleware{}
	handler := m.RequirePermission("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	user := &models.User{ID: "u1", Role: models.RoleUser}
	ctx := WithUser(context.Background(), user)
	r := httptest.NewRequest("GET", "/api/admin/action", nil)
	r = r.WithContext(ctx)
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "forbidden" {
		t.Errorf("error = %q, want %q", resp["error"], "forbidden")
	}
	msg, _ := resp["message"].(string)
	if msg == "" {
		t.Error("expected non-empty message in forbidden response")
	}
}

func TestMiddleware_SetSessionCookie(t *testing.T) {
	m := &Middleware{
		cookieDomain: "example.com",
		cookieSecure: true,
	}

	session := &models.Session{
		ID: "sess-abc-123",
	}
	// Set times so MaxAge can be computed
	now := session.CreatedAt
	session.ExpiresAt = now.Add(3600_000_000_000) // 1 hour in nanoseconds (time.Hour)

	w := httptest.NewRecorder()
	m.SetSessionCookie(w, session)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected at least one cookie")
	}
	c := cookies[0]
	if c.Name != SessionCookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, SessionCookieName)
	}
	if c.Value != "sess-abc-123" {
		t.Errorf("cookie value = %q, want %q", c.Value, "sess-abc-123")
	}
	if c.Domain != "example.com" {
		t.Errorf("cookie domain = %q, want %q", c.Domain, "example.com")
	}
	if !c.Secure {
		t.Error("cookie should be secure")
	}
	if !c.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
	if c.Path != "/" {
		t.Errorf("cookie path = %q, want %q", c.Path, "/")
	}
}

func TestMiddleware_ClearSessionCookie(t *testing.T) {
	m := &Middleware{
		cookieDomain: "example.com",
		cookieSecure: true,
	}

	w := httptest.NewRecorder()
	m.ClearSessionCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected at least one cookie")
	}
	c := cookies[0]
	if c.Name != SessionCookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, SessionCookieName)
	}
	if c.Value != "" {
		t.Errorf("cookie value = %q, want empty", c.Value)
	}
	if c.MaxAge != -1 {
		t.Errorf("cookie MaxAge = %d, want -1", c.MaxAge)
	}
}

func TestNewMiddleware(t *testing.T) {
	m := NewMiddleware(nil, nil, "test.com", true)
	if m == nil {
		t.Fatal("NewMiddleware() returned nil")
	}
	if m.cookieDomain != "test.com" {
		t.Errorf("cookieDomain = %q, want %q", m.cookieDomain, "test.com")
	}
	if !m.cookieSecure {
		t.Error("cookieSecure should be true")
	}
}
