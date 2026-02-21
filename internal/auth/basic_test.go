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

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

func TestNewBasicProvider(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.BasicAuthConfig
		wantUsers int
	}{
		{
			name:      "empty config",
			cfg:       &config.BasicAuthConfig{},
			wantUsers: 0,
		},
		{
			name: "single user",
			cfg: &config.BasicAuthConfig{
				Users: []config.BasicAuthUser{
					{Username: "admin", Password: "pass", Role: "admin"},
				},
			},
			wantUsers: 1,
		},
		{
			name: "multiple users",
			cfg: &config.BasicAuthConfig{
				Users: []config.BasicAuthUser{
					{Username: "admin", Password: "pass1", Role: "admin"},
					{Username: "viewer", Password: "pass2", Role: "user"},
				},
			},
			wantUsers: 2,
		},
		{
			name: "invalid role defaults to user",
			cfg: &config.BasicAuthConfig{
				Users: []config.BasicAuthUser{
					{Username: "dev", Password: "pass", Role: "invalid-role"},
				},
			},
			wantUsers: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewBasicProvider(tt.cfg)
			if p == nil {
				t.Fatal("NewBasicProvider() returned nil")
			}
			if len(p.users) != tt.wantUsers {
				t.Errorf("len(users) = %d, want %d", len(p.users), tt.wantUsers)
			}
		})
	}
}

func TestNewBasicProvider_InvalidRoleDefaultsToUser(t *testing.T) {
	cfg := &config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "dev", Password: "pass", Role: "bogus"},
		},
	}
	p := NewBasicProvider(cfg)
	u, ok := p.users["dev"]
	if !ok {
		t.Fatal("expected user 'dev' to exist")
	}
	if u.role != models.RoleUser {
		t.Errorf("expected role %q for invalid input, got %q", models.RoleUser, u.role)
	}
}

func TestBasicProvider_Name(t *testing.T) {
	p := NewBasicProvider(&config.BasicAuthConfig{})
	if got := p.Name(); got != "basic" {
		t.Errorf("Name() = %q, want %q", got, "basic")
	}
}

func TestBasicProvider_DisplayName(t *testing.T) {
	p := NewBasicProvider(&config.BasicAuthConfig{})
	if got := p.DisplayName(); got != "Basic Auth" {
		t.Errorf("DisplayName() = %q, want %q", got, "Basic Auth")
	}
}

func TestBasicProvider_Authenticate(t *testing.T) {
	cfg := &config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "admin-pass", Role: "admin"},
			{Username: "viewer", Password: "viewer-pass", Role: "user"},
		},
	}
	p := NewBasicProvider(cfg)

	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
		wantID   string
		wantRole models.Role
	}{
		{
			name:     "valid admin credentials",
			username: "admin",
			password: "admin-pass",
			wantErr:  false,
			wantID:   "basic:admin",
			wantRole: models.RoleAdmin,
		},
		{
			name:     "valid user credentials",
			username: "viewer",
			password: "viewer-pass",
			wantErr:  false,
			wantID:   "basic:viewer",
			wantRole: models.RoleUser,
		},
		{
			name:     "wrong password",
			username: "admin",
			password: "wrong-pass",
			wantErr:  true,
		},
		{
			name:     "unknown user",
			username: "unknown",
			password: "any-pass",
			wantErr:  true,
		},
		{
			name:     "empty username",
			username: "",
			password: "pass",
			wantErr:  true,
		},
		{
			name:     "empty password for existing user",
			username: "admin",
			password: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := p.Authenticate(context.Background(), tt.username, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Error("Authenticate() expected error, got nil")
				}
				if user != nil {
					t.Error("Authenticate() expected nil user on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Authenticate() unexpected error: %v", err)
			}
			if user == nil {
				t.Fatal("Authenticate() returned nil user")
			}
			if user.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", user.ID, tt.wantID)
			}
			if user.Role != tt.wantRole {
				t.Errorf("Role = %q, want %q", user.Role, tt.wantRole)
			}
			if user.Provider != "basic" {
				t.Errorf("Provider = %q, want %q", user.Provider, "basic")
			}
			if user.Login != tt.username {
				t.Errorf("Login = %q, want %q", user.Login, tt.username)
			}
			wantEmail := tt.username + "@local"
			if user.Email != wantEmail {
				t.Errorf("Email = %q, want %q", user.Email, wantEmail)
			}
			if user.Name != tt.username {
				t.Errorf("Name = %q, want %q", user.Name, tt.username)
			}
		})
	}
}

func TestBasicProvider_Authenticate_ErrorMessages(t *testing.T) {
	cfg := &config.BasicAuthConfig{
		Users: []config.BasicAuthUser{
			{Username: "admin", Password: "secret", Role: "admin"},
		},
	}
	p := NewBasicProvider(cfg)

	// Both invalid user and invalid password should produce the same error message
	// to prevent username enumeration.
	_, errBadUser := p.Authenticate(context.Background(), "nonexistent", "secret")
	_, errBadPass := p.Authenticate(context.Background(), "admin", "wrong")

	if errBadUser == nil || errBadPass == nil {
		t.Fatal("expected errors for both bad user and bad pass")
	}
	if errBadUser.Error() != errBadPass.Error() {
		t.Errorf("error messages should match to prevent user enumeration: %q vs %q",
			errBadUser.Error(), errBadPass.Error())
	}
}
