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

package config

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"server port", cfg.Server.Port, 4141},
		{"server host", cfg.Server.Host, "0.0.0.0"},
		{"server log level", cfg.Server.LogLevel, "info"},
		{"redis address", cfg.Redis.Address, "localhost:6379"},
		{"redis db", cfg.Redis.DB, 0},
		{"autoplan enabled", cfg.Defaults.Autoplan, true},
		{"require approval disabled", cfg.Defaults.RequireApproval, false},
		{"delete source branch disabled", cfg.Defaults.DeleteSourceBranch, false},
		{"auto merge disabled", cfg.Defaults.AutoMerge, false},
		{"merge method", cfg.Defaults.MergeMethod, "squash"},
		{"skip no changes disabled", cfg.Defaults.SkipNoChanges, false},
		{"auth disabled", cfg.Auth.Enabled, false},
		{"session TTL", cfg.Auth.SessionTTL, 24 * time.Hour},
		{"cookie secure", cfg.Auth.CookieSecure, true},
		{"default role", cfg.Auth.DefaultRole, "user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("DefaultConfig() %s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestConfigAuthEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"auth enabled", true, true},
		{"auth disabled", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Auth: AuthConfig{Enabled: tt.enabled}}
			if got := cfg.AuthEnabled(); got != tt.want {
				t.Errorf("AuthEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasGitHubOAuth(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "nil github config",
			config: &Config{},
			want:   false,
		},
		{
			name:   "empty client id",
			config: &Config{Auth: AuthConfig{GitHub: &GitHubOAuthConfig{}}},
			want:   false,
		},
		{
			name:   "has client id",
			config: &Config{Auth: AuthConfig{GitHub: &GitHubOAuthConfig{ClientID: "my-id"}}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasGitHubOAuth(); got != tt.want {
				t.Errorf("HasGitHubOAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasOIDC(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "nil oidc config",
			config: &Config{},
			want:   false,
		},
		{
			name:   "empty issuer url",
			config: &Config{Auth: AuthConfig{OIDC: &OIDCConfig{}}},
			want:   false,
		},
		{
			name:   "has issuer url",
			config: &Config{Auth: AuthConfig{OIDC: &OIDCConfig{IssuerURL: "https://sso.example.com"}}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasOIDC(); got != tt.want {
				t.Errorf("HasOIDC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasBasicAuth(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "nil basic config",
			config: &Config{},
			want:   false,
		},
		{
			name:   "empty users",
			config: &Config{Auth: AuthConfig{Basic: &BasicAuthConfig{}}},
			want:   false,
		},
		{
			name: "has users",
			config: &Config{Auth: AuthConfig{Basic: &BasicAuthConfig{
				Users: []BasicAuthUser{{Username: "admin", Password: "pass"}},
			}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasBasicAuth(); got != tt.want {
				t.Errorf("HasBasicAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasGitHub(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "zero values",
			config: &Config{},
			want:   false,
		},
		{
			name:   "only app_id set",
			config: &Config{GitHub: GitHubConfig{AppID: 12345}},
			want:   false,
		},
		{
			name:   "only app_private_key set",
			config: &Config{GitHub: GitHubConfig{AppPrivateKey: "some-key"}},
			want:   false,
		},
		{
			name:   "both app_id and app_private_key set",
			config: &Config{GitHub: GitHubConfig{AppID: 12345, AppPrivateKey: "some-key"}},
			want:   true,
		},
		{
			name:   "webhook_secret set but not app fields",
			config: &Config{GitHub: GitHubConfig{WebhookSecret: "secret"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasGitHub(); got != tt.want {
				t.Errorf("HasGitHub() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasGitLab(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "zero values",
			config: &Config{},
			want:   false,
		},
		{
			name:   "token set",
			config: &Config{GitLab: GitLabConfig{Token: "glpat-xxxx"}},
			want:   true,
		},
		{
			name:   "empty token",
			config: &Config{GitLab: GitLabConfig{Token: ""}},
			want:   false,
		},
		{
			name:   "url and webhook set but no token",
			config: &Config{GitLab: GitLabConfig{URL: "https://gitlab.com", WebhookSecret: "secret"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasGitLab(); got != tt.want {
				t.Errorf("HasGitLab() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigHasGitLabOAuth(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   bool
	}{
		{
			name:   "nil gitlab oauth config",
			config: &Config{},
			want:   false,
		},
		{
			name:   "empty client id",
			config: &Config{Auth: AuthConfig{GitLab: &GitLabOAuthConfig{}}},
			want:   false,
		},
		{
			name:   "has client id",
			config: &Config{Auth: AuthConfig{GitLab: &GitLabOAuthConfig{ClientID: "my-gitlab-id"}}},
			want:   true,
		},
		{
			name:   "client secret set but no client id",
			config: &Config{Auth: AuthConfig{GitLab: &GitLabOAuthConfig{ClientSecret: "secret"}}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasGitLabOAuth(); got != tt.want {
				t.Errorf("HasGitLabOAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLockTTL(t *testing.T) {
	ttl := LockTTL()
	expected := 7 * 24 * time.Hour
	if ttl != expected {
		t.Errorf("LockTTL() = %v, want %v", ttl, expected)
	}
}

func TestIsRepoAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowedRepos []string
		repo         string
		want         bool
	}{
		{
			name:         "empty allowed list allows all",
			allowedRepos: nil,
			repo:         "org/any-repo",
			want:         true,
		},
		{
			name:         "exact match",
			allowedRepos: []string{"org/repo1"},
			repo:         "org/repo1",
			want:         true,
		},
		{
			name:         "exact match miss",
			allowedRepos: []string{"org/repo1"},
			repo:         "org/repo2",
			want:         false,
		},
		{
			name:         "wildcard match",
			allowedRepos: []string{"org/*"},
			repo:         "org/any-repo",
			want:         true,
		},
		{
			name:         "wildcard miss different org",
			allowedRepos: []string{"org/*"},
			repo:         "other-org/repo",
			want:         false,
		},
		{
			name:         "multiple patterns with match",
			allowedRepos: []string{"team-a/*", "team-b/specific"},
			repo:         "team-b/specific",
			want:         true,
		},
		{
			name:         "multiple patterns no match",
			allowedRepos: []string{"team-a/*", "team-b/specific"},
			repo:         "team-c/repo",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Defaults: DefaultsConfig{AllowedRepos: tt.allowedRepos}}
			if got := cfg.IsRepoAllowed(tt.repo); got != tt.want {
				t.Errorf("IsRepoAllowed(%q) = %v, want %v", tt.repo, got, tt.want)
			}
		})
	}
}

func TestLoadRepoConfig(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		check   func(t *testing.T, cfg *RepoConfig)
		wantErr bool
	}{
		{
			name: "full config",
			data: `
version: 1
autoplan: true
require_approval: true
auto_merge: false
applications:
  - name: "my-app"
    paths:
      - "apps/my-app/**"
      - "base/**"
  - name: "my-app-*"
    paths:
      - "apps/my-app/**"
    applicationset: "my-appset"
sync_requirements:
  - name: "production-*"
    require_approval: true
    allowed_users:
      - "admin"
`,
			check: func(t *testing.T, cfg *RepoConfig) {
				if cfg.Version != 1 {
					t.Errorf("Version = %d, want 1", cfg.Version)
				}
				if cfg.Autoplan == nil || !*cfg.Autoplan {
					t.Error("Autoplan should be true")
				}
				if cfg.RequireApproval == nil || !*cfg.RequireApproval {
					t.Error("RequireApproval should be true")
				}
				if cfg.AutoMerge == nil || *cfg.AutoMerge {
					t.Error("AutoMerge should be false")
				}
				if len(cfg.Applications) != 2 {
					t.Errorf("Applications count = %d, want 2", len(cfg.Applications))
				}
				if len(cfg.SyncRequirements) != 1 {
					t.Errorf("SyncRequirements count = %d, want 1", len(cfg.SyncRequirements))
				}
				if cfg.SyncRequirements[0].Name != "production-*" {
					t.Errorf("SyncRequirements[0].Name = %q, want %q", cfg.SyncRequirements[0].Name, "production-*")
				}
			},
		},
		{
			name: "minimal config defaults version to 1",
			data: `
autoplan: false
`,
			check: func(t *testing.T, cfg *RepoConfig) {
				if cfg.Version != 1 {
					t.Errorf("Version = %d, want 1", cfg.Version)
				}
				if cfg.Autoplan == nil || *cfg.Autoplan {
					t.Error("Autoplan should be false")
				}
			},
		},
		{
			name: "skip_no_changes config",
			data: `
version: 1
skip_no_changes: true
`,
			check: func(t *testing.T, cfg *RepoConfig) {
				if cfg.SkipNoChanges == nil || !*cfg.SkipNoChanges {
					t.Error("SkipNoChanges should be true")
				}
			},
		},
		{
			name: "skip_no_changes false",
			data: `
version: 1
skip_no_changes: false
`,
			check: func(t *testing.T, cfg *RepoConfig) {
				if cfg.SkipNoChanges == nil || *cfg.SkipNoChanges {
					t.Error("SkipNoChanges should be false")
				}
			},
		},
		{
			name: "skip_no_changes not set",
			data: `
version: 1
`,
			check: func(t *testing.T, cfg *RepoConfig) {
				if cfg.SkipNoChanges != nil {
					t.Error("SkipNoChanges should be nil when not set")
				}
			},
		},
		{
			name:    "invalid YAML",
			data:    `{invalid: yaml: [`,
			wantErr: true,
		},
		{
			name: "empty YAML defaults version",
			data: ``,
			check: func(t *testing.T, cfg *RepoConfig) {
				if cfg.Version != 1 {
					t.Errorf("Version = %d, want 1 (default)", cfg.Version)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadRepoConfig([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadRepoConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestMatchRepoPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		repo    string
		want    bool
	}{
		{"exact match", "org/repo", "org/repo", true},
		{"exact mismatch", "org/repo", "org/other", false},
		{"wildcard match", "org/*", "org/repo", true},
		{"wildcard match nested", "org/*", "org/sub-repo", true},
		{"wildcard mismatch", "org/*", "other/repo", false},
		{"no slash after prefix", "org/*", "org-extra/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchRepoPattern(tt.pattern, tt.repo); got != tt.want {
				t.Errorf("matchRepoPattern(%q, %q) = %v, want %v", tt.pattern, tt.repo, got, tt.want)
			}
		})
	}
}
