package health

import "fmt"

// ErrorType represents the type of error that occurred
type ErrorType int

const (
	// ErrorTypeNotFound indicates that a database or table was not found in the configuration
	ErrorTypeNotFound ErrorType = iota
	// ErrorTypeConnection indicates a database connection or communication failure
	ErrorTypeConnection
	// ErrorTypeQuery indicates a query execution failure
	ErrorTypeQuery
	// ErrorTypeTimeout indicates a timeout occurred
	ErrorTypeTimeout
)

// HealthError represents an error that occurred during health check operations
type HealthError struct {
	Type     ErrorType
	Database string
	Table    string
	Message  string
	Cause    error
}

// Error implements the error interface
func (e *HealthError) Error() string {
	if e.Table != "" {
		return fmt.Sprintf("health check error for %s/%s: %s", e.Database, e.Table, e.Message)
	}
	return fmt.Sprintf("health check error for %s: %s", e.Database, e.Message)
}

// Unwrap returns the underlying error
func (e *HealthError) Unwrap() error {
	return e.Cause
}

// IsConnectionError returns true if the error is a connection-related error
func (e *HealthError) IsConnectionError() bool {
	return e.Type == ErrorTypeConnection
}

// IsNotFoundError returns true if the error is a not found error
func (e *HealthError) IsNotFoundError() bool {
	return e.Type == ErrorTypeNotFound
}

// IsQueryError returns true if the error is a query execution error
func (e *HealthError) IsQueryError() bool {
	return e.Type == ErrorTypeQuery
}

// IsTimeoutError returns true if the error is a timeout error
func (e *HealthError) IsTimeoutError() bool {
	return e.Type == ErrorTypeTimeout
}

// NewNotFoundError creates a new not found error
func NewNotFoundError(database, table, message string) *HealthError {
	return &HealthError{
		Type:     ErrorTypeNotFound,
		Database: database,
		Table:    table,
		Message:  message,
	}
}

// NewConnectionError creates a new connection error
func NewConnectionError(database, table, message string, cause error) *HealthError {
	return &HealthError{
		Type:     ErrorTypeConnection,
		Database: database,
		Table:    table,
		Message:  message,
		Cause:    cause,
	}
}

// NewQueryError creates a new query error
func NewQueryError(database, table, message string, cause error) *HealthError {
	return &HealthError{
		Type:     ErrorTypeQuery,
		Database: database,
		Table:    table,
		Message:  message,
		Cause:    cause,
	}
}

// NewTimeoutError creates a new timeout error
func NewTimeoutError(database, table, message string, cause error) *HealthError {
	return &HealthError{
		Type:     ErrorTypeTimeout,
		Database: database,
		Table:    table,
		Message:  message,
		Cause:    cause,
	}
}