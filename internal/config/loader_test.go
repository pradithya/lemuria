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
