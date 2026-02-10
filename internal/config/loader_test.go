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
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvOverlayOnlyPrefixed(t *testing.T) {
	// Write a minimal valid config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `
github:
  webhook_secret: "file-webhook-secret"
  app_id: 12345
  app_private_key: "file-private-key"
argocd:
  server_url: "https://argocd.example.com"
  token: "file-argocd-token"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Set a prefixed env var - this SHOULD override the config value
	t.Setenv("LEMURIA_ARGOCD__TOKEN", "env-argocd-token")

	// Set an unprefixed env var - this should NOT override anything
	t.Setenv("ARGOCD__TOKEN", "unprefixed-argocd-token")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// The prefixed env var should have overridden the file value
	if cfg.ArgoCD.Token != "env-argocd-token" {
		t.Errorf("ArgoCD.Token = %q, want %q (from LEMURIA_ARGOCD__TOKEN)", cfg.ArgoCD.Token, "env-argocd-token")
	}

	// The server URL should remain unchanged (no env override for it)
	if cfg.ArgoCD.ServerURL != "https://argocd.example.com" {
		t.Errorf("ArgoCD.ServerURL = %q, want %q (from config file)", cfg.ArgoCD.ServerURL, "https://argocd.example.com")
	}
}

func TestLoadEnvOverlayHierarchy(t *testing.T) {
	// Write a minimal valid config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgContent := `
github:
  webhook_secret: "file-webhook-secret"
  app_id: 12345
  app_private_key: "file-private-key"
argocd:
  server_url: "https://argocd.example.com"
  token: "file-argocd-token"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Double underscore maps to hierarchy separator (.)
	// Single underscore is preserved in the key
	t.Setenv("LEMURIA_ARGOCD__SERVER_URL", "https://override.example.com")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ArgoCD.ServerURL != "https://override.example.com" {
		t.Errorf("ArgoCD.ServerURL = %q, want %q", cfg.ArgoCD.ServerURL, "https://override.example.com")
	}
}

func TestLoadRequiresAtLeastOnePath(t *testing.T) {
	_, err := Load()
	if err == nil {
		t.Error("Load() with no paths should return error")
	}
}
