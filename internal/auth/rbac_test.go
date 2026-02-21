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

func TestMatchesEmail(t *testing.T) {
	r := &ConfigRoleResolver{}

	tests := []struct {
		name    string
		user    *models.User
		pattern string
		want    bool
	}{
		{
			name:    "exact match",
			user:    &models.User{Email: "admin@example.com"},
			pattern: "admin@example.com",
			want:    true,
		},
		{
			name:    "no match",
			user:    &models.User{Email: "user@other.com"},
			pattern: "admin@example.com",
			want:    false,
		},
		{
			name:    "wildcard domain match",
			user:    &models.User{Email: "anyone@example.com"},
			pattern: "*@example.com",
			want:    true,
		},
		{
			name:    "wildcard domain no match",
			user:    &models.User{Email: "anyone@other.com"},
			pattern: "*@example.com",
			want:    false,
		},
		{
			name:    "empty email",
			user:    &models.User{Email: ""},
			pattern: "*@example.com",
			want:    false,
		},
		{
			name:    "wildcard prefix",
			user:    &models.User{Email: "admin@platform.example.com"},
			pattern: "*@platform.example.com",
			want:    true,
		},
		{
			name:    "partial wildcard",
			user:    &models.User{Email: "admin-team@example.com"},
			pattern: "admin-*@example.com",
			want:    true,
		},
		{
			name:    "exact match same string",
			user:    &models.User{Email: "test@test.com"},
			pattern: "test@test.com",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.matchesEmail(tt.user, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesEmail(%q, %q) = %v, want %v", tt.user.Email, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchesGitHubTeam(t *testing.T) {
	r := &ConfigRoleResolver{}

	tests := []struct {
		name    string
		user    *models.User
		pattern string
		want    bool
	}{
		{
			name: "matching team",
			user: &models.User{
				Provider: "github",
				Groups:   []string{"myorg/platform-team"},
			},
			pattern: "@myorg/platform-team",
			want:    true,
		},
		{
			name: "no matching team",
			user: &models.User{
				Provider: "github",
				Groups:   []string{"myorg/other-team"},
			},
			pattern: "@myorg/platform-team",
			want:    false,
		},
		{
			name: "non-github provider",
			user: &models.User{
				Provider: "gitlab",
				Groups:   []string{"myorg/platform-team"},
			},
			pattern: "@myorg/platform-team",
			want:    false,
		},
		{
			name: "empty groups",
			user: &models.User{
				Provider: "github",
				Groups:   nil,
			},
			pattern: "@myorg/platform-team",
			want:    false,
		},
		{
			name: "multiple groups one matches",
			user: &models.User{
				Provider: "github",
				Groups:   []string{"org/team-a", "org/team-b", "org/team-c"},
			},
			pattern: "@org/team-b",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.matchesGitHubTeam(tt.user, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesGitHubTeam() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesGroup(t *testing.T) {
	r := &ConfigRoleResolver{}

	tests := []struct {
		name      string
		user      *models.User
		groupName string
		want      bool
	}{
		{
			name:      "matching group",
			user:      &models.User{Groups: []string{"admin", "developers"}},
			groupName: "admin",
			want:      true,
		},
		{
			name:      "no matching group",
			user:      &models.User{Groups: []string{"developers"}},
			groupName: "admin",
			want:      false,
		},
		{
			name:      "empty groups",
			user:      &models.User{Groups: nil},
			groupName: "admin",
			want:      false,
		},
		{
			name:      "exact match required",
			user:      &models.User{Groups: []string{"admin-team"}},
			groupName: "admin",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.matchesGroup(tt.user, tt.groupName)
			if got != tt.want {
				t.Errorf("matchesGroup(%q) = %v, want %v", tt.groupName, got, tt.want)
			}
		})
	}
}

func TestMatchesAssignment(t *testing.T) {
	r := &ConfigRoleResolver{}

	tests := []struct {
		name       string
		user       *models.User
		assignment RoleAssignment
		want       bool
	}{
		{
			name: "email pattern match",
			user: &models.User{Email: "admin@example.com", Provider: "github"},
			assignment: RoleAssignment{
				Pattern: "admin@example.com",
				Role:    "admin",
			},
			want: true,
		},
		{
			name: "email wildcard match",
			user: &models.User{Email: "user@example.com", Provider: "github"},
			assignment: RoleAssignment{
				Pattern: "*@example.com",
				Role:    "admin",
			},
			want: true,
		},
		{
			name: "github team match",
			user: &models.User{Provider: "github", Groups: []string{"org/admins"}},
			assignment: RoleAssignment{
				Pattern: "@org/admins",
				Role:    "admin",
			},
			want: true,
		},
		{
			name: "group match",
			user: &models.User{Provider: "oidc", Groups: []string{"platform-admins"}},
			assignment: RoleAssignment{
				Pattern: "platform-admins",
				Role:    "admin",
			},
			want: true,
		},
		{
			name: "provider filter matches",
			user: &models.User{Email: "admin@example.com", Provider: "github"},
			assignment: RoleAssignment{
				Pattern:  "admin@example.com",
				Role:     "admin",
				Provider: "github",
			},
			want: true,
		},
		{
			name: "provider filter does not match",
			user: &models.User{Email: "admin@example.com", Provider: "gitlab"},
			assignment: RoleAssignment{
				Pattern:  "admin@example.com",
				Role:     "admin",
				Provider: "github",
			},
			want: false,
		},
		{
			name: "no provider filter - matches any provider",
			user: &models.User{Email: "admin@example.com", Provider: "oidc"},
			assignment: RoleAssignment{
				Pattern: "admin@example.com",
				Role:    "admin",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.matchesAssignment(tt.user, tt.assignment)
			if got != tt.want {
				t.Errorf("matchesAssignment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewConfigRoleResolver(t *testing.T) {
	tests := []struct {
		name            string
		defaultRole     models.Role
		wantDefaultRole models.Role
	}{
		{
			name:            "valid default role",
			defaultRole:     models.RoleAdmin,
			wantDefaultRole: models.RoleAdmin,
		},
		{
			name:            "invalid default role falls back to user",
			defaultRole:     models.Role("superuser"),
			wantDefaultRole: models.RoleUser,
		},
		{
			name:            "empty default role falls back to user",
			defaultRole:     models.Role(""),
			wantDefaultRole: models.RoleUser,
		},
		{
			name:            "user default role",
			defaultRole:     models.RoleUser,
			wantDefaultRole: models.RoleUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewConfigRoleResolver(nil, tt.defaultRole, nil)
			if r.defaultRole != tt.wantDefaultRole {
				t.Errorf("defaultRole = %q, want %q", r.defaultRole, tt.wantDefaultRole)
			}
		})
	}
}

func TestResolveRole_WithNilStore(t *testing.T) {
	tests := []struct {
		name        string
		assignments []RoleAssignment
		defaultRole models.Role
		user        *models.User
		wantRole    models.Role
	}{
		{
			name:        "no assignments returns default",
			assignments: nil,
			defaultRole: models.RoleUser,
			user:        &models.User{Email: "user@example.com", Provider: "github"},
			wantRole:    models.RoleUser,
		},
		{
			name: "first matching assignment wins",
			assignments: []RoleAssignment{
				{Pattern: "*@example.com", Role: "admin"},
				{Pattern: "user@example.com", Role: "user"},
			},
			defaultRole: models.RoleUser,
			user:        &models.User{Email: "user@example.com", Provider: "github"},
			wantRole:    models.RoleAdmin,
		},
		{
			name: "no matching assignment returns default",
			assignments: []RoleAssignment{
				{Pattern: "*@other.com", Role: "admin"},
			},
			defaultRole: models.RoleUser,
			user:        &models.User{Email: "user@example.com", Provider: "github"},
			wantRole:    models.RoleUser,
		},
		{
			name: "github team assignment",
			assignments: []RoleAssignment{
				{Pattern: "@myorg/admins", Role: "admin"},
			},
			defaultRole: models.RoleUser,
			user: &models.User{
				Provider: "github",
				Groups:   []string{"myorg/admins"},
			},
			wantRole: models.RoleAdmin,
		},
		{
			name: "group assignment",
			assignments: []RoleAssignment{
				{Pattern: "platform-admins", Role: "admin"},
			},
			defaultRole: models.RoleUser,
			user: &models.User{
				Provider: "oidc",
				Groups:   []string{"platform-admins"},
			},
			wantRole: models.RoleAdmin,
		},
		{
			name: "invalid role in assignment returns default",
			assignments: []RoleAssignment{
				{Pattern: "*@example.com", Role: "invalid-role"},
			},
			defaultRole: models.RoleUser,
			user:        &models.User{Email: "user@example.com", Provider: "github"},
			wantRole:    models.RoleUser,
		},
		{
			name: "provider filter restricts match",
			assignments: []RoleAssignment{
				{Pattern: "*@example.com", Role: "admin", Provider: "oidc"},
			},
			defaultRole: models.RoleUser,
			user:        &models.User{Email: "user@example.com", Provider: "github"},
			wantRole:    models.RoleUser,
		},
		{
			name: "provider filter allows match",
			assignments: []RoleAssignment{
				{Pattern: "*@example.com", Role: "admin", Provider: "github"},
			},
			defaultRole: models.RoleUser,
			user:        &models.User{Email: "user@example.com", Provider: "github"},
			wantRole:    models.RoleAdmin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewConfigRoleResolver(tt.assignments, tt.defaultRole, nil)
			got := r.ResolveRole(context.Background(), tt.user)
			if got != tt.wantRole {
				t.Errorf("ResolveRole() = %q, want %q", got, tt.wantRole)
			}
		})
	}
}

func TestSetUserRole_NilStore(t *testing.T) {
	r := NewConfigRoleResolver(nil, models.RoleUser, nil)
	err := r.SetUserRole(context.Background(), "user1", models.RoleAdmin)
	if err != nil {
		t.Errorf("SetUserRole() with nil store should return nil, got %v", err)
	}
}

func TestGetUserRole_NilStore(t *testing.T) {
	r := NewConfigRoleResolver(nil, models.RoleUser, nil)
	role, found, err := r.GetUserRole(context.Background(), "user1")
	if err != nil {
		t.Errorf("GetUserRole() with nil store unexpected error: %v", err)
	}
	if found {
		t.Error("GetUserRole() with nil store should return found=false")
	}
	if role != "" {
		t.Errorf("GetUserRole() with nil store should return empty role, got %q", role)
	}
}

func TestDeleteUserRole_NilStore(t *testing.T) {
	r := NewConfigRoleResolver(nil, models.RoleUser, nil)
	err := r.DeleteUserRole(context.Background(), "user1")
	if err != nil {
		t.Errorf("DeleteUserRole() with nil store should return nil, got %v", err)
	}
}

func TestResolveRole_WithRedisStore(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = client.Close() })

	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	// Set a persistent role override
	err := store.SetUserRole(ctx, "github:42", models.RoleAdmin)
	if err != nil {
		t.Fatalf("SetUserRole() error: %v", err)
	}

	r := NewConfigRoleResolver(nil, models.RoleUser, store)

	user := &models.User{
		ID:       "github:42",
		Login:    "overridden",
		Email:    "user@example.com",
		Provider: "github",
	}

	// Should return admin from persistent override
	got := r.ResolveRole(ctx, user)
	if got != models.RoleAdmin {
		t.Errorf("ResolveRole() = %q, want %q (persistent override)", got, models.RoleAdmin)
	}
}

func TestResolveRole_PersistentOverrideTakesPrecedence(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = client.Close() })

	store := NewRedisSessionStore(client, time.Hour)
	ctx := context.Background()

	// Set persistent override to admin
	err := store.SetUserRole(ctx, "github:50", models.RoleAdmin)
	if err != nil {
		t.Fatalf("SetUserRole() error: %v", err)
	}

	// Assignments would give "user" role
	assignments := []RoleAssignment{
		{Pattern: "*@example.com", Role: "user"},
	}
	r := NewConfigRoleResolver(assignments, models.RoleUser, store)

	user := &models.User{
		ID:       "github:50",
		Login:    "override-test",
		Email:    "test@example.com",
		Provider: "github",
	}

	// Persistent override should win over assignment
	got := r.ResolveRole(ctx, user)
	if got != models.RoleAdmin {
		t.Errorf("ResolveRole() = %q, want %q (persistent override should take precedence)", got, models.RoleAdmin)
	}
}

func TestSetUserRole_WithRedisStore(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = client.Close() })

	store := NewRedisSessionStore(client, time.Hour)
	r := NewConfigRoleResolver(nil, models.RoleUser, store)
	ctx := context.Background()

	err := r.SetUserRole(ctx, "github:60", models.RoleAdmin)
	if err != nil {
		t.Fatalf("SetUserRole() error: %v", err)
	}

	role, found, err := r.GetUserRole(ctx, "github:60")
	if err != nil {
		t.Fatalf("GetUserRole() error: %v", err)
	}
	if !found {
		t.Error("expected found=true")
	}
	if role != models.RoleAdmin {
		t.Errorf("role = %q, want %q", role, models.RoleAdmin)
	}
}

func TestDeleteUserRole_WithRedisStore(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = client.Close() })

	store := NewRedisSessionStore(client, time.Hour)
	r := NewConfigRoleResolver(nil, models.RoleUser, store)
	ctx := context.Background()

	_ = r.SetUserRole(ctx, "github:70", models.RoleAdmin)

	err := r.DeleteUserRole(ctx, "github:70")
	if err != nil {
		t.Fatalf("DeleteUserRole() error: %v", err)
	}

	_, found, err := r.GetUserRole(ctx, "github:70")
	if err != nil {
		t.Fatalf("GetUserRole() error: %v", err)
	}
	if found {
		t.Error("expected found=false after deletion")
	}
}
