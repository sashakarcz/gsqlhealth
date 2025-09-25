# GSQLHealth - Database Health Monitoring Service

GSQLHealth is a comprehensive Go-based service for monitoring the health of multiple databases (MySQL, PostgreSQL, and Microsoft SQL Server) through configurable SQL queries. It provides RESTful HTTP endpoints to check database connectivity and execute custom health check queries.

## Features

- **Multi-Database Support**: MySQL, PostgreSQL, and Microsoft SQL Server
- **Resilient Connections**: Non-blocking startup with automatic connection recovery
- **Periodic Health Checks**: Configurable intervals for automatic health monitoring
- **Result Caching**: Fast responses using cached health check results
- **RESTful API**: Clean HTTP endpoints for health status with real-time and cached modes
- **Configurable Health Checks**: Custom SQL queries per table/database
- **Connection Pooling**: Optimized database connections with automatic cleanup
- **Retry Logic**: Exponential backoff with configurable retry parameters
- **Background Recovery**: Automatic reconnection to failed databases
- **Concurrent Processing**: Parallel execution of health checks
- **Structured Logging**: JSON and text logging with configurable levels
- **Graceful Shutdown**: Proper cleanup of resources
- **Docker Support**: Ready-to-use Docker container
- **Configuration Validation**: Built-in config validation
- **Comprehensive Error Handling**: Detailed error messages and recovery

## Installation

### From Source

```bash
git clone https://github.com/sashakarcz/gsqlhealth.git
cd gsqlhealth
make build
```

### Using Go Install

```bash
go install ./cmd/gsqlhealth
```

### Using Docker

```bash
docker build -t gsqlhealth .
docker run -p 8080:8080 -v $(pwd)/config.yaml:/app/config.yaml gsqlhealth
```

## Configuration

Create a `config.yaml` file with your database configurations:

```yaml
databases:
  - name: "primary-mysql"
    type: "mysql"
    host: "localhost"
    port: 3306
    username: "health_user"
    password: "health_pass"
    database: "production"
    tables:
      - name: "users"
        query: "SELECT COUNT(*) as count FROM users WHERE created_at > DATE_SUB(NOW(), INTERVAL 1 HOUR)"
        timeout: 5
        check_interval: 30
      - name: "orders"
        query: "SELECT COUNT(*) as active_orders FROM orders WHERE status = 'active'"
        timeout: 10
        check_interval: 60

  - name: "analytics-postgres"
    type: "postgres"
    host: "analytics.example.com"
    port: 5432
    username: "readonly_user"
    password: "secure_password"
    database: "analytics"
    ssl_mode: "require"
    tables:
      - name: "events"
        query: "SELECT COUNT(*) as event_count FROM events WHERE created_at > NOW() - INTERVAL '1 hour'"
        timeout: 15
        check_interval: 120

  - name: "reporting-mssql"
    type: "mssql"
    host: "reports.internal.com"
    port: 1433
    username: "report_user"
    password: "report_password"
    database: "ReportingDB"
    tables:
      - name: "daily_reports"
        query: "SELECT COUNT(*) as pending_reports FROM daily_reports WHERE status = 'pending'"
        timeout: 20

server:
  host: "localhost"
  port: 8080
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 120

logging:
  level: "info"
  format: "json"

retry:
  max_attempts: 0          # Maximum connection attempts during startup (0 = infinite)
  initial_delay: 5         # Initial retry delay in seconds
  max_delay: 60            # Maximum retry delay in seconds
  backoff_factor: 2        # Exponential backoff multiplier
  connection_retry: 30     # Background connection recovery interval in seconds
```

### Configuration Options

#### Database Configuration

- `name`: Unique identifier for the database
- `type`: Database type (`mysql`, `postgres`, `mssql`)
- `host`: Database host
- `port`: Database port
- `username`: Database username
- `password`: Database password
- `database`: Database name
- `ssl_mode`: SSL mode (optional, varies by database type)
- `tables`: Array of table health check configurations

#### Table Configuration

- `name`: Unique identifier for the table/check
- `query`: SQL query to execute for health check
- `timeout`: Query timeout in seconds
- `check_interval`: How often to run the health check in seconds

#### Server Configuration

- `host`: HTTP server host
- `port`: HTTP server port
- `read_timeout`: HTTP read timeout in seconds
- `write_timeout`: HTTP write timeout in seconds
- `idle_timeout`: HTTP idle timeout in seconds

#### Logging Configuration

- `level`: Log level (`debug`, `info`, `warn`, `error`)
- `format`: Log format (`json`, `text`)

#### Retry Configuration

