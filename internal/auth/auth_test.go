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

	"github.com/org/lemuria/internal/models"
)

func TestUserFromContext(t *testing.T) {
	tests := []struct {
		name    string
		user    *models.User
		wantNil bool
		wantID  string
	}{
		{
			name:    "no user in context",
			user:    nil,
			wantNil: true,
		},
		{
			name:    "user present in context",
			user:    &models.User{ID: "github:123", Login: "testuser"},
			wantNil: false,
			wantID:  "github:123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != nil {
				ctx = WithUser(ctx, tt.user)
			}
			got := UserFromContext(ctx)
			if tt.wantNil {
				if got != nil {
					t.Errorf("UserFromContext() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("UserFromContext() = nil, want non-nil")
				}
				if got.ID != tt.wantID {
					t.Errorf("UserFromContext().ID = %q, want %q", got.ID, tt.wantID)
				}
			}
		})
	}
}

func TestSessionFromContext(t *testing.T) {
	tests := []struct {
		name    string
		session *models.Session
		wantNil bool
		wantID  string
	}{
		{
			name:    "no session in context",
			session: nil,
			wantNil: true,
		},
		{
			name:    "session present in context",
			session: &models.Session{ID: "sess-abc"},
			wantNil: false,
			wantID:  "sess-abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.session != nil {
				ctx = WithSession(ctx, tt.session)
			}
			got := SessionFromContext(ctx)
			if tt.wantNil {
				if got != nil {
					t.Errorf("SessionFromContext() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Fatal("SessionFromContext() = nil, want non-nil")
				}
				if got.ID != tt.wantID {
					t.Errorf("SessionFromContext().ID = %q, want %q", got.ID, tt.wantID)
				}
			}
		})
	}
}

func TestUserFromContext_Roundtrip(t *testing.T) {
	user := &models.User{
		ID:       "github:42",
		Login:    "roundtrip",
		Email:    "rt@example.com",
		Provider: "github",
		Role:     models.RoleAdmin,
	}

	ctx := WithUser(context.Background(), user)
	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil user")
	}
	if got != user {
		t.Error("expected same user pointer from roundtrip")
	}
}

func TestSessionFromContext_Roundtrip(t *testing.T) {
	session := &models.Session{
		ID:     "sess-roundtrip",
		UserID: "github:42",
	}

	ctx := WithSession(context.Background(), session)
	got := SessionFromContext(ctx)
	if got == nil {
		t.Fatal("expected non-nil session")
	}
	if got != session {
		t.Error("expected same session pointer from roundtrip")
	}
}

func TestUserFromRequest(t *testing.T) {
	tests := []struct {
		name    string
		user    *models.User
		wantNil bool
	}{
		{
			name:    "no user in request",
			user:    nil,
			wantNil: true,
		},
		{
			name:    "user present in request",
			user:    &models.User{ID: "github:99", Login: "requser"},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != nil {
				ctx = WithUser(ctx, tt.user)
			}
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			got := UserFromRequest(r)
			if tt.wantNil && got != nil {
				t.Errorf("UserFromRequest() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("UserFromRequest() = nil, want non-nil")
			}
		})
	}
}

func TestSessionFromRequest(t *testing.T) {
	tests := []struct {
		name    string
		session *models.Session
		wantNil bool
	}{
		{
			name:    "no session in request",
			session: nil,
			wantNil: true,
		},
		{
			name:    "session present in request",
			session: &models.Session{ID: "sess-req"},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.session != nil {
				ctx = WithSession(ctx, tt.session)
			}
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			got := SessionFromRequest(r)
			if tt.wantNil && got != nil {
				t.Errorf("SessionFromRequest() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("SessionFromRequest() = nil, want non-nil")
			}
		})
	}
}

