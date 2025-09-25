package health

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"gsqlhealth/internal/database"
)

// ScheduledCheck represents a scheduled health check task
type ScheduledCheck struct {
	DatabaseName string
	TableName    string
	Interval     time.Duration
	ticker       *time.Ticker
	stopCh       chan bool
}

// Scheduler manages periodic health checks
type Scheduler struct {
	service     *Service
	logger      *slog.Logger
	checks      map[string]*ScheduledCheck // key: "database/table"
	results     map[string]*CachedResult   // key: "database/table"
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// CachedResult holds a cached health check result with timestamp
type CachedResult struct {
	Result    *database.HealthResult
	Error     error
	UpdatedAt time.Time
	mu        sync.RWMutex
}

// NewScheduler creates a new health check scheduler
func NewScheduler(service *Service, logger *slog.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		service: service,
		logger:  logger,
		checks:  make(map[string]*ScheduledCheck),
		results: make(map[string]*CachedResult),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start initializes and starts all scheduled health checks
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Starting health check scheduler")

	// Create scheduled checks for all configured tables
	for _, dbConfig := range s.service.config.Databases {
		for _, tableConfig := range dbConfig.Tables {
			key := s.getCheckKey(dbConfig.Name, tableConfig.Name)

			scheduledCheck := &ScheduledCheck{
				DatabaseName: dbConfig.Name,
				TableName:    tableConfig.Name,
				Interval:     tableConfig.GetCheckInterval(),
				stopCh:       make(chan bool, 1),
			}

			s.checks[key] = scheduledCheck
			s.results[key] = &CachedResult{
				UpdatedAt: time.Now(),
			}

			// Start the periodic check
			go s.runPeriodicCheck(scheduledCheck)

			s.logger.Info("Scheduled health check",
				"database", dbConfig.Name,
				"table", tableConfig.Name,
				"interval", tableConfig.GetCheckInterval())
		}
	}

	return nil
}

// Stop stops all scheduled health checks
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Stopping health check scheduler")

	// Stop all scheduled checks
	for _, check := range s.checks {
		if check.ticker != nil {
			check.ticker.Stop()
		}
		select {
		case check.stopCh <- true:
		default:
		}
	}

	// Cancel the context
	s.cancel()

	s.logger.Info("Health check scheduler stopped")
}

