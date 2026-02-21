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

	"golang.org/x/oauth2"

	"github.com/org/lemuria/internal/config"
)

func TestNewGitLabProvider(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *config.GitLabOAuthConfig
		baseURL         string
		wantAuthURL     string
		wantTokenURL    string
		wantRedirectURL string
	}{
		{
			name: "default gitlab.com URL",
			cfg: &config.GitLabOAuthConfig{
				ClientID:     "gl-client",
				ClientSecret: "gl-secret",
			},
			baseURL:         "https://app.example.com",
			wantAuthURL:     "https://gitlab.com/oauth/authorize",
			wantTokenURL:    "https://gitlab.com/oauth/token",
			wantRedirectURL: "https://app.example.com/auth/gitlab/callback",
		},
		{
			name: "custom gitlab URL",
			cfg: &config.GitLabOAuthConfig{
				URL:          "https://gitlab.mycompany.com",
				ClientID:     "gl-client",
				ClientSecret: "gl-secret",
			},
			baseURL:         "https://app.example.com",
			wantAuthURL:     "https://gitlab.mycompany.com/oauth/authorize",
			wantTokenURL:    "https://gitlab.mycompany.com/oauth/token",
			wantRedirectURL: "https://app.example.com/auth/gitlab/callback",
		},
		{
			name: "with allowed groups",
			cfg: &config.GitLabOAuthConfig{
				ClientID:      "gl-client",
				ClientSecret:  "gl-secret",
				AllowedGroups: []string{"mygroup", "othergroup"},
			},
			baseURL:         "https://app.example.com",
			wantAuthURL:     "https://gitlab.com/oauth/authorize",
			wantTokenURL:    "https://gitlab.com/oauth/token",
			wantRedirectURL: "https://app.example.com/auth/gitlab/callback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewGitLabProvider(tt.cfg, tt.baseURL)
			if p == nil {
				t.Fatal("NewGitLabProvider() returned nil")
			}
			if p.config.Endpoint.AuthURL != tt.wantAuthURL {
				t.Errorf("AuthURL = %q, want %q", p.config.Endpoint.AuthURL, tt.wantAuthURL)
			}
			if p.config.Endpoint.TokenURL != tt.wantTokenURL {
				t.Errorf("TokenURL = %q, want %q", p.config.Endpoint.TokenURL, tt.wantTokenURL)
			}
			if p.config.RedirectURL != tt.wantRedirectURL {
				t.Errorf("RedirectURL = %q, want %q", p.config.RedirectURL, tt.wantRedirectURL)
			}
			if p.config.ClientID != tt.cfg.ClientID {
				t.Errorf("ClientID = %q, want %q", p.config.ClientID, tt.cfg.ClientID)
			}
		})
	}
}

func TestGitLabProvider_Name(t *testing.T) {
	p := NewGitLabProvider(&config.GitLabOAuthConfig{}, "")
	if got := p.Name(); got != "gitlab" {
		t.Errorf("Name() = %q, want %q", got, "gitlab")
	}
}

func TestGitLabProvider_DisplayName(t *testing.T) {
	p := NewGitLabProvider(&config.GitLabOAuthConfig{}, "")
	if got := p.DisplayName(); got != "GitLab" {
		t.Errorf("DisplayName() = %q, want %q", got, "GitLab")
	}
}

func TestGitLabProvider_AuthURL(t *testing.T) {
	cfg := &config.GitLabOAuthConfig{
		ClientID:     "my-gl-client",
		ClientSecret: "my-gl-secret",
	}
	p := NewGitLabProvider(cfg, "https://app.example.com")
	url := p.AuthURL("test-state-123")

	if url == "" {
		t.Error("AuthURL() returned empty string")
	}
	if !contains(url, "client_id=my-gl-client") {
		t.Errorf("AuthURL() = %q, expected to contain client_id", url)
	}
	if !contains(url, "state=test-state-123") {
		t.Errorf("AuthURL() = %q, expected to contain state", url)
	}
}

