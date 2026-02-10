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
	"strings"
	"testing"
)

func TestValidate_SessionSecretMinLength(t *testing.T) {
	baseCfg := func() *Config {
		cfg := DefaultConfig()
		cfg.GitHub = GitHubConfig{
			WebhookSecret: "webhook-secret",
			AppID:         12345,
			AppPrivateKey: "private-key",
		}
		cfg.ArgoCD = ArgoCDConfig{
			ServerURL: "https://argocd.example.com",
			Token:     "argocd-token",
		}
		cfg.Auth.Enabled = true
		return cfg
	}

	tests := []struct {
		name          string
		sessionSecret string
		wantErr       bool
		errContains   string
	}{
		{
			name:          "empty session secret",
			sessionSecret: "",
			wantErr:       true,
			errContains:   "auth.session_secret is required",
		},
		{
			name:          "too short session secret (16 bytes)",
			sessionSecret: "short-secret-16b",
			wantErr:       true,
			errContains:   "auth.session_secret must be at least 32 bytes",
		},
		{
			name:          "too short session secret (31 bytes)",
			sessionSecret: strings.Repeat("a", 31),
			wantErr:       true,
			errContains:   "auth.session_secret must be at least 32 bytes",
		},
		{
			name:          "exactly 32 bytes is valid",
			sessionSecret: strings.Repeat("a", 32),
			wantErr:       false,
		},
		{
			name:          "longer than 32 bytes is valid",
			sessionSecret: strings.Repeat("a", 64),
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg()
			cfg.Auth.SessionSecret = tt.sessionSecret
			err := validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validate() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidate_BasicAuthPasswordHash(t *testing.T) {
	baseCfg := func() *Config {
		cfg := DefaultConfig()
		cfg.GitHub = GitHubConfig{
			WebhookSecret: "webhook-secret",
			AppID:         12345,
			AppPrivateKey: "private-key",
		}
		cfg.ArgoCD = ArgoCDConfig{
			ServerURL: "https://argocd.example.com",
			Token:     "argocd-token",
		}
		cfg.Auth.Enabled = true
		cfg.Auth.SessionSecret = strings.Repeat("a", 32)
		return cfg
	}

	tests := []struct {
		name        string
		basicAuth   *BasicAuthConfig
		wantErr     bool
		errContains string
	}{
		{
			name:      "nil basic auth config is ok",
			basicAuth: nil,
			wantErr:   false,
		},
		{
			name:      "empty users list is ok",
			basicAuth: &BasicAuthConfig{Users: []BasicAuthUser{}},
			wantErr:   false,
		},
		{
			name: "valid user with password_hash",
			basicAuth: &BasicAuthConfig{
				Users: []BasicAuthUser{
					{Username: "admin", PasswordHash: "$2a$10$somehash", Role: "admin"},
				},
			},
			wantErr: false,
		},
		{
			name: "user with empty password_hash",
			basicAuth: &BasicAuthConfig{
				Users: []BasicAuthUser{
					{Username: "admin", PasswordHash: "", Role: "admin"},
				},
			},
			wantErr:     true,
			errContains: "auth.basic.users[0].password_hash is required",
		},
		{
			name: "second user missing password_hash",
			basicAuth: &BasicAuthConfig{
				Users: []BasicAuthUser{
					{Username: "admin", PasswordHash: "$2a$10$somehash", Role: "admin"},
					{Username: "user", PasswordHash: "", Role: "user"},
				},
			},
			wantErr:     true,
			errContains: "auth.basic.users[1].password_hash is required",
		},
		{
			name: "multiple users all missing password_hash",
			basicAuth: &BasicAuthConfig{
				Users: []BasicAuthUser{
					{Username: "admin", PasswordHash: "", Role: "admin"},
					{Username: "user", PasswordHash: "", Role: "user"},
				},
			},
			wantErr:     true,
			errContains: "auth.basic.users[0].password_hash is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg()
			cfg.Auth.Basic = tt.basicAuth
			err := validate(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validate() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidate_BasicAuthPasswordHashHintsBcrypt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GitHub = GitHubConfig{
		WebhookSecret: "webhook-secret",
		AppID:         12345,
		AppPrivateKey: "private-key",
	}
	cfg.ArgoCD = ArgoCDConfig{
		ServerURL: "https://argocd.example.com",
		Token:     "argocd-token",
	}
	cfg.Auth.Enabled = true
	cfg.Auth.SessionSecret = strings.Repeat("a", 32)
	cfg.Auth.Basic = &BasicAuthConfig{
		Users: []BasicAuthUser{
			{Username: "admin", PasswordHash: "", Role: "admin"},
		},
	}

	err := validate(cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error message should mention bcrypt to help users migrate from plaintext password
	if !strings.Contains(err.Error(), "bcrypt") {
		t.Errorf("error should mention bcrypt to guide migration, got: %v", err)
	}
}
