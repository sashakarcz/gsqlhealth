package health

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gsqlhealth/internal/config"
	"gsqlhealth/internal/database"
)

// Service manages health checks for multiple databases
type Service struct {
	config    *config.Config
	manager   *database.Manager
	factory   *database.DriverFactory
	drivers   map[string]database.Driver // key: "database_name"
	scheduler *Scheduler
	mu        sync.RWMutex
	logger    *slog.Logger
}

// NewService creates a new health check service
func NewService(cfg *config.Config, logger *slog.Logger) *Service {
	service := &Service{
		config:  cfg,
		manager: database.NewManager(),
		factory: database.NewDriverFactory(),
		drivers: make(map[string]database.Driver),
		logger:  logger,
	}

	// Create scheduler
	service.scheduler = NewScheduler(service, logger)

	return service
}

// Initialize sets up the service and starts background database connections
func (s *Service) Initialize(ctx context.Context) error {
	s.logger.Info("Initializing health service")

	// Start the scheduler for periodic health checks (even without database connections)
	if err := s.scheduler.Start(); err != nil {
		s.logger.Error("Failed to start health check scheduler", "error", err)
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	// Start background database connections asynchronously
	go s.initializeDatabaseConnections(ctx)

	// Start background connection recovery
	go s.BackgroundConnectionRecovery(ctx)

	s.logger.Info("Health service initialized, database connections starting in background")
	return nil
}

// initializeDatabaseConnections attempts to connect to all databases in the background
func (s *Service) initializeDatabaseConnections(ctx context.Context) {
	s.logger.Info("Starting database connection initialization in background")

	connector := NewRetryableConnector(&s.config.Retry, s.logger)
	connectedCount := 0

	for _, dbConfig := range s.config.Databases {
		// Start each database connection attempt in its own goroutine
		go func(dbConfig config.Database) {
			driver, err := s.factory.CreateDriver(dbConfig.Type)
			if err != nil {
				s.logger.Error("Failed to create driver",
					"database", dbConfig.Name,
					"type", dbConfig.Type,
					"error", err)
				return
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

			// Attempt connection with infinite retry logic
			if err := connector.ConnectWithRetry(ctx, driver, connInfo, dbConfig.Name); err != nil {
				s.logger.Warn("Database connection initialization cancelled",
					"database", dbConfig.Name,
					"error", err)
				driver.Close()
				return
			}

			// Successfully connected, add to drivers map
			s.mu.Lock()
			s.drivers[dbConfig.Name] = driver
			connectedCount++
			s.mu.Unlock()

			s.logger.Info("Successfully connected to database",
				"database", dbConfig.Name,
				"type", dbConfig.Type,
				"host", dbConfig.Host)
		}(dbConfig)
	}
}

// CheckHealth performs a health check for a specific database and table
func (s *Service) CheckHealth(ctx context.Context, databaseName, tableName string) (*database.HealthResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find the database configuration
	var dbConfig *config.Database
	var tableConfig *config.Table

	for _, db := range s.config.Databases {
		if db.Name == databaseName {
			dbConfig = &db
			for _, table := range db.Tables {
				if table.Name == tableName {
					tableConfig = &table
					break
				}
			}
			break
		}
	}

	if dbConfig == nil {
		return nil, NewNotFoundError(databaseName, "", "database not found in configuration")
	}

	if tableConfig == nil {
		return nil, NewNotFoundError(databaseName, tableName, "table not found in database configuration")
	}

	// Get the driver
	driver, exists := s.drivers[databaseName]
	if !exists {
		return nil, NewConnectionError(databaseName, tableName, "database connection failed", nil)
	}

	// Create result structure
	result := &database.HealthResult{
		DatabaseName: databaseName,
		TableName:    tableName,
		Timestamp:    time.Now(),
	}

	// Set query timeout
	queryCtx, cancel := context.WithTimeout(ctx, tableConfig.GetQueryTimeout())
	defer cancel()

	// Record start time
	startTime := time.Now()

	// Execute the health check query
	data, err := driver.ExecuteHealthCheck(queryCtx, tableConfig.Query)
	result.QueryTime = time.Since(startTime)

	if err != nil {
		result.Status = "unhealthy"
		result.Error = err.Error()
		s.logger.Error("Health check failed",
			"database", databaseName,
			"table", tableName,
			"query_time", result.QueryTime,
			"error", err)

		// Determine error type based on the error message and context
		if queryCtx.Err() == context.DeadlineExceeded {
			return result, NewTimeoutError(databaseName, tableName, "query execution timeout", err)
		} else if s.isConnectionError(err) {
			return result, NewConnectionError(databaseName, tableName, "database connection failed", err)
		} else {
			return result, NewQueryError(databaseName, tableName, "query execution failed", err)
		}
	} else {
		result.Status = "healthy"
		result.Data = data
		s.logger.Debug("Health check successful",
			"database", databaseName,
			"table", tableName,
			"query_time", result.QueryTime)
	}

	return result, nil
}

// CheckDatabaseHealth performs health checks for all tables in a database
func (s *Service) CheckDatabaseHealth(ctx context.Context, databaseName string) ([]*database.HealthResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find the database configuration
	var dbConfig *config.Database
	for _, db := range s.config.Databases {
		if db.Name == databaseName {
			dbConfig = &db
			break
		}
	}

	if dbConfig == nil {
		return nil, fmt.Errorf("database %s not found", databaseName)
	}

	var results []*database.HealthResult
	var wg sync.WaitGroup
	resultsChan := make(chan *database.HealthResult, len(dbConfig.Tables))

	// Execute health checks concurrently for all tables
	for _, table := range dbConfig.Tables {
		wg.Add(1)
		go func(tableName string) {
			defer wg.Done()
			result, err := s.CheckHealth(ctx, databaseName, tableName)
			if err != nil {
				// Create error result if health check fails
				result = &database.HealthResult{
					DatabaseName: databaseName,
					TableName:    tableName,
					Status:       "error",
					Error:        err.Error(),
					Timestamp:    time.Now(),
				}
			}
			resultsChan <- result
		}(table.Name)
	}

	// Wait for all checks to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for result := range resultsChan {
		results = append(results, result)
	}

	return results, nil
}

// CheckAllHealth performs health checks for all databases and tables
func (s *Service) CheckAllHealth(ctx context.Context) (map[string][]*database.HealthResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make(map[string][]*database.HealthResult)
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Execute health checks concurrently for all databases
	for _, dbConfig := range s.config.Databases {
		wg.Add(1)
		go func(databaseName string) {
			defer wg.Done()
			dbResults, err := s.CheckDatabaseHealth(ctx, databaseName)
			if err != nil {
				s.logger.Error("Failed to check database health",
					"database", databaseName,
					"error", err)
				// Create error result for the entire database
				dbResults = []*database.HealthResult{{
					DatabaseName: databaseName,
					TableName:    "all",
					Status:       "error",
					Error:        err.Error(),
					Timestamp:    time.Now(),
				}}
			}

			mu.Lock()
			results[databaseName] = dbResults
			mu.Unlock()
		}(dbConfig.Name)
	}

	wg.Wait()
	return results, nil
}

// Ping tests connectivity to a specific database
func (s *Service) Ping(ctx context.Context, databaseName string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	driver, exists := s.drivers[databaseName]
	if !exists {
		return fmt.Errorf("driver for database %s not initialized", databaseName)
	}

	return driver.Ping(ctx)
}

// Close closes all database connections and stops the scheduler
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop the scheduler first
	s.scheduler.Stop()

	var errors []error
	for name, driver := range s.drivers {
		if err := driver.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close %s: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors closing connections: %v", errors)
	}

	s.logger.Info("All database connections closed and scheduler stopped")
	return nil
}