- `max_attempts`: Maximum connection attempts during startup (0 = infinite retries, recommended)
- `initial_delay`: Initial retry delay in seconds
- `max_delay`: Maximum retry delay in seconds
- `backoff_factor`: Exponential backoff multiplier
- `connection_retry`: Background connection recovery interval in seconds

## Usage

### Basic Usage

```bash
# Run with default config file (config.yaml)
./gsqlhealth

# Run with custom config file
./gsqlhealth -config /path/to/config.yaml

# Validate configuration without running
./gsqlhealth -config config.yaml -validate

# Show version
./gsqlhealth -version
```

### Development

```bash
# Install dependencies
make deps

# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Build for development
make build

# Run with live reload (requires air)
make dev
```

## Resilient Database Connections

GSQLHealth is designed to be resilient to database connectivity issues:

### Startup Behavior
- **Non-blocking startup**: Service starts even if some databases are unavailable
- **Retry logic**: Attempts to connect to databases with exponential backoff
- **Graceful degradation**: Continues operating with available databases

### Connection Recovery
- **Background recovery**: Automatically attempts to reconnect to failed databases
- **Health monitoring**: Continuously monitors connection status
- **Automatic failover**: Switches to recovered connections seamlessly

### Configuration
The retry behavior is fully configurable:
```yaml
retry:
  max_attempts: 0          # Startup connection attempts (0 = infinite)
  initial_delay: 5         # Start with 5-second delays
  max_delay: 60            # Cap delays at 1 minute
  backoff_factor: 2        # Double the delay each attempt
  connection_retry: 30     # Check for recovery every 30 seconds
```

**Infinite Retries**: By default, the service will retry connections indefinitely (`max_attempts: 0`). This ensures:
- Service stays running during database outages
- Continuous monitoring and outage data collection
- Automatic recovery when databases come back online
- Historical data about connection failures and recovery times

## Periodic Health Checks

GSQLHealth automatically runs health checks at configurable intervals for each table. This provides several benefits:

- **Fast Response Times**: API endpoints return cached results instantly
- **Continuous Monitoring**: Health status is always up-to-date
- **Reduced Database Load**: Scheduled checks prevent excessive query load
- **Background Processing**: Health checks run independently of API requests

### How It Works

1. **Startup**: The service connects to all configured databases and starts periodic health check schedulers
2. **Scheduled Execution**: Each table's health check runs at its configured `check_interval`
3. **Result Caching**: Results are cached with timestamps for instant retrieval
4. **API Response**: Endpoints return cached results by default, with option for real-time checks

### Query Parameters

Add `?realtime=true` to any health endpoint to force real-time database queries instead of using cached results:

```bash
# Use cached results (fast)
curl http://localhost:8080/health/primary-mysql/users

# Force real-time check (slower, current data)
curl http://localhost:8080/health/primary-mysql/users?realtime=true
```

### Choosing Check Intervals

Consider these factors when setting `check_interval` values:

- **Critical Systems**: 30-60 seconds for high-priority databases
- **Reporting Systems**: 300-600 seconds for less critical systems
- **Heavy Queries**: Longer intervals for expensive queries to reduce database load
- **Light Queries**: Shorter intervals for simple COUNT queries
- **Database Load**: Balance monitoring frequency with database performance

**Example intervals:**
```yaml
tables:
  - name: "user_sessions"     # Critical user data
    check_interval: 30
  - name: "daily_reports"     # Less critical reporting
    check_interval: 300
  - name: "audit_logs"        # Heavy query, less frequent
    check_interval: 600
```

## API Endpoints

### Health Check Endpoints

#### GET `/health`
Returns overall health status of all configured databases and tables.

**HTTP Status Codes:**
- `200 OK` - All health checks completed successfully (all healthy or non-connection errors)
- `503 Service Unavailable` - One or more database connection failures detected
- `504 Gateway Timeout` - One or more query timeouts detected

**Response:**
```json
{
  "status": "healthy",
  "total_checks": 6,
  "healthy_checks": 6,
  "timestamp": "2023-10-01T12:00:00Z",
  "databases": {
    "primary-mysql": [
      {
        "database_name": "primary-mysql",
        "table_name": "users",
        "status": "healthy",
        "data": {"count": 1234},
        "query_time": "25ms",
        "timestamp": "2023-10-01T12:00:00Z"
      }
    ]
  }
}
```

#### GET `/health/{database}`
Returns health status for all tables in a specific database.

**HTTP Status Codes:**
- `200 OK` - Health check completed successfully (all checks healthy or non-connection errors)
- `503 Service Unavailable` - Database connection failed or communication error detected
- `404 Not Found` - Database not found in configuration
- `504 Gateway Timeout` - Query timeout exceeded

