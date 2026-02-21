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
)

// redirectTransport is an http.RoundTripper that redirects GitHub API requests
// to a local test server, allowing us to test methods with hardcoded URLs.
type redirectTransport struct {
	targetURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to the test server but keep the path
	newURL := t.targetURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

func TestGitHubProvider_getUserInfo_WithMockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         int64(42),
			"login":      "octocat",
			"email":      "octocat@github.com",
			"name":       "The Octocat",
			"avatar_url": "https://avatars.githubusercontent.com/u/42",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-access-token"}
	user, err := p.getUserInfo(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserInfo() error: %v", err)
	}
	if user.ID != "github:42" {
		t.Errorf("ID = %q, want %q", user.ID, "github:42")
	}
	if user.Login != "octocat" {
		t.Errorf("Login = %q, want %q", user.Login, "octocat")
	}
	if user.Email != "octocat@github.com" {
		t.Errorf("Email = %q, want %q", user.Email, "octocat@github.com")
	}
	if user.Name != "The Octocat" {
		t.Errorf("Name = %q, want %q", user.Name, "The Octocat")
	}
	if user.AvatarURL != "https://avatars.githubusercontent.com/u/42" {
		t.Errorf("AvatarURL = %q", user.AvatarURL)
	}
	if user.Provider != "github" {
		t.Errorf("Provider = %q, want %q", user.Provider, "github")
	}
}

func TestGitHubProvider_getUserInfo_NoPublicEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    int64(99),
			"login": "private-email-user",
			"email": nil, // no public email
			"name":  "Private User",
		})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "secondary@example.com", "primary": false, "verified": true},
			{"email": "primary@example.com", "primary": true, "verified": true},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	user, err := p.getUserInfo(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserInfo() error: %v", err)
	}
	if user.Email != "primary@example.com" {
		t.Errorf("Email = %q, want %q (primary verified email)", user.Email, "primary@example.com")
	}
}

func TestGitHubProvider_getUserInfo_FallbackToVerifiedEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    int64(55),
			"login": "noprimary",
			"email": nil,
			"name":  "No Primary",
		})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "verified@example.com", "primary": false, "verified": true},
			{"email": "unverified@example.com", "primary": true, "verified": false},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	user, err := p.getUserInfo(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserInfo() error: %v", err)
	}
	if user.Email != "verified@example.com" {
		t.Errorf("Email = %q, want %q (fallback to first verified)", user.Email, "verified@example.com")
	}
}

func TestGitHubProvider_getUserInfo_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	_, err := p.getUserInfo(context.Background(), token)
	if err == nil {
		t.Error("getUserInfo() should return error for non-200 response")
	}
}

func TestGitHubProvider_getPrimaryEmail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "secondary@example.com", "primary": false, "verified": true},
			{"email": "primary@example.com", "primary": true, "verified": true},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	email, err := p.getPrimaryEmail(context.Background(), token)
	if err != nil {
		t.Fatalf("getPrimaryEmail() error: %v", err)
	}
	if email != "primary@example.com" {
		t.Errorf("email = %q, want %q", email, "primary@example.com")
	}
}

func TestGitHubProvider_getPrimaryEmail_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	_, err := p.getPrimaryEmail(context.Background(), token)
	if err == nil {
		t.Error("getPrimaryEmail() should return error for non-200 response")
	}
}

func TestGitHubProvider_getPrimaryEmail_NoEmails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	email, err := p.getPrimaryEmail(context.Background(), token)
	if err != nil {
		t.Fatalf("getPrimaryEmail() error: %v", err)
	}
	if email != "" {
		t.Errorf("email = %q, want empty string for no emails", email)
	}
}

func TestGitHubProvider_getUserOrgs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"login": "orgA"},
			{"login": "orgB"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	orgs, err := p.getUserOrgs(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserOrgs() error: %v", err)
	}
	if len(orgs) != 2 {
		t.Fatalf("orgs count = %d, want 2", len(orgs))
	}
	if orgs[0] != "orgA" || orgs[1] != "orgB" {
		t.Errorf("orgs = %v, want [orgA, orgB]", orgs)
	}
}

func TestGitHubProvider_getUserOrgs_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	_, err := p.getUserOrgs(context.Background(), token)
	if err == nil {
		t.Error("getUserOrgs() should return error for non-200 response")
	}
}

func TestGitHubProvider_getUserTeams(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/teams", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"slug":         "admins",
				"organization": map[string]any{"login": "myorg"},
			},
			{
				"slug":         "developers",
				"organization": map[string]any{"login": "myorg"},
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	teams, err := p.getUserTeams(context.Background(), token)
	if err != nil {
		t.Fatalf("getUserTeams() error: %v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("teams count = %d, want 2", len(teams))
	}
	if teams[0] != "myorg/admins" {
		t.Errorf("teams[0] = %q, want %q", teams[0], "myorg/admins")
	}
	if teams[1] != "myorg/developers" {
		t.Errorf("teams[1] = %q, want %q", teams[1], "myorg/developers")
	}
}

func TestGitHubProvider_getUserTeams_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/teams", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := &GitHubProvider{
		httpClient: &http.Client{Transport: &redirectTransport{targetURL: server.URL}},
	}

	token := &oauth2.Token{AccessToken: "test-token"}
	_, err := p.getUserTeams(context.Background(), token)
	if err == nil {
		t.Error("getUserTeams() should return error for non-200 response")
	}
}