// GetDatabaseNames returns a list of configured database names
func (s *Service) GetDatabaseNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var names []string
	for _, db := range s.config.Databases {
		names = append(names, db.Name)
	}
	return names
}

// GetTableNames returns a list of table names for a specific database
func (s *Service) GetTableNames(databaseName string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, db := range s.config.Databases {
		if db.Name == databaseName {
			var names []string
			for _, table := range db.Tables {
				names = append(names, table.Name)
			}
			return names, nil
		}
	}

	return nil, fmt.Errorf("database %s not found", databaseName)
}

// isConnectionError determines if an error is related to database connectivity
func (s *Service) isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Common database connection error patterns
	connectionErrors := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"connection lost",
		"no connection",
		"dial tcp",
		"network unreachable",
		"host unreachable",
		"timeout expired",
		"server shutdown",
		"connection closed",
		"broken pipe",
		"connection aborted",
		"connection failure",
		"driver: bad connection",
		"invalid connection",
		"connection is not established",
		"failed to connect",
		"can't connect",
		"unable to connect",
		"database is not available",
		"server not available",
		"server is not available",
		"does not exist",
		"communications link failure",
		"connection string",
	}

	for _, pattern := range connectionErrors {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// GetCachedHealth returns cached health check result for a specific database and table
func (s *Service) GetCachedHealth(databaseName, tableName string) (*database.HealthResult, error, time.Time) {
	return s.scheduler.GetCachedResult(databaseName, tableName)
}

// GetCachedDatabaseHealth returns cached health check results for all tables in a database
func (s *Service) GetCachedDatabaseHealth(databaseName string) ([]*database.HealthResult, error) {
	return s.scheduler.GetCachedDatabaseResults(databaseName)
}

// GetAllCachedHealth returns all cached health check results
func (s *Service) GetAllCachedHealth() map[string][]*database.HealthResult {
	return s.scheduler.GetAllCachedResults()
}

// IsHealthResultFresh checks if a cached health result is still fresh
func (s *Service) IsHealthResultFresh(databaseName, tableName string) bool {
	return s.scheduler.IsResultFresh(databaseName, tableName)
}

// GetCacheStats returns statistics about cached health check results
func (s *Service) GetCacheStats() map[string]interface{} {
	return s.scheduler.GetCacheStats()
}