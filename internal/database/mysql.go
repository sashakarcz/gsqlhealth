package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLDriver implements the Driver interface for MySQL databases
type MySQLDriver struct {
	db *sql.DB
}

// NewMySQLDriver creates a new MySQL driver instance
func NewMySQLDriver() *MySQLDriver {
	return &MySQLDriver{}
}

// Connect establishes a connection to the MySQL database
func (d *MySQLDriver) Connect(ctx context.Context, info ConnectionInfo) error {
	dsn := d.buildDSN(info)

	var err error
	d.db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open MySQL connection: %w", err)
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
		return fmt.Errorf("failed to ping MySQL database: %w", err)
	}

	return nil
}

// Close closes the MySQL database connection
func (d *MySQLDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// ExecuteHealthCheck executes a health check query and returns the results
func (d *MySQLDriver) ExecuteHealthCheck(ctx context.Context, query string) (map[string]interface{}, error) {
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
func (d *MySQLDriver) Ping(ctx context.Context) error {
	if d.db == nil {
		return fmt.Errorf("database connection is not established")
	}
	return d.db.PingContext(ctx)
}

// GetDriverName returns the name of the database driver
func (d *MySQLDriver) GetDriverName() string {
	return "mysql"
}

// buildDSN constructs the MySQL data source name
func (d *MySQLDriver) buildDSN(info ConnectionInfo) string {
	var params []string

	// Always set charset to utf8mb4 for better Unicode support
	params = append(params, "charset=utf8mb4")

	// Set parse time to true for better time handling
	params = append(params, "parseTime=true")

	// Set location to UTC
	params = append(params, "loc=UTC")

	// Set connection timeout
	if info.Timeout > 0 {
		timeoutSeconds := int(info.Timeout.Seconds())
		params = append(params, fmt.Sprintf("timeout=%ds", timeoutSeconds))
		params = append(params, fmt.Sprintf("readTimeout=%ds", timeoutSeconds))
		params = append(params, fmt.Sprintf("writeTimeout=%ds", timeoutSeconds))
	}

	// Enable multi-statements for compatibility
	params = append(params, "multiStatements=true")

	// Handle SSL/TLS configuration
	if info.SSLMode != "" {
		switch strings.ToLower(info.SSLMode) {
		case "disable", "false":
			params = append(params, "tls=false")
		case "require", "true":
			params = append(params, "tls=skip-verify")
		case "verify-ca":
			params = append(params, "tls=preferred")
		case "verify-full":
			params = append(params, "tls=true")
		default:
			params = append(params, "tls=preferred")
		}
	}

	paramStr := strings.Join(params, "&")

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		info.Username,
		info.Password,
		info.Host,
		info.Port,
		info.Database,
		paramStr)
}

// processRows processes SQL query results and returns them as a map
func (d *MySQLDriver) processRows(rows *sql.Rows) (map[string]interface{}, error) {
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
func (d *MySQLDriver) convertValue(value interface{}, colType *sql.ColumnType) interface{} {
	if value == nil {
		return nil
	}

	// Handle byte arrays (common for TEXT fields)
	if byteVal, ok := value.([]byte); ok {
		// Try to convert to string if it's valid UTF-8
		str := string(byteVal)
		return str
	}

	// Handle time values
	if timeVal, ok := value.(time.Time); ok {
		return timeVal.Format(time.RFC3339)
	}

	return value
}