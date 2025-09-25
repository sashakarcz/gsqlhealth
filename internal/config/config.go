package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	Databases []Database `yaml:"databases"`
	Server    Server     `yaml:"server"`
	Logging   Logging    `yaml:"logging"`
	Retry     Retry      `yaml:"retry"`
}

// Database represents a database connection configuration
type Database struct {
	Name     string  `yaml:"name"`
	Type     string  `yaml:"type"`
	Host     string  `yaml:"host"`
	Port     int     `yaml:"port"`
	Username string  `yaml:"username"`
	Password string  `yaml:"password"`
	Database string  `yaml:"database"`
	SSLMode  string  `yaml:"ssl_mode,omitempty"`
	Tables   []Table `yaml:"tables"`
}

// Table represents a table health check configuration
type Table struct {
	Name          string `yaml:"name"`
	Query         string `yaml:"query"`
	Timeout       int    `yaml:"timeout"`        // timeout in seconds
	CheckInterval int    `yaml:"check_interval"` // check interval in seconds
}

// Server represents HTTP server configuration
type Server struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
	IdleTimeout  int    `yaml:"idle_timeout"`
}

// Logging represents logging configuration
type Logging struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Retry represents connection retry configuration
type Retry struct {
	MaxAttempts     int `yaml:"max_attempts"`      // Maximum number of retry attempts (0 = infinite)
	InitialDelay    int `yaml:"initial_delay"`     // Initial retry delay in seconds
	MaxDelay        int `yaml:"max_delay"`         // Maximum retry delay in seconds
	BackoffFactor   int `yaml:"backoff_factor"`    // Exponential backoff multiplier
	ConnectionRetry int `yaml:"connection_retry"`  // Retry interval for connection recovery in seconds
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults for retry configuration
	config.Retry.SetDefaults()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// Validate performs validation on the configuration
func (c *Config) Validate() error {
	if len(c.Databases) == 0 {
		return fmt.Errorf("at least one database must be configured")
	}

	for i, db := range c.Databases {
		if err := db.Validate(); err != nil {
			return fmt.Errorf("database %d (%s): %w", i, db.Name, err)
		}
	}

	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("server configuration: %w", err)
	}

	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("retry configuration: %w", err)
	}

	return nil
}

// Validate validates database configuration
func (d *Database) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("database name is required")
	}

	if d.Type != "mysql" && d.Type != "postgres" && d.Type != "mssql" {
		return fmt.Errorf("unsupported database type: %s", d.Type)
	}

	if d.Host == "" {
		return fmt.Errorf("database host is required")
	}

	if d.Port <= 0 || d.Port > 65535 {
		return fmt.Errorf("invalid port number: %d", d.Port)
	}

	if d.Username == "" {
		return fmt.Errorf("database username is required")
	}

	if d.Database == "" {
		return fmt.Errorf("database name is required")
	}

	if len(d.Tables) == 0 {
		return fmt.Errorf("at least one table must be configured")
	}

	for i, table := range d.Tables {
		if err := table.Validate(); err != nil {
			return fmt.Errorf("table %d (%s): %w", i, table.Name, err)
		}
	}

	return nil
}

// Validate validates table configuration
func (t *Table) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("table name is required")
	}

	if t.Query == "" {
		return fmt.Errorf("table query is required")
	}

	if t.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	if t.CheckInterval <= 0 {
		return fmt.Errorf("check_interval must be positive")
	}

	return nil
}

// Validate validates server configuration
func (s *Server) Validate() error {
	if s.Host == "" {
		return fmt.Errorf("server host is required")
	}

	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", s.Port)
	}

	if s.ReadTimeout <= 0 {
		return fmt.Errorf("read timeout must be positive")
	}

	if s.WriteTimeout <= 0 {
		return fmt.Errorf("write timeout must be positive")
	}

	if s.IdleTimeout <= 0 {
		return fmt.Errorf("idle timeout must be positive")
	}

	return nil
}

// GetAddress returns the server address in host:port format
func (s *Server) GetAddress() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// GetReadTimeout returns read timeout as time.Duration
func (s *Server) GetReadTimeout() time.Duration {
	return time.Duration(s.ReadTimeout) * time.Second
}

// GetWriteTimeout returns write timeout as time.Duration
func (s *Server) GetWriteTimeout() time.Duration {
	return time.Duration(s.WriteTimeout) * time.Second
}

// GetIdleTimeout returns idle timeout as time.Duration
func (s *Server) GetIdleTimeout() time.Duration {
	return time.Duration(s.IdleTimeout) * time.Second
}

// GetQueryTimeout returns query timeout as time.Duration
func (t *Table) GetQueryTimeout() time.Duration {
	return time.Duration(t.Timeout) * time.Second
}

// GetCheckInterval returns check interval as time.Duration
func (t *Table) GetCheckInterval() time.Duration {
	return time.Duration(t.CheckInterval) * time.Second
}

// Validate validates retry configuration
func (r *Retry) Validate() error {
	if r.MaxAttempts < 0 {
		return fmt.Errorf("max_attempts cannot be negative")
	}

	if r.InitialDelay <= 0 {
		return fmt.Errorf("initial_delay must be positive")
	}

	if r.MaxDelay <= 0 {
		return fmt.Errorf("max_delay must be positive")
	}

	if r.InitialDelay > r.MaxDelay {
		return fmt.Errorf("initial_delay cannot be greater than max_delay")
	}

	if r.BackoffFactor <= 0 {
		return fmt.Errorf("backoff_factor must be positive")
	}

	if r.ConnectionRetry <= 0 {
		return fmt.Errorf("connection_retry must be positive")
	}

	return nil
}

// GetInitialDelay returns initial delay as time.Duration
func (r *Retry) GetInitialDelay() time.Duration {
	return time.Duration(r.InitialDelay) * time.Second
}

// GetMaxDelay returns max delay as time.Duration
func (r *Retry) GetMaxDelay() time.Duration {
	return time.Duration(r.MaxDelay) * time.Second
}

// GetConnectionRetry returns connection retry interval as time.Duration
func (r *Retry) GetConnectionRetry() time.Duration {
	return time.Duration(r.ConnectionRetry) * time.Second
}

// SetDefaults sets default retry values if not specified
func (r *Retry) SetDefaults() {
	if r.MaxAttempts == 0 && r.InitialDelay == 0 {
		r.MaxAttempts = 0 // infinite retries
		r.InitialDelay = 5
		r.MaxDelay = 60
		r.BackoffFactor = 2
		r.ConnectionRetry = 30
	}
}