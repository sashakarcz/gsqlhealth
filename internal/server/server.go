package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gsqlhealth/internal/config"
	"gsqlhealth/internal/database"
	"gsqlhealth/internal/health"

	"github.com/gorilla/mux"
)

// Server represents the HTTP server
type Server struct {
	config        *config.Config
	healthService *health.Service
	logger        *slog.Logger
	httpServer    *http.Server
}

// NewServer creates a new HTTP server instance
func NewServer(cfg *config.Config, healthService *health.Service, logger *slog.Logger) *Server {
	return &Server{
		config:        cfg,
		healthService: healthService,
		logger:        logger,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	router := s.setupRoutes()

	s.httpServer = &http.Server{
		Addr:         s.config.Server.GetAddress(),
		Handler:      router,
		ReadTimeout:  s.config.Server.GetReadTimeout(),
		WriteTimeout: s.config.Server.GetWriteTimeout(),
		IdleTimeout:  s.config.Server.GetIdleTimeout(),
	}

	s.logger.Info("Starting HTTP server",
		"address", s.config.Server.GetAddress(),
		"read_timeout", s.config.Server.GetReadTimeout(),
		"write_timeout", s.config.Server.GetWriteTimeout())

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}

// setupRoutes configures the HTTP routes
func (s *Server) setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Middleware
	router.Use(s.loggingMiddleware)
	router.Use(s.corsMiddleware)
	router.Use(s.recoveryMiddleware)

	// Health check endpoints
	router.HandleFunc("/health", s.handleOverallHealth).Methods("GET")
	router.HandleFunc("/health/{database}", s.handleDatabaseHealth).Methods("GET")
	router.HandleFunc("/health/{database}/{table}", s.handleTableHealth).Methods("GET")

	// Info endpoints
	router.HandleFunc("/databases", s.handleListDatabases).Methods("GET")
	router.HandleFunc("/databases/{database}/tables", s.handleListTables).Methods("GET")

	// Ping endpoints
	router.HandleFunc("/ping/{database}", s.handlePing).Methods("GET")

	// Cache statistics endpoint
	router.HandleFunc("/cache/stats", s.handleCacheStats).Methods("GET")

	// Root endpoint
	router.HandleFunc("/", s.handleRoot).Methods("GET")

	return router
}

// handleOverallHealth handles requests to /health
func (s *Server) handleOverallHealth(w http.ResponseWriter, r *http.Request) {
	// Check if we should force real-time checks
	forceRealTime := r.URL.Query().Get("realtime") == "true"

	var results map[string][]*database.HealthResult
	var err error

	if forceRealTime {
		// Perform real-time health checks
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		results, err = s.healthService.CheckAllHealth(ctx)
		if err != nil {
			s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to perform health checks", err)
			return
		}
	} else {
		// Use cached results
		results = s.healthService.GetAllCachedHealth()
	}

	// Calculate overall status and check for connection errors
	overallStatus := "healthy"
	totalChecks := 0
	healthyChecks := 0
	hasConnectionError := false
	hasTimeout := false

	for _, dbResults := range results {
		for _, result := range dbResults {
			totalChecks++
			if result.Status == "healthy" {
				healthyChecks++
			} else {
				overallStatus = "unhealthy"

				// Check if this is a connection error based on error message
				if result.Error != "" {
					if s.isConnectionErrorMessage(result.Error) {
						hasConnectionError = true
					} else if s.isTimeoutErrorMessage(result.Error) {
						hasTimeout = true
					}
				}
			}
		}
	}

	// Return appropriate HTTP status code based on error types
	var statusCode int
	if hasConnectionError {
		statusCode = http.StatusServiceUnavailable
	} else if hasTimeout {
		statusCode = http.StatusGatewayTimeout
	} else {
		statusCode = http.StatusOK
	}

	response := map[string]interface{}{
		"status":         overallStatus,
		"total_checks":   totalChecks,
		"healthy_checks": healthyChecks,
		"timestamp":      time.Now(),
		"databases":      results,
	}

	s.writeJSONResponse(w, statusCode, response)
}

