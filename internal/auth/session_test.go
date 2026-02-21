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
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/org/lemuria/internal/models"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return mr, client
}

func TestNewRedisSessionStore(t *testing.T) {
	_, client := newTestRedis(t)

	tests := []struct {
		name    string
		ttl     time.Duration
		wantTTL time.Duration
	}{
		{
			name:    "default TTL when zero",
			ttl:     0,
			wantTTL: 24 * time.Hour,
		},
		{
			name:    "custom TTL",
			ttl:     2 * time.Hour,
			wantTTL: 2 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewRedisSessionStore(client, tt.ttl)
			if store == nil {
				t.Fatal("NewRedisSessionStore() returned nil")
			}
			if store.sessionTTL != tt.wantTTL {
				t.Errorf("sessionTTL = %v, want %v", store.sessionTTL, tt.wantTTL)
			}
		})
	}
}

func TestRedisSessionStore_Create(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	user := &models.User{
		ID:       "github:123",
		Login:    "testuser",
		Email:    "test@example.com",
		Provider: "github",
		Role:     models.RoleUser,
	}

	session, err := store.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if session == nil {
		t.Fatal("Create() returned nil session")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
	if session.UserID != "github:123" {
		t.Errorf("UserID = %q, want %q", session.UserID, "github:123")
	}
	if session.User == nil {
		t.Fatal("session User should not be nil")
	}
	if session.User.Login != "testuser" {
		t.Errorf("User.Login = %q, want %q", session.User.Login, "testuser")
	}
	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if session.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should not be zero")
	}
	if !session.ExpiresAt.After(session.CreatedAt) {
		t.Error("ExpiresAt should be after CreatedAt")
	}
}

func TestRedisSessionStore_Get(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:456",
		Login:    "getuser",
		Provider: "github",
		Role:     models.RoleAdmin,
	}

	// Create a session first
	created, err := store.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Get the session
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil for existing session")
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.UserID != "github:456" {
		t.Errorf("UserID = %q, want %q", got.UserID, "github:456")
	}
	if got.User.Login != "getuser" {
		t.Errorf("User.Login = %q, want %q", got.User.Login, "getuser")
	}
}

func TestRedisSessionStore_Get_NotFound(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	got, err := store.Get(context.Background(), "nonexistent-session-id")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != nil {
		t.Errorf("Get() = %v, want nil for nonexistent session", got)
	}
}

func TestRedisSessionStore_Get_Expired(t *testing.T) {
	mr, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Second)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:789",
		Login:    "expireduser",
		Provider: "github",
	}

	created, err := store.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Fast forward time in miniredis to expire the session
	mr.FastForward(2 * time.Second)

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != nil {
		t.Errorf("Get() should return nil for expired session, got %v", got)
	}
}

func TestRedisSessionStore_Delete(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:111",
		Login:    "deleteuser",
		Provider: "github",
	}

	created, err := store.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Delete the session
	err = store.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Verify it's gone
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after Delete error: %v", err)
	}
	if got != nil {
		t.Error("session should be nil after deletion")
	}
}

func TestRedisSessionStore_Refresh(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	user := &models.User{
		ID:       "github:222",
		Login:    "refreshuser",
		Provider: "github",
	}

	created, err := store.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	originalExpiry := created.ExpiresAt

	// Small delay to ensure time difference
	time.Sleep(10 * time.Millisecond)

	err = store.Refresh(ctx, created.ID)
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after Refresh error: %v", err)
	}
	if got == nil {
		t.Fatal("session should exist after refresh")
	}
	if !got.ExpiresAt.After(originalExpiry) {
		t.Errorf("ExpiresAt should be extended after refresh: got %v, original %v", got.ExpiresAt, originalExpiry)
	}
}

func TestRedisSessionStore_Refresh_NotFound(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	err := store.Refresh(context.Background(), "nonexistent-session")
	if err == nil {
		t.Error("Refresh() should return error for nonexistent session")
	}
}

func TestRedisSessionStore_ListAll(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	// Create multiple sessions
	users := []*models.User{
		{ID: "github:1", Login: "user1", Provider: "github"},
		{ID: "github:2", Login: "user2", Provider: "github"},
		{ID: "github:3", Login: "user3", Provider: "github"},
	}

	for _, u := range users {
		_, err := store.Create(ctx, u)
		if err != nil {
			t.Fatalf("Create() error: %v", err)
		}
	}

	sessions, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("ListAll() returned %d sessions, want 3", len(sessions))
	}
}

func TestRedisSessionStore_ListAll_Empty(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	sessions, err := store.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll() error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("ListAll() returned %d sessions, want 0", len(sessions))
	}
}