// runPeriodicCheck runs a periodic health check for a specific database/table
func (s *Scheduler) runPeriodicCheck(check *ScheduledCheck) {
	key := s.getCheckKey(check.DatabaseName, check.TableName)

	// Perform initial check
	s.performHealthCheck(check.DatabaseName, check.TableName, key)

	// Set up ticker for periodic checks
	check.ticker = time.NewTicker(check.Interval)
	defer check.ticker.Stop()

	for {
		select {
		case <-check.ticker.C:
			s.performHealthCheck(check.DatabaseName, check.TableName, key)
		case <-check.stopCh:
			s.logger.Debug("Stopping scheduled check",
				"database", check.DatabaseName,
				"table", check.TableName)
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// performHealthCheck executes a health check and updates the cached result
func (s *Scheduler) performHealthCheck(databaseName, tableName, key string) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	s.logger.Debug("Performing scheduled health check",
		"database", databaseName,
		"table", tableName)

	result, err := s.service.CheckHealth(ctx, databaseName, tableName)

	// Update cached result
	s.mu.RLock()
	cachedResult, exists := s.results[key]
	s.mu.RUnlock()

	if exists {
		cachedResult.mu.Lock()
		cachedResult.Result = result
		cachedResult.Error = err
		cachedResult.UpdatedAt = time.Now()
		cachedResult.mu.Unlock()

		if err != nil {
			s.logger.Warn("Scheduled health check failed",
				"database", databaseName,
				"table", tableName,
				"error", err)
		} else {
			s.logger.Debug("Scheduled health check completed",
				"database", databaseName,
				"table", tableName,
				"status", result.Status)
		}
	}
}

// GetCachedResult returns the cached result for a specific database/table
func (s *Scheduler) GetCachedResult(databaseName, tableName string) (*database.HealthResult, error, time.Time) {
	key := s.getCheckKey(databaseName, tableName)

	s.mu.RLock()
	cachedResult, exists := s.results[key]
	s.mu.RUnlock()

	if !exists {
		return nil, NewNotFoundError(databaseName, tableName, "no cached result available"), time.Time{}
	}

	cachedResult.mu.RLock()
	defer cachedResult.mu.RUnlock()

	return cachedResult.Result, cachedResult.Error, cachedResult.UpdatedAt
}

// GetCachedDatabaseResults returns cached results for all tables in a database
func (s *Scheduler) GetCachedDatabaseResults(databaseName string) ([]*database.HealthResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*database.HealthResult
	found := false

	for key, cachedResult := range s.results {
		if s.checkKeyMatches(key, databaseName, "") {
			found = true
			cachedResult.mu.RLock()
			if cachedResult.Result != nil {
				results = append(results, cachedResult.Result)
			} else if cachedResult.Error != nil {
				// Create error result
				_, tableName := s.parseCheckKey(key)
				errorResult := &database.HealthResult{
					DatabaseName: databaseName,
					TableName:    tableName,
					Status:       "error",
					Error:        cachedResult.Error.Error(),
					Timestamp:    cachedResult.UpdatedAt,
				}
				results = append(results, errorResult)
			}
			cachedResult.mu.RUnlock()
		}
	}

	if !found {
		return nil, NewNotFoundError(databaseName, "", "database not found in configuration")
	}

	return results, nil
}

// GetAllCachedResults returns all cached health check results
func (s *Scheduler) GetAllCachedResults() map[string][]*database.HealthResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make(map[string][]*database.HealthResult)

	for key, cachedResult := range s.results {
		databaseName, _ := s.parseCheckKey(key)

		cachedResult.mu.RLock()
		if cachedResult.Result != nil {
			results[databaseName] = append(results[databaseName], cachedResult.Result)
		} else if cachedResult.Error != nil {
			// Create error result
			_, tableName := s.parseCheckKey(key)
			errorResult := &database.HealthResult{
				DatabaseName: databaseName,
				TableName:    tableName,
				Status:       "error",
				Error:        cachedResult.Error.Error(),
				Timestamp:    cachedResult.UpdatedAt,
			}
			results[databaseName] = append(results[databaseName], errorResult)
		}
		cachedResult.mu.RUnlock()
	}

	return results
}

// IsResultFresh checks if a cached result is still fresh (within the check interval)
func (s *Scheduler) IsResultFresh(databaseName, tableName string) bool {
	key := s.getCheckKey(databaseName, tableName)

	s.mu.RLock()
	check, checkExists := s.checks[key]
	cachedResult, resultExists := s.results[key]
	s.mu.RUnlock()

	if !checkExists || !resultExists {
		return false
	}

	cachedResult.mu.RLock()
	updatedAt := cachedResult.UpdatedAt
	cachedResult.mu.RUnlock()

	// Consider result fresh if it's within the check interval
	return time.Since(updatedAt) < check.Interval
}

// getCheckKey creates a unique key for a database/table combination
func (s *Scheduler) getCheckKey(databaseName, tableName string) string {
	return databaseName + "/" + tableName
}

// parseCheckKey parses a check key back into database and table names
func (s *Scheduler) parseCheckKey(key string) (string, string) {
	parts := make([]string, 2)
	if idx := len(key); idx > 0 {
		for i, char := range key {
			if char == '/' {
				parts[0] = key[:i]
				parts[1] = key[i+1:]
				break
			}
		}
	}
	return parts[0], parts[1]
}

// checkKeyMatches checks if a key matches database and table criteria
func (s *Scheduler) checkKeyMatches(key, databaseName, tableName string) bool {
	db, table := s.parseCheckKey(key)

	if db != databaseName {
		return false
	}

	if tableName != "" && table != tableName {
		return false
	}

	return true
}

// GetCacheStats returns statistics about the cached results
func (s *Scheduler) GetCacheStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalChecks := len(s.results)
	freshResults := 0
	healthyResults := 0
	unhealthyResults := 0

	for key, cachedResult := range s.results {
		cachedResult.mu.RLock()
		if cachedResult.Result != nil {
			if cachedResult.Result.Status == "healthy" {
				healthyResults++
			} else {
				unhealthyResults++
			}
		}
		cachedResult.mu.RUnlock()

		databaseName, tableName := s.parseCheckKey(key)
		if s.IsResultFresh(databaseName, tableName) {
			freshResults++
		}
	}

	return map[string]interface{}{
		"total_checks":     totalChecks,
		"fresh_results":    freshResults,
		"healthy_results":  healthyResults,
		"unhealthy_results": unhealthyResults,
	}
}