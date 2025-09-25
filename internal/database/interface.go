package database

import (
	"context"
	"fmt"
	"time"
)

// HealthResult represents the result of a health check query
type HealthResult struct {
	DatabaseName string                 `json:"database_name"`
	TableName    string                 `json:"table_name"`
	Status       string                 `json:"status"`
	Data         map[string]interface{} `json:"data,omitempty"`
	Error        string                 `json:"error,omitempty"`
	QueryTime    time.Duration          `json:"query_time"`
	Timestamp    time.Time              `json:"timestamp"`
}

// ConnectionInfo holds database connection information
type ConnectionInfo struct {
	Host     string
	Port     int
	Username string
	Password string
	Database string
	SSLMode  string
	Timeout  time.Duration
}

// Driver interface defines the contract for database drivers
type Driver interface {
	// Connect establishes a connection to the database
	Connect(ctx context.Context, info ConnectionInfo) error

	// Close closes the database connection
	Close() error

	// ExecuteHealthCheck executes a health check query
	ExecuteHealthCheck(ctx context.Context, query string) (map[string]interface{}, error)

	// Ping tests the database connection
	Ping(ctx context.Context) error

	// GetDriverName returns the name of the database driver
	GetDriverName() string
}

// Manager manages database connections and health checks
type Manager struct {
	drivers map[string]Driver
}

// NewManager creates a new database manager
func NewManager() *Manager {
	return &Manager{
		drivers: make(map[string]Driver),
	}
}

// RegisterDriver registers a database driver
func (m *Manager) RegisterDriver(dbType string, driver Driver) {
	m.drivers[dbType] = driver
}

// GetDriver returns a driver for the specified database type
func (m *Manager) GetDriver(dbType string) (Driver, error) {
	driver, exists := m.drivers[dbType]
	if !exists {
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
	return driver, nil
}

// GetSupportedTypes returns a list of supported database types
func (m *Manager) GetSupportedTypes() []string {
	types := make([]string, 0, len(m.drivers))
	for dbType := range m.drivers {
		types = append(types, dbType)
	}
	return types
}

// DriverFactory creates database drivers
type DriverFactory struct{}

// NewDriverFactory creates a new driver factory
func NewDriverFactory() *DriverFactory {
	return &DriverFactory{}
}

// CreateDriver creates a new driver instance for the specified database type
func (f *DriverFactory) CreateDriver(dbType string) (Driver, error) {
	switch dbType {
	case "mysql":
		return NewMySQLDriver(), nil
	case "postgres":
		return NewPostgreSQLDriver(), nil
	case "mssql":
		return NewMSSQLDriver(), nil
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}