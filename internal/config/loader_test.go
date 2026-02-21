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
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupCaptureLogger sets the global default logger to one that writes to the
// returned buffer, so tests can inspect emitted log messages. Call the returned
// cleanup function to restore the previous default.
func setupCaptureLogger() (*bytes.Buffer, func()) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	return &buf, func() { slog.SetDefault(prev) }
}

// writeConfigFile is a helper that writes YAML content to a temp file and returns the path.
func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	return path
}

func TestLoad_NoPaths(t *testing.T) {
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when no paths provided, got nil")
	}
	if !strings.Contains(err.Error(), "at least one config path is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoad_ValidGitHubConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
github:
  webhook_secret: "gh-secret"
  app_id: 12345
  app_private_key: "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----"
argocd:
  server_url: "https://argocd.example.com"
  token: "argocd-token"
server:
  port: 8080
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.GitHub.AppID != 12345 {
		t.Errorf("GitHub.AppID = %d, want 12345", cfg.GitHub.AppID)
	}
	if cfg.GitHub.WebhookSecret != "gh-secret" {
		t.Errorf("GitHub.WebhookSecret = %q, want %q", cfg.GitHub.WebhookSecret, "gh-secret")
	}
	if cfg.ArgoCD.ServerURL != "https://argocd.example.com" {
		t.Errorf("ArgoCD.ServerURL = %q, want %q", cfg.ArgoCD.ServerURL, "https://argocd.example.com")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	// Defaults should be preserved for unset fields
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want default %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Redis.Address != "localhost:6379" {
		t.Errorf("Redis.Address = %q, want default %q", cfg.Redis.Address, "localhost:6379")
	}
}

func TestLoad_ValidGitLabConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
gitlab:
  url: "https://gitlab.example.com"
  token: "glpat-xxxx"
  webhook_secret: "gl-secret"
argocd:
  server_url: "https://argocd.example.com"
  token: "argocd-token"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.GitLab.Token != "glpat-xxxx" {
		t.Errorf("GitLab.Token = %q, want %q", cfg.GitLab.Token, "glpat-xxxx")
	}
	if cfg.GitLab.URL != "https://gitlab.example.com" {
		t.Errorf("GitLab.URL = %q, want %q", cfg.GitLab.URL, "https://gitlab.example.com")
	}
}

