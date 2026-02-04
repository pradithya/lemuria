package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/server"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "lemuria.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Lemuria %s (commit: %s)\n", version, commit)
		os.Exit(0)
	}

	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("starting lemuria",
		"version", version,
		"commit", commit,
	)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Create and run server
	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	defer func() {
		if err := srv.Close(); err != nil {
			logger.Error("error closing server", "error", err)
		}
	}()

	if err := srv.Run(); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
