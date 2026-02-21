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

package queue

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

// captureLog replaces slog's default logger with one writing to a buffer,
// runs fn, then restores the original logger.  Returns what was logged.
func captureLog(t *testing.T, level slog.Level, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(orig) })

	fn()
	return buf.String()
}

func TestNewSlogAdapter(t *testing.T) {
	adapter := newSlogAdapter()
	if adapter == nil {
		t.Fatal("newSlogAdapter() returned nil")
	}
	if adapter.logger == nil {
		t.Fatal("adapter.logger is nil")
	}
}

func TestSlogAdapter_Debug(t *testing.T) {
	// Create adapter after setting default so it picks up the right logger.
	output := captureLog(t, slog.LevelDebug, func() {
		a := newSlogAdapter()
		a.Debug("hello", " ", "debug")
	})

	if output == "" {
		t.Fatal("expected debug output, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("hello debug")) {
		t.Errorf("expected log to contain 'hello debug', got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("level=DEBUG")) {
		t.Errorf("expected DEBUG level, got: %s", output)
	}
}

func TestSlogAdapter_Info(t *testing.T) {
	output := captureLog(t, slog.LevelInfo, func() {
		a := newSlogAdapter()
		a.Info("info message")
	})

	if output == "" {
		t.Fatal("expected info output, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("info message")) {
		t.Errorf("expected log to contain 'info message', got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("level=INFO")) {
		t.Errorf("expected INFO level, got: %s", output)
	}
}

func TestSlogAdapter_Warn(t *testing.T) {
	output := captureLog(t, slog.LevelWarn, func() {
		a := newSlogAdapter()
		a.Warn("warning message")
	})

	if output == "" {
		t.Fatal("expected warn output, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("warning message")) {
		t.Errorf("expected log to contain 'warning message', got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("level=WARN")) {
		t.Errorf("expected WARN level, got: %s", output)
	}
}

func TestSlogAdapter_Error(t *testing.T) {
	output := captureLog(t, slog.LevelError, func() {
		a := newSlogAdapter()
		a.Error("error message")
	})

	if output == "" {
		t.Fatal("expected error output, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("error message")) {
		t.Errorf("expected log to contain 'error message', got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("level=ERROR")) {
		t.Errorf("expected ERROR level, got: %s", output)
	}
}

func TestSlogAdapter_Fatal(t *testing.T) {
	// Fatal should log at ERROR level (not actually exit)
	output := captureLog(t, slog.LevelError, func() {
		a := newSlogAdapter()
		a.Fatal("fatal message")
	})

	if output == "" {
		t.Fatal("expected fatal output, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("fatal message")) {
		t.Errorf("expected log to contain 'fatal message', got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("level=ERROR")) {
		t.Errorf("expected ERROR level for Fatal, got: %s", output)
	}
}

func TestSlogAdapter_ComponentAttribute(t *testing.T) {
	output := captureLog(t, slog.LevelInfo, func() {
		a := newSlogAdapter()
		a.Info("test")
	})

	if !bytes.Contains([]byte(output), []byte("component=asynq")) {
		t.Errorf("expected 'component=asynq' attribute, got: %s", output)
	}
}

func TestSlogAdapter_MultipleArgs(t *testing.T) {
	output := captureLog(t, slog.LevelInfo, func() {
		a := newSlogAdapter()
		a.Info("part1", " ", "part2", " ", "part3")
	})

	if !bytes.Contains([]byte(output), []byte("part1 part2 part3")) {
		t.Errorf("expected concatenated message 'part1 part2 part3', got: %s", output)
	}
}

func TestSlogAdapter_EmptyArgs(t *testing.T) {
	// Calling with no args should not panic
	output := captureLog(t, slog.LevelInfo, func() {
		a := newSlogAdapter()
		a.Info()
	})

	// Should still produce a log line (empty message)
	if output == "" {
		t.Error("expected output even with no args")
	}
}

func TestSlogAdapter_DebugFilteredByLevel(t *testing.T) {
	// With INFO level, debug messages should be suppressed
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(orig) })

	a := newSlogAdapter()
	a.Debug("should not appear")

	if buf.Len() > 0 {
		t.Errorf("debug message should be filtered at INFO level, got: %s", buf.String())
	}
}

// Verify slogAdapter satisfies the asynq Logger interface at compile time.
// asynq.Logger requires Debug, Info, Warn, Error, Fatal with variadic args.
var _ interface {
	Debug(args ...interface{})
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Fatal(args ...interface{})
} = (*slogAdapter)(nil)

// Ensure the adapter methods work with a custom context-aware handler
// (regression guard against handlers that require context).
func TestSlogAdapter_WithContextHandler(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	// Wrap in a handler that checks context (common pattern)
	wrapped := contextHandler{inner: handler}
	orig := slog.Default()
	slog.SetDefault(slog.New(&wrapped))
	t.Cleanup(func() { slog.SetDefault(orig) })

	a := newSlogAdapter()
	a.Debug("ctx-test")
	a.Info("ctx-test")
	a.Warn("ctx-test")
	a.Error("ctx-test")
	a.Fatal("ctx-test")

	if buf.Len() == 0 {
		t.Error("expected output from context-aware handler")
	}
}

// contextHandler is a minimal slog.Handler wrapper for testing.
type contextHandler struct {
	inner slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{inner: h.inner.WithGroup(name)}
}