func TestLoad_MultipleFilesMerge(t *testing.T) {
	dir := t.TempDir()
	base := writeConfigFile(t, dir, "base.yaml", `
github:
  webhook_secret: "gh-secret"
  app_id: 12345
  app_private_key: "fake-key"
argocd:
  server_url: "https://argocd.example.com"
  token: "base-token"
server:
  port: 4141
`)
	override := writeConfigFile(t, dir, "override.yaml", `
argocd:
  token: "override-token"
server:
  port: 9090
`)

	cfg, err := Load(base, override)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Override values should win
	if cfg.ArgoCD.Token != "override-token" {
		t.Errorf("ArgoCD.Token = %q, want %q (from override)", cfg.ArgoCD.Token, "override-token")
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090 (from override)", cfg.Server.Port)
	}
	// Base values should be preserved
	if cfg.GitHub.AppID != 12345 {
		t.Errorf("GitHub.AppID = %d, want 12345 (from base)", cfg.GitHub.AppID)
	}
	if cfg.ArgoCD.ServerURL != "https://argocd.example.com" {
		t.Errorf("ArgoCD.ServerURL = %q, want value from base", cfg.ArgoCD.ServerURL)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "bad.yaml", `{invalid: yaml: [`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_DefaultsPreserved(t *testing.T) {
	dir := t.TempDir()
	// Minimal valid config - only required fields
	path := writeConfigFile(t, dir, "config.yaml", `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// All defaults should be preserved
	if cfg.Server.Port != 4141 {
		t.Errorf("Server.Port = %d, want default 4141", cfg.Server.Port)
	}
	if cfg.Server.LogLevel != "info" {
		t.Errorf("Server.LogLevel = %q, want default %q", cfg.Server.LogLevel, "info")
	}
	if cfg.Server.MetricsPort != 9090 {
		t.Errorf("Server.MetricsPort = %d, want default 9090", cfg.Server.MetricsPort)
	}
	if !cfg.Defaults.Autoplan {
		t.Error("Defaults.Autoplan should be true by default")
	}
	if cfg.Defaults.MergeMethod != "squash" {
		t.Errorf("Defaults.MergeMethod = %q, want default %q", cfg.Defaults.MergeMethod, "squash")
	}
	if cfg.Auth.DefaultRole != "user" {
		t.Errorf("Auth.DefaultRole = %q, want default %q", cfg.Auth.DefaultRole, "user")
	}
	if cfg.Queue.Concurrency != 10 {
		t.Errorf("Queue.Concurrency = %d, want default 10", cfg.Queue.Concurrency)
	}
}

func TestLoad_EnvVarOverride(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "file-token"
`)

	// Set env var to override argocd token
	t.Setenv("ARGOCD__TOKEN", "env-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ArgoCD.Token != "env-token" {
		t.Errorf("ArgoCD.Token = %q, want %q (from env var)", cfg.ArgoCD.Token, "env-token")
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantErrs  []string
		wantNoErr bool
	}{
		{
			name: "no VCS provider configured",
			yaml: `
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantErrs: []string{"at least one VCS provider"},
		},
		{
			name: "partial github - missing webhook_secret",
			yaml: `
github:
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantErrs: []string{"github.webhook_secret is required"},
		},
		{
			name: "partial github - missing app_id",
			yaml: `
github:
  webhook_secret: "secret"
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantErrs: []string{"github.app_id is required"},
		},
		{
			name: "partial github - missing app_private_key",
			yaml: `
github:
  webhook_secret: "secret"
  app_id: 1
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantErrs: []string{"github.app_private_key is required"},
		},
		{
			name: "partial gitlab - missing token",
			yaml: `
gitlab:
  webhook_secret: "secret"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantErrs: []string{"gitlab.token is required"},
		},
		{
			name: "partial gitlab - missing webhook_secret",
			yaml: `
gitlab:
  token: "glpat-xxxx"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantErrs: []string{"gitlab.webhook_secret is required"},
		},
		{
			name: "missing argocd server_url",
			yaml: `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  token: "token"
`,
			wantErrs: []string{"argocd.server_url is required"},
		},
		{
			name: "missing argocd token",
			yaml: `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
`,
			wantErrs: []string{"argocd.token is required"},
		},
		{
			name: "multiple errors at once",
			yaml: `
github:
  app_id: 1
argocd:
  server_url: "https://argocd.example.com"
`,
			wantErrs: []string{
				"github.webhook_secret is required",
				"github.app_private_key is required",
				"argocd.token is required",
			},
		},
		{
			name: "valid github config",
			yaml: `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantNoErr: true,
		},
		{
			name: "valid gitlab config",
			yaml: `
gitlab:
  token: "glpat-xxxx"
  webhook_secret: "secret"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantNoErr: true,
		},
		{
			name: "valid both providers",
			yaml: `
github:
  webhook_secret: "gh-secret"
  app_id: 1
  app_private_key: "key"
gitlab:
  token: "glpat-xxxx"
  webhook_secret: "gl-secret"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
`,
			wantNoErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeConfigFile(t, dir, "config.yaml", tt.yaml)

			_, err := Load(path)
			if tt.wantNoErr {
				if err != nil {
					t.Fatalf("Load() unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			for _, wantErr := range tt.wantErrs {
				if !strings.Contains(err.Error(), wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), wantErr)
				}
			}
		})
	}
}

func TestLoad_QueueConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
queue:
  enabled: true
  concurrency: 5
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Queue.Enabled {
		t.Error("Queue.Enabled should be true")
	}
	if cfg.Queue.Concurrency != 5 {
		t.Errorf("Queue.Concurrency = %d, want 5", cfg.Queue.Concurrency)
	}
}