func TestIsAuthenticated(t *testing.T) {
	tests := []struct {
		name string
		user *models.User
		want bool
	}{
		{
			name: "no user - not authenticated",
			user: nil,
			want: false,
		},
		{
			name: "user present - authenticated",
			user: &models.User{ID: "github:1"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != nil {
				ctx = WithUser(ctx, tt.user)
			}
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			got := IsAuthenticated(r)
			if got != tt.want {
				t.Errorf("IsAuthenticated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAdmin(t *testing.T) {
	tests := []struct {
		name string
		user *models.User
		want bool
	}{
		{
			name: "no user - not admin",
			user: nil,
			want: false,
		},
		{
			name: "user role - not admin",
			user: &models.User{ID: "github:1", Role: models.RoleUser},
			want: false,
		},
		{
			name: "admin role - is admin",
			user: &models.User{ID: "github:1", Role: models.RoleAdmin},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != nil {
				ctx = WithUser(ctx, tt.user)
			}
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			got := IsAdmin(r)
			if got != tt.want {
				t.Errorf("IsAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasPermission(t *testing.T) {
	tests := []struct {
		name       string
		user       *models.User
		permission string
		want       bool
	}{
		{
			name:       "no user - no permission",
			user:       nil,
			permission: "read",
			want:       false,
		},
		{
			name:       "user role has read",
			user:       &models.User{ID: "u1", Role: models.RoleUser},
			permission: "read",
			want:       true,
		},
		{
			name:       "user role does not have write",
			user:       &models.User{ID: "u1", Role: models.RoleUser},
			permission: "write",
			want:       false,
		},
		{
			name:       "admin role has write",
			user:       &models.User{ID: "u1", Role: models.RoleAdmin},
			permission: "write",
			want:       true,
		},
		{
			name:       "admin role has admin",
			user:       &models.User{ID: "u1", Role: models.RoleAdmin},
			permission: "admin",
			want:       true,
		},
		{
			name:       "admin role has delete",
			user:       &models.User{ID: "u1", Role: models.RoleAdmin},
			permission: "delete",
			want:       true,
		},
		{
			name:       "user role does not have admin",
			user:       &models.User{ID: "u1", Role: models.RoleUser},
			permission: "admin",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != nil {
				ctx = WithUser(ctx, tt.user)
			}
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			got := HasPermission(r, tt.permission)
			if got != tt.want {
				t.Errorf("HasPermission(%q) = %v, want %v", tt.permission, got, tt.want)
			}
		})
	}
}

func TestWithUser_OverwritesPrevious(t *testing.T) {
	user1 := &models.User{ID: "u1"}
	user2 := &models.User{ID: "u2"}

	ctx := WithUser(context.Background(), user1)
	ctx = WithUser(ctx, user2)

	got := UserFromContext(ctx)
	if got == nil || got.ID != "u2" {
		t.Errorf("expected user2 after overwrite, got %v", got)
	}
}

func TestWithSession_OverwritesPrevious(t *testing.T) {
	s1 := &models.Session{ID: "s1"}
	s2 := &models.Session{ID: "s2"}

	ctx := WithSession(context.Background(), s1)
	ctx = WithSession(ctx, s2)

	got := SessionFromContext(ctx)
	if got == nil || got.ID != "s2" {
		t.Errorf("expected s2 after overwrite, got %v", got)
	}
}

func TestUserFromContext_WrongType(t *testing.T) {
	// If the context has a value of wrong type under the key, should return nil.
	ctx := context.WithValue(context.Background(), UserContextKey, "not-a-user")
	got := UserFromContext(ctx)
	if got != nil {
		t.Errorf("expected nil for wrong type, got %v", got)
	}
}

func TestSessionFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), SessionContextKey, 12345)
	got := SessionFromContext(ctx)
	if got != nil {
		t.Errorf("expected nil for wrong type, got %v", got)
	}
}

func TestContextKeys_BothCoexist(t *testing.T) {
	user := &models.User{ID: "u1", Login: "coexist"}
	session := &models.Session{ID: "s1", UserID: "u1"}

	ctx := WithUser(context.Background(), user)
	ctx = WithSession(ctx, session)

	gotUser := UserFromContext(ctx)
	gotSession := SessionFromContext(ctx)

	if gotUser == nil || gotUser.ID != "u1" {
		t.Error("user should be present alongside session")
	}
	if gotSession == nil || gotSession.ID != "s1" {
		t.Error("session should be present alongside user")
	}
}

func TestIsAuthenticated_ViaRequest(t *testing.T) {
	// Verify that IsAuthenticated works via the HTTP request path.
	user := &models.User{ID: "u1", Role: models.RoleUser}
	ctx := WithUser(context.Background(), user)
	r := &http.Request{}
	r = r.WithContext(ctx)

	if !IsAuthenticated(r) {
		t.Error("expected authenticated")
	}
}
