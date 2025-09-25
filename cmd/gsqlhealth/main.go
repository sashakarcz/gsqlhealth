package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gsqlhealth/internal/config"
	"gsqlhealth/internal/health"
	"gsqlhealth/internal/server"
)

const (
	defaultConfigPath = "config.yaml"
	shutdownTimeout   = 30 * time.Second
)

func main() {
	// Parse command line flags
	var (
		configPath = flag.String("config", defaultConfigPath, "Path to configuration file")
		version    = flag.Bool("version", false, "Show version information")
		validate   = flag.Bool("validate", false, "Validate configuration and exit")
	)
	flag.Parse()

	// Show version
	if *version {
		fmt.Println("gsqlhealth v1.0.0")
		fmt.Println("Database health monitoring service")
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Validate configuration and exit if requested
	if *validate {
		fmt.Println("Configuration is valid")
		os.Exit(0)
	}

	// Setup logger
	logger := setupLogger(cfg.Logging)
	logger.Info("Starting gsqlhealth",
		"version", "1.0.0",
		"config_path", *configPath)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create health service
	healthService := health.NewService(cfg, logger)

	// Initialize database connections
	logger.Info("Initializing database connections")
	if err := healthService.Initialize(ctx); err != nil {
		logger.Error("Failed to initialize health service", "error", err)
		os.Exit(1)
	}

	// Create HTTP server
	httpServer := server.NewServer(cfg, healthService, logger)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start HTTP server in a goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		logger.Info("HTTP server starting",
			"address", cfg.Server.GetAddress())

		if err := httpServer.Start(); err != nil {
			serverErrChan <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErrChan:
		if err != nil {
			logger.Error("HTTP server error", "error", err)
		}
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", "signal", sig.String())
	}

	// Graceful shutdown
	logger.Info("Shutting down gracefully", "timeout", shutdownTimeout)

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down HTTP server", "error", err)
	}

	// Close database connections
	if err := healthService.Close(); err != nil {
		logger.Error("Error closing database connections", "error", err)
	}

	logger.Info("Shutdown complete")
}

// setupLogger creates and configures the logger based on configuration
func setupLogger(logConfig config.Logging) *slog.Logger {
	var level slog.Level

	// Parse log level
	switch logConfig.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Create handler options
	opts := &slog.HandlerOptions{
		Level: level,
		AddSource: level == slog.LevelDebug, // Add source info for debug level
	}

	var handler slog.Handler

	// Choose handler based on format
	switch logConfig.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		// Default to JSON for better structured logging
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}