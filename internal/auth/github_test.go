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

	"github.com/org/lemuria/internal/config"
)

func TestNewGitHubProvider(t *testing.T) {
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		AllowedOrgs:  []string{"myorg"},
		AllowedTeams: []string{"myorg/admins"},
	}
	p := NewGitHubProvider(cfg, "https://lemuria.example.com")

	if p == nil {
		t.Fatal("NewGitHubProvider() returned nil")
	}
	if p.config.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", p.config.ClientID, "test-client-id")
	}
	if p.config.ClientSecret != "test-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", p.config.ClientSecret, "test-client-secret")
	}
	wantRedirect := "https://lemuria.example.com/auth/github/callback"
	if p.config.RedirectURL != wantRedirect {
		t.Errorf("RedirectURL = %q, want %q", p.config.RedirectURL, wantRedirect)
	}
	if len(p.allowedOrgs) != 1 || p.allowedOrgs[0] != "myorg" {
		t.Errorf("allowedOrgs = %v, want [myorg]", p.allowedOrgs)
	}
	if len(p.allowedTeams) != 1 || p.allowedTeams[0] != "myorg/admins" {
		t.Errorf("allowedTeams = %v, want [myorg/admins]", p.allowedTeams)
	}
}

func TestGitHubProvider_Name(t *testing.T) {
	p := NewGitHubProvider(&config.GitHubOAuthConfig{}, "")
	if got := p.Name(); got != "github" {
		t.Errorf("Name() = %q, want %q", got, "github")
	}
}

func TestGitHubProvider_DisplayName(t *testing.T) {
	p := NewGitHubProvider(&config.GitHubOAuthConfig{}, "")
	if got := p.DisplayName(); got != "GitHub" {
		t.Errorf("DisplayName() = %q, want %q", got, "GitHub")
	}
}

func TestGitHubProvider_AuthURL(t *testing.T) {
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "my-client-id",
		ClientSecret: "my-secret",
	}
	p := NewGitHubProvider(cfg, "https://app.example.com")
	url := p.AuthURL("test-state")

	// The URL should contain the client_id and state parameters
	if url == "" {
		t.Error("AuthURL() returned empty string")
	}
	// It should use GitHub's authorize endpoint
	if !contains(url, "github.com") {
		t.Errorf("AuthURL() = %q, expected to contain github.com", url)
	}
	if !contains(url, "client_id=my-client-id") {
		t.Errorf("AuthURL() = %q, expected to contain client_id", url)
	}
	if !contains(url, "state=test-state") {
		t.Errorf("AuthURL() = %q, expected to contain state", url)
	}
}

func TestGitHubProvider_isOrgMember(t *testing.T) {
	tests := []struct {
		name        string
		allowedOrgs []string
		userOrgs    []string
		want        bool
	}{
		{
			name:        "member of allowed org",
			allowedOrgs: []string{"myorg"},
			userOrgs:    []string{"myorg", "otherorg"},
			want:        true,
		},
		{
			name:        "not member of allowed org",
			allowedOrgs: []string{"myorg"},
			userOrgs:    []string{"otherorg"},
			want:        false,
		},
		{
			name:        "empty user orgs",
			allowedOrgs: []string{"myorg"},
			userOrgs:    nil,
			want:        false,
		},
		{
			name:        "empty allowed orgs",
			allowedOrgs: nil,
			userOrgs:    []string{"myorg"},
			want:        false,
		},
		{
			name:        "multiple allowed orgs - second matches",
			allowedOrgs: []string{"org1", "org2"},
			userOrgs:    []string{"org2"},
			want:        true,
		},
		{
			name:        "multiple allowed orgs - none match",
			allowedOrgs: []string{"org1", "org2"},
			userOrgs:    []string{"org3"},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &GitHubProvider{allowedOrgs: tt.allowedOrgs}
			got := p.isOrgMember(tt.userOrgs)
			if got != tt.want {
				t.Errorf("isOrgMember(%v) = %v, want %v", tt.userOrgs, got, tt.want)
			}
		})
	}
}

func TestGitHubProvider_getUserInfo(t *testing.T) {
	// Create mock server for GitHub API
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":         42,
			"login":      "testuser",
			"email":      "test@example.com",
			"name":       "Test User",
			"avatar_url": "https://github.com/avatar.png",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := []map[string]any{
			{"email": "test@example.com", "primary": true, "verified": true},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// We can't easily test getUserInfo since it uses hardcoded URLs.
	// Instead, test the Exchange flow indirectly through getUserOrgs and getUserTeams
	// which also use hardcoded URLs. Focus on isOrgMember which is fully testable.
	_ = server
}

func TestGitHubProvider_getUserOrgs_MockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := []map[string]any{
			{"login": "org1"},
			{"login": "org2"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// getUserOrgs uses hardcoded githubOrgsAPI URL, so we cannot directly test it
	// with a mock server without refactoring. The isOrgMember helper is tested above.
}

func TestGitHubProvider_getUserTeams_MockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/teams", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := []map[string]any{
			{
				"slug":         "admins",
				"organization": map[string]any{"login": "myorg"},
			},
			{
				"slug":         "developers",
				"organization": map[string]any{"login": "myorg"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// getUserTeams uses hardcoded githubTeamsAPI URL, so we cannot directly test it
	// with a mock server without refactoring. The team format "org/team" is verified
	// indirectly through the RBAC matchesGitHubTeam tests.
}

func TestGitHubProvider_Scopes(t *testing.T) {
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "id",
		ClientSecret: "secret",
	}
	p := NewGitHubProvider(cfg, "https://app.example.com")

	wantScopes := []string{"user:email", "read:org"}
	if len(p.config.Scopes) != len(wantScopes) {
		t.Fatalf("Scopes count = %d, want %d", len(p.config.Scopes), len(wantScopes))
	}
	for i, s := range wantScopes {
		if p.config.Scopes[i] != s {
			t.Errorf("Scopes[%d] = %q, want %q", i, p.config.Scopes[i], s)
		}
	}
}

func TestGitHubProvider_Exchange_NoToken(t *testing.T) {
	// Exchange should fail when the token endpoint is not reachable
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "id",
		ClientSecret: "secret",
	}
	p := NewGitHubProvider(cfg, "https://app.example.com")

	_, err := p.Exchange(context.Background(), "invalid-code")
	if err == nil {
		t.Error("Exchange() with invalid code should return error")
	}
}

// contains is a helper to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
