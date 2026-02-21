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

	"golang.org/x/oauth2"
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

func TestOIDCProvider_Name(t *testing.T) {
	p := &OIDCProvider{name: "My SSO"}
	if got := p.Name(); got != "oidc" {
		t.Errorf("Name() = %q, want %q", got, "oidc")
	}
}

func TestOIDCProvider_DisplayName(t *testing.T) {
	tests := []struct {
		name     string
		provider *OIDCProvider
		want     string
	}{
		{
			name:     "custom name",
			provider: &OIDCProvider{name: "Corporate SSO"},
			want:     "Corporate SSO",
		},
		{
			name:     "default name",
			provider: &OIDCProvider{name: "SSO"},
			want:     "SSO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.provider.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOIDCProvider_AuthURL(t *testing.T) {
	p := &OIDCProvider{
		config: &oauth2.Config{
			ClientID: "oidc-client-id",
			Endpoint: oauth2.Endpoint{
				AuthURL: "https://idp.example.com/authorize",
			},
			RedirectURL: "https://app.example.com/auth/oidc/callback",
			Scopes:      []string{"openid", "profile", "email"},
		},
	}

	url := p.AuthURL("test-state-xyz")
	if url == "" {
		t.Error("AuthURL() returned empty string")
	}
	if !contains(url, "client_id=oidc-client-id") {
		t.Errorf("AuthURL() = %q, expected to contain client_id", url)
	}
	if !contains(url, "state=test-state-xyz") {
		t.Errorf("AuthURL() = %q, expected to contain state", url)
	}
	if !contains(url, "idp.example.com") {
		t.Errorf("AuthURL() = %q, expected to contain idp URL", url)
	}
}

func TestOIDCProvider_buildUser_WithPicture(t *testing.T) {
	p := &OIDCProvider{
		usernameClaim: "preferred_username",
		emailClaim:    "email",
	}

	claims := map[string]any{
		"preferred_username": "picuser",
		"email":              "pic@example.com",
		"name":               "Pic User",
		"picture":            "https://idp.example.com/avatar/123",
	}

	user := p.buildUser("subj-123", claims)
	if user.AvatarURL != "https://idp.example.com/avatar/123" {
		t.Errorf("AvatarURL = %q, want %q", user.AvatarURL, "https://idp.example.com/avatar/123")
	}
}

func TestOIDCProvider_buildUser_GivenNameOnly(t *testing.T) {
	p := &OIDCProvider{
		usernameClaim: "preferred_username",
		emailClaim:    "email",
	}

	claims := map[string]any{
		"preferred_username": "givenonly",
		"given_name":         "OnlyGiven",
	}

	user := p.buildUser("subj-456", claims)
	if user.Name != "OnlyGiven" {
		t.Errorf("Name = %q, want %q", user.Name, "OnlyGiven")
	}
}

func TestOIDCProvider_buildUser_EmptyClaims(t *testing.T) {
	p := &OIDCProvider{
		usernameClaim: "preferred_username",
		emailClaim:    "email",
		groupsClaim:   "groups",
	}

	user := p.buildUser("subj-empty", map[string]any{})
	if user.ID != "oidc:subj-empty" {
		t.Errorf("ID = %q, want %q", user.ID, "oidc:subj-empty")
	}
	if user.Login != "" {
		t.Errorf("Login = %q, want empty for missing claims", user.Login)
	}
	if user.Email != "" {
		t.Errorf("Email = %q, want empty for missing claims", user.Email)
	}
	if len(user.Groups) != 0 {
		t.Errorf("Groups = %v, want empty", user.Groups)
	}
}

func TestOIDCProvider_buildUser_GroupsWithNonStringEntries(t *testing.T) {
	p := &OIDCProvider{
		usernameClaim: "preferred_username",
		emailClaim:    "email",
		groupsClaim:   "groups",
	}

	claims := map[string]any{
		"preferred_username": "mixedgroups",
		"groups":             []any{"admin", 42, "developers", true},
	}

	user := p.buildUser("subj-mixed", claims)
	// Only string entries should be included
	if len(user.Groups) != 2 {
		t.Errorf("Groups count = %d, want 2 (only strings)", len(user.Groups))
	}
}