#### GET `/health/{database}/{table}`
Returns health status for a specific table in a database.

**HTTP Status Codes:**
- `200 OK` - Health check completed successfully (healthy or non-connection errors)
- `400 Bad Request` - Query execution error (bad SQL syntax, etc.)
- `503 Service Unavailable` - Database connection failed or communication error detected
- `404 Not Found` - Database or table not found in configuration
- `504 Gateway Timeout` - Query timeout exceeded

### Information Endpoints

#### GET `/databases`
Lists all configured databases.

#### GET `/databases/{database}/tables`
Lists all configured tables for a specific database.

#### GET `/ping/{database}`
Tests connectivity to a specific database without running queries.

#### GET `/cache/stats`
Returns statistics about cached health check results.

**Response:**
```json
{
  "cache_stats": {
    "total_checks": 6,
    "fresh_results": 4,
    "healthy_results": 5,
    "unhealthy_results": 1
  },
  "timestamp": "2023-10-01T12:00:00Z"
}
```

#### GET `/`
Returns service information and available endpoints.

## Database-Specific Considerations

### MySQL

- Supports SSL/TLS connections
- Connection pooling optimized for MySQL workloads
- Handles MySQL-specific data types correctly
- Uses `utf8mb4` charset for better Unicode support

### PostgreSQL

- Full SSL mode support (`disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`)
- Binary parameters enabled for better performance
- Proper handling of PostgreSQL arrays and custom types
- Application name set for easier identification in logs

### Microsoft SQL Server

- TLS encryption support with certificate validation options
- Proper handling of UNIQUEIDENTIFIER, BIT, DECIMAL types
- Connection string optimization for SQL Server
- Supports both SQL Server Authentication and Windows Authentication

## Monitoring and Observability

### Logging

The service provides structured logging with the following log levels:

- `DEBUG`: Detailed execution information
- `INFO`: General operational information
- `WARN`: Warning conditions
- `ERROR`: Error conditions

### Metrics

Health check results include:

- Query execution time
- Success/failure status
- Row counts (if applicable)
- Error messages
- Timestamps

### Health Check Strategy

The service uses a fail-fast approach:

- Individual table failures don't affect other checks
- Database connection failures are isolated per database
- Concurrent execution prevents blocking
- Configurable timeouts prevent hanging queries

## Security Considerations

- Passwords should be stored securely (consider using environment variables)
- Use read-only database users when possible
- Enable SSL/TLS for database connections
- Run the service with minimal privileges
- Regularly rotate database credentials

## Performance Optimization

- Connection pooling reduces connection overhead
- Concurrent health checks improve response times
- Configurable timeouts prevent resource exhaustion
- Efficient database drivers with proper connection reuse
- Memory-efficient result processing

## Error Handling

The service provides detailed error responses with appropriate HTTP status codes:

### HTTP Status Codes

- **200 OK**: Health check completed successfully (healthy or non-connection/timeout errors)
- **400 Bad Request**: Query execution error (invalid SQL syntax, permission denied, etc.)
- **404 Not Found**: Database or table not found in configuration
- **500 Internal Server Error**: Unexpected server error
- **503 Service Unavailable**: Database connection failed or communication issues
- **504 Gateway Timeout**: Query execution timeout exceeded

### Error Response Format

```json
{
  "error": "Cannot connect to database 'primary-mysql' for table 'users'",
  "details": "dial tcp 127.0.0.1:3306: connection refused",
  "timestamp": "2023-10-01T12:00:00Z"
}
```

**Note**: Connection failures now return **HTTP 503 Service Unavailable** instead of HTTP 400, as this better represents that the database service is temporarily unavailable rather than the request being malformed.

## Edge Cases Handled

1. **Connection Failures**: Returns HTTP 503 with detailed error messages for database connectivity issues
2. **Query Timeouts**: Returns HTTP 504 when queries exceed configured timeouts
3. **Malformed Results**: Safe handling of unexpected query results
4. **Memory Management**: Efficient processing of large result sets
5. **Concurrent Access**: Thread-safe operations for multiple simultaneous requests
6. **Graceful Shutdown**: Proper cleanup of database connections and HTTP server
7. **Configuration Errors**: Returns HTTP 404 for missing databases/tables in config
8. **Database-Specific Types**: Proper conversion of database-specific data types
9. **Network Issues**: Comprehensive detection of connection-related errors
10. **Query Errors**: Distinction between connection failures and SQL execution errors

## Contributing

1. Fork the repository
2. Create a feature branch
3. Write tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

MIT License - see LICENSE file for details.