// handleDatabaseHealth handles requests to /health/{database}
func (s *Server) handleDatabaseHealth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	databaseName := vars["database"]

	// Check if we should force real-time checks
	forceRealTime := r.URL.Query().Get("realtime") == "true"

	var results []*database.HealthResult
	var err error

	if forceRealTime {
		// Perform real-time health checks
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		results, err = s.healthService.CheckDatabaseHealth(ctx, databaseName)
		if err != nil {
			statusCode, message := s.getErrorResponse(err, databaseName, "")
			s.writeErrorResponse(w, statusCode, message, err)
			return
		}
	} else {
		// Use cached results
		results, err = s.healthService.GetCachedDatabaseHealth(databaseName)
		if err != nil {
			statusCode, message := s.getErrorResponse(err, databaseName, "")
			s.writeErrorResponse(w, statusCode, message, err)
			return
		}
	}

	// Calculate database status and check for connection errors
	databaseStatus := "healthy"
	hasConnectionError := false
	hasTimeout := false

	for _, result := range results {
		if result.Status != "healthy" {
			databaseStatus = "unhealthy"

			// Check if this is a connection error based on error message
			if result.Error != "" {
				if s.isConnectionErrorMessage(result.Error) {
					hasConnectionError = true
				} else if s.isTimeoutErrorMessage(result.Error) {
					hasTimeout = true
				}
			}
		}
	}

	// Return appropriate HTTP status code based on error types
	var statusCode int
	if hasConnectionError {
		statusCode = http.StatusServiceUnavailable
	} else if hasTimeout {
		statusCode = http.StatusGatewayTimeout
	} else {
		statusCode = http.StatusOK
	}

	response := map[string]interface{}{
		"database":  databaseName,
		"status":    databaseStatus,
		"tables":    results,
		"timestamp": time.Now(),
	}

	s.writeJSONResponse(w, statusCode, response)
}

// handleTableHealth handles requests to /health/{database}/{table}
func (s *Server) handleTableHealth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	databaseName := vars["database"]
	tableName := vars["table"]

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if we should force real-time checks
	forceRealTime := r.URL.Query().Get("realtime") == "true"

	if forceRealTime {
		// Perform real-time health check
		result, err := s.healthService.CheckHealth(ctx, databaseName, tableName)
		if err != nil {
			statusCode, message := s.getErrorResponse(err, databaseName, tableName)
			s.writeErrorResponse(w, statusCode, message, err)
			return
		}

		// Check if result indicates connection or timeout error
		var statusCode int
		if result.Status != "healthy" && result.Error != "" {
			if s.isConnectionErrorMessage(result.Error) {
				statusCode = http.StatusServiceUnavailable
			} else if s.isTimeoutErrorMessage(result.Error) {
				statusCode = http.StatusGatewayTimeout
			} else {
				statusCode = http.StatusOK
			}
		} else {
			statusCode = http.StatusOK
		}

		s.writeJSONResponse(w, statusCode, result)
	} else {
		// Use cached result
		result, err, updatedAt := s.healthService.GetCachedHealth(databaseName, tableName)
		if err != nil {
			statusCode, message := s.getErrorResponse(err, databaseName, tableName)
			s.writeErrorResponse(w, statusCode, message, err)
			return
		}

		// Check if cached result indicates connection or timeout error
		var statusCode int
		if result != nil && result.Status != "healthy" && result.Error != "" {
			if s.isConnectionErrorMessage(result.Error) {
				statusCode = http.StatusServiceUnavailable
			} else if s.isTimeoutErrorMessage(result.Error) {
				statusCode = http.StatusGatewayTimeout
			} else {
				statusCode = http.StatusOK
			}
		} else {
			statusCode = http.StatusOK
		}

		// Add cache metadata to response
		response := map[string]interface{}{
			"result":        result,
			"cached":        true,
			"last_updated":  updatedAt,
			"is_fresh":      s.healthService.IsHealthResultFresh(databaseName, tableName),
		}

		s.writeJSONResponse(w, statusCode, response)
	}
}