func TestRedisSessionStore_CreateState(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	state, err := store.CreateState(context.Background(), "https://example.com/callback")
	if err != nil {
		t.Fatalf("CreateState() error: %v", err)
	}
	if state == nil {
		t.Fatal("CreateState() returned nil")
	}
	if state.State == "" {
		t.Error("State should not be empty")
	}
	if state.RedirectURL != "https://example.com/callback" {
		t.Errorf("RedirectURL = %q, want %q", state.RedirectURL, "https://example.com/callback")
	}
	if state.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestRedisSessionStore_ValidateState(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	// Create a state
	created, err := store.CreateState(ctx, "https://example.com/redirect")
	if err != nil {
		t.Fatalf("CreateState() error: %v", err)
	}

	// Validate it
	validated, err := store.ValidateState(ctx, created.State)
	if err != nil {
		t.Fatalf("ValidateState() error: %v", err)
	}
	if validated == nil {
		t.Fatal("ValidateState() returned nil")
	}
	if validated.RedirectURL != "https://example.com/redirect" {
		t.Errorf("RedirectURL = %q, want %q", validated.RedirectURL, "https://example.com/redirect")
	}

	// Validate again should fail (one-time use)
	_, err = store.ValidateState(ctx, created.State)
	if err == nil {
		t.Error("ValidateState() should fail on second use (one-time token)")
	}
}

func TestRedisSessionStore_ValidateState_Invalid(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	_, err := store.ValidateState(context.Background(), "nonexistent-state")
	if err == nil {
		t.Error("ValidateState() should return error for nonexistent state")
	}
}

func TestRedisSessionStore_SetUserRole(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	err := store.SetUserRole(ctx, "github:123", models.RoleAdmin)
	if err != nil {
		t.Fatalf("SetUserRole() error: %v", err)
	}

	role, found, err := store.GetUserRole(ctx, "github:123")
	if err != nil {
		t.Fatalf("GetUserRole() error: %v", err)
	}
	if !found {
		t.Error("GetUserRole() found = false, want true")
	}
	if role != models.RoleAdmin {
		t.Errorf("role = %q, want %q", role, models.RoleAdmin)
	}
}

func TestRedisSessionStore_GetUserRole_NotFound(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)

	role, found, err := store.GetUserRole(context.Background(), "nonexistent-user")
	if err != nil {
		t.Fatalf("GetUserRole() error: %v", err)
	}
	if found {
		t.Error("GetUserRole() found = true, want false for nonexistent user")
	}
	if role != "" {
		t.Errorf("role = %q, want empty", role)
	}
}

func TestRedisSessionStore_DeleteUserRole(t *testing.T) {
	_, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	// Set then delete
	err := store.SetUserRole(ctx, "github:999", models.RoleAdmin)
	if err != nil {
		t.Fatalf("SetUserRole() error: %v", err)
	}

	err = store.DeleteUserRole(ctx, "github:999")
	if err != nil {
		t.Fatalf("DeleteUserRole() error: %v", err)
	}

	_, found, err := store.GetUserRole(ctx, "github:999")
	if err != nil {
		t.Fatalf("GetUserRole() after delete error: %v", err)
	}
	if found {
		t.Error("role should not be found after deletion")
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1, err := generateSessionID()
	if err != nil {
		t.Fatalf("generateSessionID() error: %v", err)
	}
	if id1 == "" {
		t.Error("generateSessionID() returned empty string")
	}
	// Should be 64 hex characters (32 bytes)
	if len(id1) != 64 {
		t.Errorf("generateSessionID() length = %d, want 64", len(id1))
	}

	// Two calls should produce different IDs
	id2, err := generateSessionID()
	if err != nil {
		t.Fatalf("generateSessionID() second call error: %v", err)
	}
	if id1 == id2 {
		t.Error("two calls to generateSessionID() should produce different IDs")
	}
}

func TestRedisSessionStore_ListAll_CleansStaleEntries(t *testing.T) {
	mr, client := newTestRedis(t)
	store := NewRedisSessionStore(client, time.Second)
	ctx := context.Background()

	// Create a session
	user := &models.User{ID: "github:1", Login: "staleuser", Provider: "github"}
	_, err := store.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Expire the session data in Redis but keep the index entry
	mr.FastForward(2 * time.Second)

	sessions, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error: %v", err)
	}
	// The expired session should be cleaned up from the result
	if len(sessions) != 0 {
		t.Errorf("ListAll() returned %d sessions, want 0 (expired should be cleaned)", len(sessions))
	}
}
