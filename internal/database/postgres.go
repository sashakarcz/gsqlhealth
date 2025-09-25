package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// PostgreSQLDriver implements the Driver interface for PostgreSQL databases
type PostgreSQLDriver struct {
	db *sql.DB
}

// NewPostgreSQLDriver creates a new PostgreSQL driver instance
func NewPostgreSQLDriver() *PostgreSQLDriver {
	return &PostgreSQLDriver{}
}

// Connect establishes a connection to the PostgreSQL database
func (d *PostgreSQLDriver) Connect(ctx context.Context, info ConnectionInfo) error {
	dsn := d.buildDSN(info)

	var err error
	d.db, err = sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}

	// Configure connection pool settings
	d.db.SetMaxOpenConns(25)
	d.db.SetMaxIdleConns(5)
	d.db.SetConnMaxLifetime(5 * time.Minute)
	d.db.SetConnMaxIdleTime(1 * time.Minute)

	// Test the connection
	ctx, cancel := context.WithTimeout(ctx, info.Timeout)
	defer cancel()

	if err := d.db.PingContext(ctx); err != nil {
		d.db.Close()
		return fmt.Errorf("failed to ping PostgreSQL database: %w", err)
	}

	return nil
}

// Close closes the PostgreSQL database connection
func (d *PostgreSQLDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// ExecuteHealthCheck executes a health check query and returns the results
func (d *PostgreSQLDriver) ExecuteHealthCheck(ctx context.Context, query string) (map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("database connection is not established")
	}

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return d.processRows(rows)
}

// Ping tests the database connection
func (d *PostgreSQLDriver) Ping(ctx context.Context) error {
	if d.db == nil {
		return fmt.Errorf("database connection is not established")
	}
	return d.db.PingContext(ctx)
}

// GetDriverName returns the name of the database driver
func (d *PostgreSQLDriver) GetDriverName() string {
	return "postgres"
}

// buildDSN constructs the PostgreSQL data source name
func (d *PostgreSQLDriver) buildDSN(info ConnectionInfo) string {
	var params []string

	// Basic connection parameters
	params = append(params, fmt.Sprintf("host=%s", info.Host))
	params = append(params, fmt.Sprintf("port=%d", info.Port))
	params = append(params, fmt.Sprintf("user=%s", info.Username))
	params = append(params, fmt.Sprintf("password=%s", info.Password))
	params = append(params, fmt.Sprintf("dbname=%s", info.Database))

	// SSL Mode configuration
	sslMode := "prefer" // default
	if info.SSLMode != "" {
		switch strings.ToLower(info.SSLMode) {
		case "disable":
			sslMode = "disable"
		case "allow":
			sslMode = "allow"
		case "prefer":
			sslMode = "prefer"
		case "require":
			sslMode = "require"
		case "verify-ca":
			sslMode = "verify-ca"
		case "verify-full":
			sslMode = "verify-full"
		default:
			sslMode = "prefer"
		}
	}
	params = append(params, fmt.Sprintf("sslmode=%s", sslMode))

	// Connection timeout
	if info.Timeout > 0 {
		timeoutSeconds := int(info.Timeout.Seconds())
		params = append(params, fmt.Sprintf("connect_timeout=%d", timeoutSeconds))
	}

	// Application name for easier identification in logs
	params = append(params, "application_name=gsqlhealth")

	// Enable binary format for better performance
	params = append(params, "binary_parameters=yes")

	return strings.Join(params, " ")
}

// processRows processes SQL query results and returns them as a map
func (d *PostgreSQLDriver) processRows(rows *sql.Rows) (map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	result := make(map[string]interface{})

	// If we have multiple rows, collect them in an array
	var allResults []map[string]interface{}

	for rows.Next() {
		// Create a slice of interface{} to hold the values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))

		// Create pointers to the values
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan the values
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert values to appropriate types
		rowResult := make(map[string]interface{})
		for i, col := range columns {
			rowResult[col] = d.convertValue(values[i], columnTypes[i])
		}

		allResults = append(allResults, rowResult)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	// If we only have one row, return it directly
	// Otherwise, return all results
	if len(allResults) == 1 {
		result = allResults[0]
	} else if len(allResults) > 1 {
		result["results"] = allResults
		result["row_count"] = len(allResults)
	} else {
		result["row_count"] = 0
	}

	return result, nil
}

// convertValue converts a database value to an appropriate Go type
func (d *PostgreSQLDriver) convertValue(value interface{}, colType *sql.ColumnType) interface{} {
	if value == nil {
		return nil
	}

	// Handle byte arrays (common for bytea fields)
	if byteVal, ok := value.([]byte); ok {
		// Try to convert to string if it's valid UTF-8
		str := string(byteVal)
		return str
	}

	// Handle time values
	if timeVal, ok := value.(time.Time); ok {
		return timeVal.Format(time.RFC3339)
	}

	// Handle PostgreSQL arrays (they come as strings from pq driver)
	if strVal, ok := value.(string); ok {
		// Check if it looks like a PostgreSQL array
		if strings.HasPrefix(strVal, "{") && strings.HasSuffix(strVal, "}") {
			// Parse simple arrays (this is a basic implementation)
			content := strings.Trim(strVal, "{}")
			if content != "" {
				elements := strings.Split(content, ",")
				for i, elem := range elements {
					elements[i] = strings.TrimSpace(elem)
				}
				return elements
			}
			return []string{}
		}
	}

	return value
}