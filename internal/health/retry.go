package health

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gsqlhealth/internal/config"
	"gsqlhealth/internal/database"
)

// RetryableConnector handles connection attempts with retry logic
type RetryableConnector struct {
	config    *config.Retry
	logger    *slog.Logger
}

// NewRetryableConnector creates a new retryable connector
func NewRetryableConnector(retryConfig *config.Retry, logger *slog.Logger) *RetryableConnector {
	return &RetryableConnector{
		config: retryConfig,
		logger: logger,
	}
}

// ConnectWithRetry attempts to connect to a database with retry logic
func (r *RetryableConnector) ConnectWithRetry(ctx context.Context, driver database.Driver, connInfo database.ConnectionInfo, databaseName string) error {
	var lastError error
	delay := r.config.GetInitialDelay()
	attempt := 1

	for {
		// Attempt connection
		r.logger.Info("Attempting database connection",
			"database", databaseName,
			"attempt", attempt,
			"delay", delay)

		err := driver.Connect(ctx, connInfo)
		if err == nil {
			r.logger.Info("Successfully connected to database",
				"database", databaseName,
				"attempt", attempt)
			return nil
		}

		lastError = err
		r.logger.Warn("Database connection failed, will retry",
			"database", databaseName,
			"attempt", attempt,
			"error", err,
			"next_retry_in", delay)

		// Check if we should stop retrying
		if r.config.MaxAttempts > 0 && attempt >= r.config.MaxAttempts {
			r.logger.Error("Max connection attempts reached",
				"database", databaseName,
				"max_attempts", r.config.MaxAttempts,
				"last_error", lastError)
			return fmt.Errorf("failed to connect after %d attempts: %w", attempt, lastError)
		}

		// Wait before next attempt (with context cancellation support)
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			r.logger.Info("Connection retry cancelled",
				"database", databaseName,
				"attempt", attempt)
			return ctx.Err()
		}

		// Calculate next delay with exponential backoff
		delay = r.calculateNextDelay(delay)
		attempt++
	}
}

// calculateNextDelay calculates the next retry delay with exponential backoff
func (r *RetryableConnector) calculateNextDelay(currentDelay time.Duration) time.Duration {
	nextDelay := time.Duration(float64(currentDelay) * float64(r.config.BackoffFactor))

	maxDelay := r.config.GetMaxDelay()
	if nextDelay > maxDelay {
		nextDelay = maxDelay
	}

	return nextDelay
}

// BackgroundConnectionRecovery runs background connection recovery for failed databases
func (s *Service) BackgroundConnectionRecovery(ctx context.Context) {
	ticker := time.NewTicker(s.config.Retry.GetConnectionRetry())
	defer ticker.Stop()

	s.logger.Info("Starting background connection recovery",
		"retry_interval", s.config.Retry.GetConnectionRetry())

	for {
		select {
		case <-ticker.C:
			s.attemptConnectionRecovery(ctx)
		case <-ctx.Done():
			s.logger.Info("Background connection recovery stopped")
			return
		}
	}
}

// attemptConnectionRecovery attempts to reconnect to failed databases
func (s *Service) attemptConnectionRecovery(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, dbConfig := range s.config.Databases {
		// Check if this database is already connected
		if driver, exists := s.drivers[dbConfig.Name]; exists {
			// Test if connection is still alive
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := driver.Ping(pingCtx); err == nil {
				cancel()
				continue // Connection is healthy
			}
			cancel()

			// Connection is dead, remove it
			s.logger.Warn("Database connection is dead, attempting recovery",
				"database", dbConfig.Name)
			driver.Close()
			delete(s.drivers, dbConfig.Name)
		}

		// Attempt to reconnect
		s.logger.Info("Attempting database recovery",
			"database", dbConfig.Name)

		driver, err := s.factory.CreateDriver(dbConfig.Type)
		if err != nil {
			s.logger.Error("Failed to create driver for recovery",
				"database", dbConfig.Name,
				"error", err)
			continue
		}

		connInfo := database.ConnectionInfo{
			Host:     dbConfig.Host,
			Port:     dbConfig.Port,
			Username: dbConfig.Username,
			Password: dbConfig.Password,
			Database: dbConfig.Database,
			SSLMode:  dbConfig.SSLMode,
			Timeout:  30 * time.Second,
		}

		// Use single attempt for recovery (don't block the recovery loop)
		connCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = driver.Connect(connCtx, connInfo)
		cancel()

		if err == nil {
			s.drivers[dbConfig.Name] = driver
			s.logger.Info("Database connection recovered",
				"database", dbConfig.Name)
		} else {
			s.logger.Debug("Database recovery failed, will try again later",
				"database", dbConfig.Name,
				"error", err)
			driver.Close()
		}
	}
}

// IsConnected checks if a database is currently connected
func (s *Service) IsConnected(databaseName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	driver, exists := s.drivers[databaseName]
	if !exists {
		return false
	}

	// Quick ping test
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return driver.Ping(ctx) == nil
}