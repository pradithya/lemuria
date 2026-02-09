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
	"testing"
)

func TestOIDCProvider_isEmailDomainAllowed(t *testing.T) {
	tests := []struct {
		name           string
		allowedDomains []string
		email          string
		want           bool
	}{
		{
			name:           "allowed domain matches exactly",
			allowedDomains: []string{"example.com"},
			email:          "user@example.com",
			want:           true,
		},
		{
			name:           "allowed domain case insensitive",
			allowedDomains: []string{"Example.COM"},
			email:          "user@example.com",
			want:           true,
		},
		{
			name:           "email domain case insensitive",
			allowedDomains: []string{"example.com"},
			email:          "user@EXAMPLE.COM",
			want:           true,
		},
		{
			name:           "domain not in allowed list",
			allowedDomains: []string{"example.com"},
			email:          "user@gmail.com",
			want:           false,
		},
		{
			name:           "multiple allowed domains - first matches",
			allowedDomains: []string{"example.com", "company.org"},
			email:          "user@example.com",
			want:           true,
		},
		{
			name:           "multiple allowed domains - second matches",
			allowedDomains: []string{"example.com", "company.org"},
			email:          "user@company.org",
			want:           true,
		},
		{
			name:           "multiple allowed domains - none match",
			allowedDomains: []string{"example.com", "company.org"},
			email:          "user@gmail.com",
			want:           false,
		},
		{
			name:           "empty email",
			allowedDomains: []string{"example.com"},
			email:          "",
			want:           false,
		},
		{
			name:           "invalid email - no @",
			allowedDomains: []string{"example.com"},
			email:          "userexample.com",
			want:           false,
		},
		{
			name:           "invalid email - multiple @",
			allowedDomains: []string{"example.com"},
			email:          "user@test@example.com",
			want:           false,
		},
		{
			name:           "subdomain does not match parent domain",
			allowedDomains: []string{"example.com"},
			email:          "user@sub.example.com",
			want:           false,
		},
		{
			name:           "parent domain does not match subdomain in allowed list",
			allowedDomains: []string{"sub.example.com"},
			email:          "user@example.com",
			want:           false,
		},
		{
			name:           "exact subdomain match",
			allowedDomains: []string{"sub.example.com"},
			email:          "user@sub.example.com",
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &OIDCProvider{
				allowedDomains: tt.allowedDomains,
			}
			got := p.isEmailDomainAllowed(tt.email)
			if got != tt.want {
				t.Errorf("isEmailDomainAllowed(%q) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

func TestOIDCProvider_buildUser(t *testing.T) {
	tests := []struct {
		name          string
		subject       string
		claims        map[string]any
		usernameClaim string
		emailClaim    string
		groupsClaim   string
		wantLogin     string
		wantEmail     string
		wantName      string
		wantGroups    []string
	}{
		{
			name:          "basic user with all claims",
			subject:       "12345",
			usernameClaim: "preferred_username",
			emailClaim:    "email",
			claims: map[string]any{
				"preferred_username": "johndoe",
				"email":              "john@example.com",
				"name":               "John Doe",
			},
			wantLogin: "johndoe",
			wantEmail: "john@example.com",
			wantName:  "John Doe",
		},
		{
			name:          "fallback to sub for username",
			subject:       "user-sub-123",
			usernameClaim: "preferred_username",
			emailClaim:    "email",
			claims: map[string]any{
				"sub":   "user-sub-123",
				"email": "john@example.com",
			},
			wantLogin: "user-sub-123",
			wantEmail: "john@example.com",
		},
		{
			name:          "name from given_name and family_name",
			subject:       "12345",
			usernameClaim: "preferred_username",
			emailClaim:    "email",
			claims: map[string]any{
				"preferred_username": "johndoe",
				"given_name":         "John",
				"family_name":        "Doe",
			},
			wantLogin: "johndoe",
			wantName:  "John Doe",
		},
		{
			name:          "groups extraction",
			subject:       "12345",
			usernameClaim: "preferred_username",
			emailClaim:    "email",
			groupsClaim:   "groups",
			claims: map[string]any{
				"preferred_username": "johndoe",
				"groups":             []any{"admin", "developers"},
			},
			wantLogin:  "johndoe",
			wantGroups: []string{"admin", "developers"},
		},
		{
			name:          "custom username claim",
			subject:       "12345",
			usernameClaim: "login",
			emailClaim:    "email",
			claims: map[string]any{
				"login": "custom_username",
				"email": "custom@example.com",
			},
			wantLogin: "custom_username",
			wantEmail: "custom@example.com",
		},
		{
			name:          "custom email claim",
			subject:       "12345",
			usernameClaim: "preferred_username",
			emailClaim:    "mail",
			claims: map[string]any{
				"preferred_username": "johndoe",
				"mail":               "john@custom.com",
			},
			wantLogin: "johndoe",
			wantEmail: "john@custom.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &OIDCProvider{
				usernameClaim: tt.usernameClaim,
				emailClaim:    tt.emailClaim,
				groupsClaim:   tt.groupsClaim,
			}

			user := p.buildUser(tt.subject, tt.claims)

			if user.Login != tt.wantLogin {
				t.Errorf("Login = %q, want %q", user.Login, tt.wantLogin)
			}
			if user.Email != tt.wantEmail {
				t.Errorf("Email = %q, want %q", user.Email, tt.wantEmail)
			}
			if user.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", user.Name, tt.wantName)
			}
			if user.ID != "oidc:"+tt.subject {
				t.Errorf("ID = %q, want %q", user.ID, "oidc:"+tt.subject)
			}
			if user.Provider != "oidc" {
				t.Errorf("Provider = %q, want %q", user.Provider, "oidc")
			}
			if len(tt.wantGroups) > 0 {
				if len(user.Groups) != len(tt.wantGroups) {
					t.Errorf("Groups count = %d, want %d", len(user.Groups), len(tt.wantGroups))
				}
				for i, g := range tt.wantGroups {
					if i < len(user.Groups) && user.Groups[i] != g {
						t.Errorf("Groups[%d] = %q, want %q", i, user.Groups[i], g)
					}
				}
			}
		})
	}
}
