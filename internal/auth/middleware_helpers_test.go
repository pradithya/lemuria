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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/models"
)

func newTestMiddleware(t *testing.T) (*Middleware, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() {
		_ = client.Close()
	})

	store := NewRedisSessionStore(client, time.Hour)
	resolver := NewConfigRoleResolver(nil, models.RoleUser, store)
	m := NewMiddleware(store, resolver, "example.com", true)
	return m, mr
}

func TestMiddleware_Authenticate_WithValidCookie(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	// Create a session
	user := &models.User{
		ID:       "github:100",
		Login:    "cookieuser",
		Email:    "cookie@example.com",
		Provider: "github",
		Role:     models.RoleUser,
	}
	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	// Build request with session cookie
	var capturedUser *models.User
	var capturedSession *models.Session
	handler := m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromRequest(r)
		capturedSession = SessionFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if capturedUser == nil {
		t.Fatal("expected user in context")
	}
	if capturedUser.Login != "cookieuser" {
		t.Errorf("user.Login = %q, want %q", capturedUser.Login, "cookieuser")
	}
	if capturedSession == nil {
		t.Fatal("expected session in context")
	}
	if capturedSession.ID != session.ID {
		t.Errorf("session.ID = %q, want %q", capturedSession.ID, session.ID)
	}
}

func TestMiddleware_Authenticate_WithBearerToken(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:200",
		Login:    "beareruser",
		Provider: "github",
		Role:     models.RoleAdmin,
	}
	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	var capturedUser *models.User
	handler := m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/api/data", nil)
	r.Header.Set("Authorization", "Bearer "+session.ID)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if capturedUser == nil {
		t.Fatal("expected user in context from bearer token")
	}
	if capturedUser.Login != "beareruser" {
		t.Errorf("user.Login = %q, want %q", capturedUser.Login, "beareruser")
	}
}

func TestMiddleware_Authenticate_NoCredentials(t *testing.T) {
	m, _ := newTestMiddleware(t)

	var capturedUser *models.User
	handler := m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/public", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (middleware should not block)", w.Code, http.StatusOK)
	}
	if capturedUser != nil {
		t.Error("expected nil user when no credentials provided")
	}
}

func TestMiddleware_Authenticate_InvalidSession(t *testing.T) {
	m, _ := newTestMiddleware(t)

	var capturedUser *models.User
	handler := m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "invalid-session-id"})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (Authenticate should not block)", w.Code, http.StatusOK)
	}
	if capturedUser != nil {
		t.Error("expected nil user for invalid session")
	}
}

func TestMiddleware_Authenticate_ExpiredSession(t *testing.T) {
	m, mr := newTestMiddleware(t)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:300",
		Login:    "expireduser",
		Provider: "github",
	}
	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	// Fast-forward past session expiry
	mr.FastForward(2 * time.Hour)

	var capturedUser *models.User
	handler := m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if capturedUser != nil {
		t.Error("expected nil user for expired session")
	}
}

func TestMiddleware_getSessionFromRequest_Cookie(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:400",
		Login:    "fromcookie",
		Provider: "github",
	}
	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	r := httptest.NewRequest("GET", "/test", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID})

	got := m.getSessionFromRequest(r)
	if got == nil {
		t.Fatal("expected session from cookie")
	}
	if got.ID != session.ID {
		t.Errorf("session.ID = %q, want %q", got.ID, session.ID)
	}
}

func TestMiddleware_getSessionFromRequest_BearerToken(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:500",
		Login:    "frombearer",
		Provider: "github",
	}
	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Authorization", "Bearer "+session.ID)

	got := m.getSessionFromRequest(r)
	if got == nil {
		t.Fatal("expected session from bearer token")
	}
	if got.ID != session.ID {
		t.Errorf("session.ID = %q, want %q", got.ID, session.ID)
	}
}

func TestMiddleware_getSessionFromRequest_NoCreds(t *testing.T) {
	m, _ := newTestMiddleware(t)

	r := httptest.NewRequest("GET", "/test", nil)
	got := m.getSessionFromRequest(r)
	if got != nil {
		t.Error("expected nil session when no credentials")
	}
}

func TestMiddleware_getSessionFromRequest_InvalidBearerToken(t *testing.T) {
	m, _ := newTestMiddleware(t)

	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Authorization", "Bearer invalid-token-123")

	got := m.getSessionFromRequest(r)
	if got != nil {
		t.Error("expected nil session for invalid bearer token")
	}
}

func TestMiddleware_CreateSession(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:600",
		Login:    "newsession",
		Email:    "new@example.com",
		Provider: "github",
	}

	w := httptest.NewRecorder()
	session, err := m.CreateSession(ctx, w, user)
	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}
	if session == nil {
		t.Fatal("CreateSession() returned nil session")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}

	// Check that cookie was set
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			found = true
			if c.Value != session.ID {
				t.Errorf("cookie value = %q, want %q", c.Value, session.ID)
			}
		}
	}
	if !found {
		t.Error("session cookie not set in response")
	}

	// Verify session is retrievable
	got, err := m.sessionStore.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got == nil {
		t.Error("session should be retrievable after creation")
	}
}

func TestMiddleware_DestroySession(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:700",
		Login:    "destroyme",
		Provider: "github",
	}

	// Create session
	session, err := m.sessionStore.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	// Build request with session in context
	r := httptest.NewRequest("POST", "/auth/logout", nil)
	sessionCtx := WithSession(r.Context(), session)
	r = r.WithContext(sessionCtx)

	w := httptest.NewRecorder()
	err = m.DestroySession(ctx, w, r)
	if err != nil {
		t.Fatalf("DestroySession() error: %v", err)
	}

	// Cookie should be cleared
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == SessionCookieName && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Error("session cookie should be cleared")
	}

	// Session should be gone from store
	got, err := m.sessionStore.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() after destroy error: %v", err)
	}
	if got != nil {
		t.Error("session should be nil after destroy")
	}
}

func TestMiddleware_DestroySession_NoSession(t *testing.T) {
	m, _ := newTestMiddleware(t)
	ctx := context.Background()

	r := httptest.NewRequest("POST", "/auth/logout", nil)
	w := httptest.NewRecorder()

	err := m.DestroySession(ctx, w, r)
	if err != nil {
		t.Fatalf("DestroySession() with no session should not error: %v", err)
	}

	// Cookie should still be cleared
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == SessionCookieName && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Error("session cookie should be cleared even when no session in context")
	}
}

func TestMiddleware_Authenticate_RoleResolution(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = client.Close() })

	store := NewRedisSessionStore(client, time.Hour)

	// Configure role resolver with assignments
	assignments := []RoleAssignment{
		{Pattern: "*@admin.example.com", Role: "admin"},
	}
	resolver := NewConfigRoleResolver(assignments, models.RoleUser, store)
	m := NewMiddleware(store, resolver, "", false)

	// Create user who should match the admin pattern
	user := &models.User{
		ID:       "github:800",
		Login:    "adminuser",
		Email:    "boss@admin.example.com",
		Provider: "github",
		Role:     models.RoleUser, // Original role
	}
	session, err := store.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("Create session error: %v", err)
	}

	var capturedUser *models.User
	handler := m.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/dashboard", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if capturedUser == nil {
		t.Fatal("expected user in context")
	}
	// The role should be resolved to admin by the role resolver
	if capturedUser.Role != models.RoleAdmin {
		t.Errorf("user.Role = %q, want %q (should be resolved by middleware)", capturedUser.Role, models.RoleAdmin)
	}
}