func TestLoad_AuthConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
auth:
  enabled: true
  session_secret: "strong-secret-value"
  default_role: "admin"
  github:
    client_id: "oauth-id"
    client_secret: "oauth-secret"
    allowed_orgs:
      - "my-org"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Auth.Enabled {
		t.Error("Auth.Enabled should be true")
	}
	if cfg.Auth.SessionSecret != "strong-secret-value" {
		t.Errorf("Auth.SessionSecret = %q, want %q", cfg.Auth.SessionSecret, "strong-secret-value")
	}
	if cfg.Auth.DefaultRole != "admin" {
		t.Errorf("Auth.DefaultRole = %q, want %q", cfg.Auth.DefaultRole, "admin")
	}
	if cfg.Auth.GitHub == nil {
		t.Fatal("Auth.GitHub should not be nil")
	}
	if cfg.Auth.GitHub.ClientID != "oauth-id" {
		t.Errorf("Auth.GitHub.ClientID = %q, want %q", cfg.Auth.GitHub.ClientID, "oauth-id")
	}
	if len(cfg.Auth.GitHub.AllowedOrgs) != 1 || cfg.Auth.GitHub.AllowedOrgs[0] != "my-org" {
		t.Errorf("Auth.GitHub.AllowedOrgs = %v, want [my-org]", cfg.Auth.GitHub.AllowedOrgs)
	}
}

func TestLoad_DefaultsConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, "config.yaml", `
github:
  webhook_secret: "secret"
  app_id: 1
  app_private_key: "key"
argocd:
  server_url: "https://argocd.example.com"
  token: "token"
defaults:
  autoplan: false
  require_approval: true
  delete_source_branch: true
  auto_merge: true
  merge_method: "merge"
  allowed_repos:
    - "org/*"
    - "team/specific-repo"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Defaults.Autoplan {
		t.Error("Defaults.Autoplan should be false")
	}
	if !cfg.Defaults.RequireApproval {
		t.Error("Defaults.RequireApproval should be true")
	}
	if !cfg.Defaults.DeleteSourceBranch {
		t.Error("Defaults.DeleteSourceBranch should be true")
	}
	if !cfg.Defaults.AutoMerge {
		t.Error("Defaults.AutoMerge should be true")
	}
	if cfg.Defaults.MergeMethod != "merge" {
		t.Errorf("Defaults.MergeMethod = %q, want %q", cfg.Defaults.MergeMethod, "merge")
	}
	if len(cfg.Defaults.AllowedRepos) != 2 {
		t.Errorf("Defaults.AllowedRepos length = %d, want 2", len(cfg.Defaults.AllowedRepos))
	}
}

func TestWarnInsecureConfig_DevSessionSecret(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Auth.SessionSecret = "dev-secret-key"

	WarnInsecureConfig(cfg)

	if !strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("expected warning about insecure session_secret containing 'dev-', got none")
	}
}

func TestWarnInsecureConfig_ChangeSessionSecret(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Auth.SessionSecret = "change-me-in-production"

	WarnInsecureConfig(cfg)

	if !strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("expected warning about insecure session_secret containing 'change', got none")
	}
}

func TestWarnInsecureConfig_StrongSecret(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Auth.SessionSecret = "f47ac10b58cc4372a5670e02b2c3d479"

	WarnInsecureConfig(cfg)

	if strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("did not expect warning for a strong session_secret")
	}
}

func TestWarnInsecureConfig_AuthDisabled(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Enabled = false
	cfg.Auth.SessionSecret = "dev-secret-key"

	WarnInsecureConfig(cfg)

	if strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("should not warn about session_secret when auth is disabled")
	}
}

func TestWarnInsecureConfig_PasswordEqualsUsername(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Basic = &BasicAuthConfig{
		Users: []BasicAuthUser{
			{Username: "admin", Password: "admin"},
		},
	}

	WarnInsecureConfig(cfg)

	if !strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("expected warning when password equals username")
	}
	if !strings.Contains(buf.String(), "admin") {
		t.Error("expected the username 'admin' to appear in the warning")
	}
}

func TestWarnInsecureConfig_StrongPassword(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Basic = &BasicAuthConfig{
		Users: []BasicAuthUser{
			{Username: "admin", Password: "sup3r-s3cret!"},
		},
	}

	WarnInsecureConfig(cfg)

	if strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("did not expect warning when password differs from username")
	}
}

func TestWarnInsecureConfig_NilBasic(t *testing.T) {
	buf, cleanup := setupCaptureLogger()
	defer cleanup()

	cfg := DefaultConfig()
	cfg.Auth.Basic = nil

	WarnInsecureConfig(cfg)

	if strings.Contains(buf.String(), "INSECURE CONFIG") {
		t.Error("did not expect warning when basic auth is nil")
	}
}
