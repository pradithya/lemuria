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

package models

import (
	"testing"
	"time"
)

func TestRoleHasPermission(t *testing.T) {
	tests := []struct {
		name       string
		role       Role
		permission string
		want       bool
	}{
		{"admin has read", RoleAdmin, "read", true},
		{"admin has write", RoleAdmin, "write", true},
		{"admin has delete", RoleAdmin, "delete", true},
		{"admin has admin", RoleAdmin, "admin", true},
		{"user has read", RoleUser, "read", true},
		{"user no write", RoleUser, "write", false},
		{"user no delete", RoleUser, "delete", false},
		{"user no admin", RoleUser, "admin", false},
		{"invalid role", Role("invalid"), "read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.HasPermission(tt.permission); got != tt.want {
				t.Errorf("HasPermission(%q) = %v, want %v", tt.permission, got, tt.want)
			}
		})
	}
}

func TestRoleIsAdmin(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{"admin", RoleAdmin, true},
		{"user", RoleUser, false},
		{"empty", Role(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.IsAdmin(); got != tt.want {
				t.Errorf("IsAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoleValid(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{"admin", RoleAdmin, true},
		{"user", RoleUser, true},
		{"empty", Role(""), false},
		{"invalid", Role("superadmin"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"not expired", time.Now().Add(1 * time.Hour), false},
		{"expired", time.Now().Add(-1 * time.Hour), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{ExpiresAt: tt.expiresAt}
			if got := s.IsExpired(); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionIsValid(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{
			name: "valid session",
			session: &Session{
				User:      &User{Login: "test"},
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			want: true,
		},
		{
			name: "expired session",
			session: &Session{
				User:      &User{Login: "test"},
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
			want: false,
		},
		{
			name: "nil user",
			session: &Session{
				User:      nil,
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}