func TestGitLabProvider_isGroupMember(t *testing.T) {
	tests := []struct {
		name          string
		allowedGroups []string
		userGroups    []string
		want          bool
	}{
		{
			name:          "member of allowed group",
			allowedGroups: []string{"mygroup"},
			userGroups:    []string{"mygroup", "othergroup"},
			want:          true,
		},
		{
			name:          "not member of allowed group",
			allowedGroups: []string{"mygroup"},
			userGroups:    []string{"othergroup"},
			want:          false,
		},
		{
			name:          "empty user groups",
			allowedGroups: []string{"mygroup"},
			userGroups:    nil,
			want:          false,
		},
		{
			name:          "empty allowed groups",
			allowedGroups: nil,
			userGroups:    []string{"mygroup"},
			want:          false,
		},
		{
			name:          "multiple allowed groups - second matches",
			allowedGroups: []string{"group1", "group2"},
			userGroups:    []string{"group2"},
			want:          true,
		},
		{
			name:          "subgroup full path match",
			allowedGroups: []string{"parent/child"},
			userGroups:    []string{"parent/child"},
			want:          true,
		},
		{
			name:          "subgroup does not match parent",
			allowedGroups: []string{"parent"},
			userGroups:    []string{"parent/child"},
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &GitLabProvider{allowedGroups: tt.allowedGroups}
			got := p.isGroupMember(tt.userGroups)
			if got != tt.want {
				t.Errorf("isGroupMember(%v) = %v, want %v", tt.userGroups, got, tt.want)
			}
		})
	}
}

func TestGitLabProvider_getUserInfo_MockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":         int64(100),
			"username":   "gluser",
			"email":      "gluser@example.com",
			"name":       "GL User",
			"avatar_url": "https://gitlab.com/avatar.png",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitLabProvider{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	token := &oauth2.Token{AccessToken: "mock-token"}
	user, err := p.getUserInfo(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserInfo() error: %v", err)
	}
	if user.ID != "gitlab:100" {
		t.Errorf("ID = %q, want %q", user.ID, "gitlab:100")
	}
	if user.Login != "gluser" {
		t.Errorf("Login = %q, want %q", user.Login, "gluser")
	}
	if user.Email != "gluser@example.com" {
		t.Errorf("Email = %q, want %q", user.Email, "gluser@example.com")
	}
	if user.Name != "GL User" {
		t.Errorf("Name = %q, want %q", user.Name, "GL User")
	}
	if user.AvatarURL != "https://gitlab.com/avatar.png" {
		t.Errorf("AvatarURL = %q, want %q", user.AvatarURL, "https://gitlab.com/avatar.png")
	}
	if user.Provider != "gitlab" {
		t.Errorf("Provider = %q, want %q", user.Provider, "gitlab")
	}
}

func TestGitLabProvider_getUserInfo_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error": "forbidden"}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitLabProvider{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	token := &oauth2.Token{AccessToken: "bad-token"}
	_, err := p.getUserInfo(context.Background(), token)
	if err == nil {
		t.Error("getUserInfo() expected error for non-200 response")
	}
}

func TestGitLabProvider_getUserGroups_MockServer(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		if page == "" || page == "1" {
			resp := []map[string]any{
				{"full_path": "group1"},
				{"full_path": "group2/subgroup"},
			}
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			// Empty response to end pagination
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitLabProvider{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	token := &oauth2.Token{AccessToken: "mock-token"}
	groups, err := p.getUserGroups(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserGroups() error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups count = %d, want 2", len(groups))
	}
	if groups[0] != "group1" {
		t.Errorf("groups[0] = %q, want %q", groups[0], "group1")
	}
	if groups[1] != "group2/subgroup" {
		t.Errorf("groups[1] = %q, want %q", groups[1], "group2/subgroup")
	}
}

func TestGitLabProvider_getUserGroups_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/groups", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitLabProvider{
		baseURL:    server.URL,
		httpClient: server.Client(),
	}

	token := &oauth2.Token{AccessToken: "mock-token"}
	_, err := p.getUserGroups(context.Background(), token)
	if err == nil {
		t.Error("getUserGroups() expected error for non-200 response")
	}
}

func TestGitLabProvider_Scopes(t *testing.T) {
	cfg := &config.GitLabOAuthConfig{
		ClientID:     "id",
		ClientSecret: "secret",
	}
	p := NewGitLabProvider(cfg, "https://app.example.com")

	wantScopes := []string{"read_user", "read_api"}
	if len(p.config.Scopes) != len(wantScopes) {
		t.Fatalf("Scopes count = %d, want %d", len(p.config.Scopes), len(wantScopes))
	}
	for i, s := range wantScopes {
		if p.config.Scopes[i] != s {
			t.Errorf("Scopes[%d] = %q, want %q", i, p.config.Scopes[i], s)
		}
	}
}

func TestGitLabProvider_Exchange_NoToken(t *testing.T) {
	cfg := &config.GitLabOAuthConfig{
		ClientID:     "id",
		ClientSecret: "secret",
	}
	p := NewGitLabProvider(cfg, "https://app.example.com")

	_, err := p.Exchange(context.Background(), "invalid-code")
	if err == nil {
		t.Error("Exchange() with invalid code should return error")
	}
}
