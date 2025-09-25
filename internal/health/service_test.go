package health

import (
	"context"
	"errors"
	"testing"

	"gsqlhealth/internal/config"
)

func TestIsConnectionError(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      errors.New("dial tcp 127.0.0.1:3306: connection refused"),
			expected: true,
		},
		{
			name:     "connection timeout",
			err:      errors.New("dial tcp 10.0.0.1:5432: connection timeout"),
			expected: true,
		},
		{
			name:     "connection reset",
			err:      errors.New("read tcp 127.0.0.1:3306: connection reset by peer"),
			expected: true,
		},
		{
			name:     "driver bad connection",
			err:      errors.New("driver: bad connection"),
			expected: true,
		},
		{
			name:     "network unreachable",
			err:      errors.New("dial tcp 192.168.1.1:1433: network unreachable"),
			expected: true,
		},
		{
			name:     "server not available",
			err:      errors.New("SQL Server is not available or does not exist"),
			expected: true,
		},
		{
			name:     "SQL syntax error - not a connection error",
			err:      errors.New("syntax error at or near 'SELCT'"),
			expected: false,
		},
		{
			name:     "table not found - not a connection error",
			err:      errors.New("table 'test.nonexistent' doesn't exist"),
			expected: false,
		},
		{
			name:     "permission denied - not a connection error",
			err:      errors.New("access denied for user 'readonly'@'localhost'"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.isConnectionError(tt.err)
			if result != tt.expected {
				t.Errorf("isConnectionError(%v) = %v; expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestHealthErrorTypes(t *testing.T) {
	tests := []struct {
		name          string
		errorFunc     func() *HealthError
		expectedType  ErrorType
		expectedIsConn bool
		expectedIsNF   bool
		expectedIsQuery bool
		expectedIsTO   bool
	}{
		{
			name: "connection error",
			errorFunc: func() *HealthError {
				return NewConnectionError("testdb", "testtable", "connection failed", errors.New("dial tcp: connection refused"))
			},
			expectedType:    ErrorTypeConnection,
			expectedIsConn:  true,
			expectedIsNF:    false,
			expectedIsQuery: false,
			expectedIsTO:    false,
		},
		{
			name: "not found error",
			errorFunc: func() *HealthError {
				return NewNotFoundError("testdb", "testtable", "table not found")
			},
			expectedType:    ErrorTypeNotFound,
			expectedIsConn:  false,
			expectedIsNF:    true,
			expectedIsQuery: false,
			expectedIsTO:    false,
		},
		{
			name: "query error",
			errorFunc: func() *HealthError {
				return NewQueryError("testdb", "testtable", "syntax error", errors.New("invalid SQL"))
			},
			expectedType:    ErrorTypeQuery,
			expectedIsConn:  false,
			expectedIsNF:    false,
			expectedIsQuery: true,
			expectedIsTO:    false,
		},
		{
			name: "timeout error",
			errorFunc: func() *HealthError {
				return NewTimeoutError("testdb", "testtable", "query timeout", context.DeadlineExceeded)
			},
			expectedType:    ErrorTypeTimeout,
			expectedIsConn:  false,
			expectedIsNF:    false,
			expectedIsQuery: false,
			expectedIsTO:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errorFunc()

			if err.Type != tt.expectedType {
				t.Errorf("Error type = %v; expected %v", err.Type, tt.expectedType)
			}

			if err.IsConnectionError() != tt.expectedIsConn {
				t.Errorf("IsConnectionError() = %v; expected %v", err.IsConnectionError(), tt.expectedIsConn)
			}

			if err.IsNotFoundError() != tt.expectedIsNF {
				t.Errorf("IsNotFoundError() = %v; expected %v", err.IsNotFoundError(), tt.expectedIsNF)
			}

			if err.IsQueryError() != tt.expectedIsQuery {
				t.Errorf("IsQueryError() = %v; expected %v", err.IsQueryError(), tt.expectedIsQuery)
			}

			if err.IsTimeoutError() != tt.expectedIsTO {
				t.Errorf("IsTimeoutError() = %v; expected %v", err.IsTimeoutError(), tt.expectedIsTO)
			}
		})
	}
}

func TestHealthErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		err      *HealthError
		expected string
	}{
		{
			name: "with table",
			err:  NewConnectionError("mydb", "mytable", "connection failed", nil),
			expected: "health check error for mydb/mytable: connection failed",
		},
		{
			name: "without table",
			err:  NewConnectionError("mydb", "", "connection failed", nil),
			expected: "health check error for mydb: connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Error() = %q; expected %q", tt.err.Error(), tt.expected)
			}
		})
	}
}

func TestCreateService(t *testing.T) {
	cfg := &config.Config{
		Databases: []config.Database{
			{
				Name:     "test",
				Type:     "mysql",
				Host:     "localhost",
				Port:     3306,
				Username: "user",
				Password: "pass",
				Database: "db",
				Tables: []config.Table{
					{Name: "table1", Query: "SELECT 1", Timeout: 5, CheckInterval: 30},
				},
			},
		},
		Server: config.Server{
			Host:         "localhost",
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
			IdleTimeout:  120,
		},
	}

	service := NewService(cfg, nil)
	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if len(service.drivers) != 0 {
		t.Errorf("Expected 0 drivers initially, got %d", len(service.drivers))
	}

	databases := service.GetDatabaseNames()
	if len(databases) != 1 || databases[0] != "test" {
		t.Errorf("Expected [test], got %v", databases)
	}

	tables, err := service.GetTableNames("test")
	if err != nil {
		t.Errorf("GetTableNames failed: %v", err)
	}
	if len(tables) != 1 || tables[0] != "table1" {
		t.Errorf("Expected [table1], got %v", tables)
	}

	_, err = service.GetTableNames("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent database")
	}
}