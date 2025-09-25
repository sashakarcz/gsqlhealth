package server

import (
	"testing"
)

func TestIsConnectionErrorMessage(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name     string
		errorMsg string
		expected bool
	}{
		{
			name:     "empty error",
			errorMsg: "",
			expected: false,
		},
		{
			name:     "database connection failed",
			errorMsg: "health check error for comet/comet: database connection failed",
			expected: true,
		},
		{
			name:     "connection refused",
			errorMsg: "dial tcp 127.0.0.1:3306: connection refused",
			expected: true,
		},
		{
			name:     "network unreachable",
			errorMsg: "dial tcp 192.168.1.1:5432: network unreachable",
			expected: true,
		},
		{
			name:     "server not available",
			errorMsg: "SQL Server is not available or does not exist",
			expected: true,
		},
		{
			name:     "syntax error - not connection",
			errorMsg: "syntax error at or near 'SELCT'",
			expected: false,
		},
		{
			name:     "permission denied - not connection",
			errorMsg: "access denied for user 'readonly'@'localhost'",
			expected: false,
		},
		{
			name:     "table not found - not connection",
			errorMsg: "table 'test.nonexistent' doesn't exist",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.isConnectionErrorMessage(tt.errorMsg)
			if result != tt.expected {
				t.Errorf("isConnectionErrorMessage(%q) = %v; expected %v", tt.errorMsg, result, tt.expected)
			}
		})
	}
}

func TestIsTimeoutErrorMessage(t *testing.T) {
	server := &Server{}

	tests := []struct {
		name     string
		errorMsg string
		expected bool
	}{
		{
			name:     "empty error",
			errorMsg: "",
			expected: false,
		},
		{
			name:     "query timeout",
			errorMsg: "health check error for db/table: query execution timeout",
			expected: true,
		},
		{
			name:     "deadline exceeded",
			errorMsg: "context deadline exceeded",
			expected: true,
		},
		{
			name:     "execution timeout",
			errorMsg: "execution timeout occurred",
			expected: true,
		},
		{
			name:     "connection timeout",
			errorMsg: "connection timeout after 30 seconds",
			expected: true,
		},
		{
			name:     "syntax error - not timeout",
			errorMsg: "syntax error in SQL query",
			expected: false,
		},
		{
			name:     "permission error - not timeout",
			errorMsg: "access denied for user",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.isTimeoutErrorMessage(tt.errorMsg)
			if result != tt.expected {
				t.Errorf("isTimeoutErrorMessage(%q) = %v; expected %v", tt.errorMsg, result, tt.expected)
			}
		})
	}
}