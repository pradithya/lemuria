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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/server"
	"github.com/urfave/cli/v3"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	cmd := &cli.Command{
		Name:    "lemuria",
		Usage:   "GitOps automation for Argo CD",
		Version: fmt.Sprintf("%s (commit: %s)", version, commit),
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to configuration file (can be specified multiple times, files are merged in order)",
				Value:   []string{"lemuria.yaml"},
			},
		},
		Action: runServer,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runServer(ctx context.Context, cmd *cli.Command) error {
	configPaths := cmd.StringSlice("config")

	// Load configuration (multiple files are merged in order)
	cfg, err := config.Load(configPaths...)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Setup logger with configured log level
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Server.LogLevel),
	}))

	logger.Info("starting lemuria",
		"version", version,
		"commit", commit,
		"config_files", configPaths,
		"log_level", cfg.Server.LogLevel,
	)

	// Create and run server
	srv, err := server.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	defer func() {
		if err := srv.Close(); err != nil {
			logger.Error("error closing server", "error", err)
		}
	}()

	if err := srv.Run(); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
