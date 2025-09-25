package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// MSSQLDriver implements the Driver interface for Microsoft SQL Server databases
type MSSQLDriver struct {
	db *sql.DB
}

// NewMSSQLDriver creates a new MS SQL Server driver instance
func NewMSSQLDriver() *MSSQLDriver {
	return &MSSQLDriver{}
}

// Connect establishes a connection to the MS SQL Server database
func (d *MSSQLDriver) Connect(ctx context.Context, info ConnectionInfo) error {
	dsn := d.buildDSN(info)

	var err error
	d.db, err = sql.Open("sqlserver", dsn)
	if err != nil {
		return fmt.Errorf("failed to open MS SQL Server connection: %w", err)
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
		return fmt.Errorf("failed to ping MS SQL Server database: %w", err)
	}

	return nil
}

// Close closes the MS SQL Server database connection
func (d *MSSQLDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// ExecuteHealthCheck executes a health check query and returns the results
func (d *MSSQLDriver) ExecuteHealthCheck(ctx context.Context, query string) (map[string]interface{}, error) {
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
func (d *MSSQLDriver) Ping(ctx context.Context) error {
	if d.db == nil {
		return fmt.Errorf("database connection is not established")
	}
	return d.db.PingContext(ctx)
}

// GetDriverName returns the name of the database driver
func (d *MSSQLDriver) GetDriverName() string {
	return "mssql"
}

// buildDSN constructs the MS SQL Server data source name
func (d *MSSQLDriver) buildDSN(info ConnectionInfo) string {
	query := url.Values{}

	// Set database name
	query.Add("database", info.Database)

	// Set connection timeout
	if info.Timeout > 0 {
		timeoutSeconds := int(info.Timeout.Seconds())
		query.Add("connection timeout", strconv.Itoa(timeoutSeconds))
		query.Add("dial timeout", strconv.Itoa(timeoutSeconds))
	}

	// Set application name for easier identification
	query.Add("app name", "gsqlhealth")

	// Enable encryption based on SSL mode
	if info.SSLMode != "" {
		switch strings.ToLower(info.SSLMode) {
		case "disable", "false":
			query.Add("encrypt", "disable")
		case "require", "true":
			query.Add("encrypt", "true")
			query.Add("trustservercertificate", "true")
		case "verify-ca":
			query.Add("encrypt", "true")
			query.Add("trustservercertificate", "false")
		case "verify-full":
			query.Add("encrypt", "true")
			query.Add("trustservercertificate", "false")
			query.Add("hostnameincertificate", info.Host)
		default:
			query.Add("encrypt", "true")
			query.Add("trustservercertificate", "true")
		}
	} else {
		// Default to encrypted connection with trusted certificate
		query.Add("encrypt", "true")
		query.Add("trustservercertificate", "true")
	}

	// Build the connection URL
	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(info.Username, info.Password),
		Host:     fmt.Sprintf("%s:%d", info.Host, info.Port),
		RawQuery: query.Encode(),
	}

	return u.String()
}

// processRows processes SQL query results and returns them as a map
func (d *MSSQLDriver) processRows(rows *sql.Rows) (map[string]interface{}, error) {
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
func (d *MSSQLDriver) convertValue(value interface{}, colType *sql.ColumnType) interface{} {
	if value == nil {
		return nil
	}

	// Handle byte arrays (common for VARBINARY, IMAGE, etc.)
	if byteVal, ok := value.([]byte); ok {
		// Try to convert to string if it's valid UTF-8
		str := string(byteVal)
		return str
	}

	// Handle time values
	if timeVal, ok := value.(time.Time); ok {
		return timeVal.Format(time.RFC3339)
	}

	// Handle MS SQL Server specific types
	switch colType.DatabaseTypeName() {
	case "UNIQUEIDENTIFIER":
		// Convert GUID to string
		if strVal, ok := value.(string); ok {
			return strings.ToUpper(strVal)
		}
	case "BIT":
		// Convert bit to boolean
		if intVal, ok := value.(int64); ok {
			return intVal != 0
		}
		if boolVal, ok := value.(bool); ok {
			return boolVal
		}
	case "DECIMAL", "NUMERIC", "MONEY", "SMALLMONEY":
		// These might come as strings from the driver
		if strVal, ok := value.(string); ok {
			return strVal
		}
	}

	return value
}