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
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/queue"
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
			&cli.StringFlag{
				Name:  "mode",
				Usage: "Run mode: server or worker",
				Value: "server",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			switch cmd.String("mode") {
			case "server":
				return runServer(ctx, cmd)
			case "worker":
				return runWorker(ctx, cmd)
			default:
				return fmt.Errorf("unknown mode %q, expected \"server\" or \"worker\"", cmd.String("mode"))
			}
		},
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
	slog.SetDefault(logger)

	slog.Info("starting lemuria",
		"version", version,
		"commit", commit,
		"mode", "server",
		"config_files", configPaths,
		"log_level", cfg.Server.LogLevel,
	)

	// Create and run server
	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	defer func() {
		if err := srv.Close(); err != nil {
			slog.Error("error closing server", "error", err)
		}
	}()

	// Start Prometheus metrics server
	if cfg.Server.MetricsPort > 0 {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.Handler())

		metricsAddr := fmt.Sprintf(":%d", cfg.Server.MetricsPort)
		go func() {
			slog.Info("starting metrics server", "addr", metricsAddr)
			if err := http.ListenAndServe(metricsAddr, metricsMux); err != nil {
				slog.Error("metrics server error", "error", err)
			}
		}()
	}

	if err := srv.Run(); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func runWorker(ctx context.Context, cmd *cli.Command) error {
	configPaths := cmd.StringSlice("config")

	cfg, err := config.Load(configPaths...)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Server.LogLevel),
	}))
	slog.SetDefault(logger)

	slog.Info("starting lemuria",
		"version", version,
		"commit", commit,
		"mode", "worker",
		"config_files", configPaths,
		"concurrency", cfg.Queue.Concurrency,
	)

	deps, err := server.InitDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize dependencies: %w", err)
	}
	defer func() {
		if err := deps.Close(); err != nil {
			slog.Error("error closing dependencies", "error", err)
		}
	}()

	handler := queue.NewWebhookHandler(
		cfg,
		deps.GithubExecutor,
		deps.GitlabExecutor,
		deps.GithubClient,
		deps.GitlabClient,
	)

	worker := queue.NewWorker(cfg.Redis, cfg.Queue)
	worker.RegisterHandler(queue.TypeWebhookProcess, handler)

	// Start Prometheus metrics server with queue collector
	if cfg.Server.MetricsPort > 0 {
		collector := queue.NewQueueCollector(cfg.Redis)
		defer func() {
			err = collector.Close()
			if err != nil {
				slog.Error("error closing queue collector", "error", err)
			}
		}()

		reg := prometheus.NewRegistry()
		reg.MustRegister(collector)

		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

		metricsAddr := fmt.Sprintf(":%d", cfg.Server.MetricsPort)
		go func() {
			slog.Info("starting metrics server", "addr", metricsAddr)
			if err := http.ListenAndServe(metricsAddr, metricsMux); err != nil {
				slog.Error("metrics server error", "error", err)
			}
		}()
	}

	if err := worker.Run(); err != nil {
		return fmt.Errorf("worker error: %w", err)
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