// handleListDatabases handles requests to /databases
func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	databases := s.healthService.GetDatabaseNames()

	response := map[string]interface{}{
		"databases": databases,
		"count":     len(databases),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleListTables handles requests to /databases/{database}/tables
func (s *Server) handleListTables(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	databaseName := vars["database"]

	tables, err := s.healthService.GetTableNames(databaseName)
	if err != nil {
		s.writeErrorResponse(w, http.StatusNotFound, fmt.Sprintf("Database '%s' not found", databaseName), err)
		return
	}

	response := map[string]interface{}{
		"database": databaseName,
		"tables":   tables,
		"count":    len(tables),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handlePing handles requests to /ping/{database}
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	databaseName := vars["database"]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	startTime := time.Now()
	err := s.healthService.Ping(ctx, databaseName)
	pingTime := time.Since(startTime)

	if err != nil {
		response := map[string]interface{}{
			"database":  databaseName,
			"status":    "unreachable",
			"error":     err.Error(),
			"ping_time": pingTime,
			"timestamp": time.Now(),
		}
		s.writeJSONResponse(w, http.StatusServiceUnavailable, response)
		return
	}

	response := map[string]interface{}{
		"database":  databaseName,
		"status":    "reachable",
		"ping_time": pingTime,
		"timestamp": time.Now(),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleCacheStats handles requests to /cache/stats
func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	stats := s.healthService.GetCacheStats()

	response := map[string]interface{}{
		"cache_stats": stats,
		"timestamp":   time.Now(),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleRoot handles requests to /
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"service":   "gsqlhealth",
		"version":   "1.0.0",
		"endpoints": []string{
			"/health",
			"/health/{database}",
			"/health/{database}/{table}",
			"/databases",
			"/databases/{database}/tables",
			"/ping/{database}",
			"/cache/stats",
		},
		"query_parameters": map[string]string{
			"realtime": "Set to 'true' to force real-time health checks instead of using cached results",
		},
		"timestamp": time.Now(),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// writeJSONResponse writes a JSON response
func (s *Server) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// writeErrorResponse writes an error response
func (s *Server) writeErrorResponse(w http.ResponseWriter, statusCode int, message string, err error) {
	s.logger.Error("HTTP error response",
		"status_code", statusCode,
		"message", message,
		"error", err)

	response := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now(),
	}

	if err != nil {
		response["details"] = err.Error()
	}

	s.writeJSONResponse(w, statusCode, response)
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture the status code
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		s.logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.statusCode,
			"duration", time.Since(start),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent())
	})
}

// corsMiddleware adds CORS headers
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// recoveryMiddleware recovers from panics
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.logger.Error("Panic recovered",
					"error", err,
					"path", r.URL.Path,
					"method", r.Method)

				s.writeErrorResponse(w, http.StatusInternalServerError,
					"Internal server error", fmt.Errorf("%v", err))
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// getErrorResponse determines the appropriate HTTP status code and message for health errors
func (s *Server) getErrorResponse(err error, database, table string) (int, string) {
	var healthError *health.HealthError
	if errors.As(err, &healthError) {
		switch {
		case healthError.IsNotFoundError():
			if table != "" {
				return http.StatusNotFound, fmt.Sprintf("Database '%s' or table '%s' not found", database, table)
			}
			return http.StatusNotFound, fmt.Sprintf("Database '%s' not found", database)
		case healthError.IsConnectionError():
			if table != "" {
				return http.StatusServiceUnavailable, fmt.Sprintf("Cannot connect to database '%s' for table '%s'", database, table)
			}
			return http.StatusServiceUnavailable, fmt.Sprintf("Cannot connect to database '%s'", database)
		case healthError.IsTimeoutError():
			if table != "" {
				return http.StatusGatewayTimeout, fmt.Sprintf("Timeout querying table '%s' in database '%s'", table, database)
			}
			return http.StatusGatewayTimeout, fmt.Sprintf("Timeout connecting to database '%s'", database)
		case healthError.IsQueryError():
			if table != "" {
				return http.StatusBadRequest, fmt.Sprintf("Query failed for table '%s' in database '%s'", table, database)
			}
			return http.StatusBadRequest, fmt.Sprintf("Query failed for database '%s'", database)
		default:
			if table != "" {
				return http.StatusInternalServerError, fmt.Sprintf("Internal error checking table '%s' in database '%s'", table, database)
			}
			return http.StatusInternalServerError, fmt.Sprintf("Internal error checking database '%s'", database)
		}
	}

	// Fallback for non-HealthError types
	if table != "" {
		return http.StatusNotFound, fmt.Sprintf("Database '%s' or table '%s' not found", database, table)
	}
	return http.StatusNotFound, fmt.Sprintf("Database '%s' not found", database)
}

// isConnectionErrorMessage checks if an error message indicates a connection failure
func (s *Server) isConnectionErrorMessage(errorMsg string) bool {
	if errorMsg == "" {
		return false
	}

	errStr := strings.ToLower(errorMsg)

	connectionPatterns := []string{
		"database connection failed",
		"connection refused",
		"connection reset",
		"connection timeout",
		"connection lost",
		"no connection",
		"dial tcp",
		"network unreachable",
		"host unreachable",
		"server not available",
		"server is not available",
		"does not exist",
		"database is not available",
		"communications link failure",
		"driver: bad connection",
		"invalid connection",
		"connection is not established",
		"failed to connect",
		"can't connect",
		"unable to connect",
	}

	for _, pattern := range connectionPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// isTimeoutErrorMessage checks if an error message indicates a timeout
func (s *Server) isTimeoutErrorMessage(errorMsg string) bool {
	if errorMsg == "" {
		return false
	}

	errStr := strings.ToLower(errorMsg)

	timeoutPatterns := []string{
		"timeout",
		"deadline exceeded",
		"query timeout",
		"execution timeout",
		"connection timeout",
	}

	for _, pattern := range timeoutPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}