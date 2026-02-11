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

package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/org/lemuria/internal/argocd"
	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/lock"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	argoClient     *argocd.Client
	lockManager    lock.Manager
	testCtx        context.Context
	redisContainer testcontainers.Container
)

func TestMain(m *testing.M) {
	// Set default logger to debug level for all packages
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	// Load environment
	loadEnv()

	// Setup context with timeout
	var cancel context.CancelFunc
	testCtx, cancel = context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	// Initialize clients
	var err error
	argoClient, err = argocd.NewClient(config.ArgoCDConfig{
		ServerURL: getEnv("ARGOCD_SERVER", "http://argocd.127.0.0.1.nip.io:8080"),
		Token:     getEnv("ARGOCD_TOKEN", ""),
		Insecure:  true,
	})
	if err != nil {
		panic("Failed to create Argo CD client: " + err.Error())
	}

	// Start Redis container if REDIS_ADDR is not set
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisContainer, err = testcontainers.GenericContainer(testCtx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "redis:7-alpine",
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor:   wait.ForLog("Ready to accept connections"),
			},
			Started: true,
		})
		if err != nil {
			panic("Failed to start Redis container: " + err.Error())
		}

		host, err := redisContainer.Host(testCtx)
		if err != nil {
			panic("Failed to get Redis container host: " + err.Error())
		}

		port, err := redisContainer.MappedPort(testCtx, "6379")
		if err != nil {
			panic("Failed to get Redis container port: " + err.Error())
		}

		redisAddr = fmt.Sprintf("%s:%s", host, port.Port())
	}

	lockManager, err = lock.NewRedisManager(config.RedisConfig{
		Address: redisAddr,
	})
	if err != nil {
		panic("Failed to create Redis lock manager: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Cleanup
	_ = lockManager.Close()
	if redisContainer != nil {
		_ = redisContainer.Terminate(testCtx)
	}

	os.Exit(code)
}

func loadEnv() {
	// Try to load from .env file
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	for _, line := range splitLines(string(data)) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := splitFirst(line, '=')
		if len(parts) == 2 {
			_ = os.Setenv(parts[0], parts[1])
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFirst(s string, sep byte) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
