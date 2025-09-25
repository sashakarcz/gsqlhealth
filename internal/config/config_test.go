package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	configContent := `
databases:
  - name: "test-mysql"
    type: "mysql"
    host: "localhost"
    port: 3306
    username: "testuser"
    password: "testpass"
    database: "testdb"
    tables:
      - name: "users"
        query: "SELECT COUNT(*) as count FROM users"
        timeout: 5
        check_interval: 30

server:
  host: "localhost"
  port: 8080
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 120

logging:
  level: "info"
  format: "json"
`

	tmpFile, err := os.CreateTemp("", "config_test_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Test loading config
	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Validate loaded config
	if len(config.Databases) != 1 {
		t.Errorf("Expected 1 database, got %d", len(config.Databases))
	}

	db := config.Databases[0]
	if db.Name != "test-mysql" {
		t.Errorf("Expected database name 'test-mysql', got '%s'", db.Name)
	}

	if db.Type != "mysql" {
		t.Errorf("Expected database type 'mysql', got '%s'", db.Type)
	}

	if config.Server.Port != 8080 {
		t.Errorf("Expected server port 8080, got %d", config.Server.Port)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name: "valid config",
			config: Config{
				Databases: []Database{
					{
						Name:     "test",
						Type:     "mysql",
						Host:     "localhost",
						Port:     3306,
						Username: "user",
						Password: "pass",
						Database: "db",
						Tables: []Table{
							{Name: "table1", Query: "SELECT 1", Timeout: 5, CheckInterval: 30},
						},
					},
				},
				Server: Server{
					Host:         "localhost",
					Port:         8080,
					ReadTimeout:  30,
					WriteTimeout: 30,
					IdleTimeout:  120,
				},
			},
			expectError: false,
		},
		{
			name: "no databases",
			config: Config{
				Databases: []Database{},
				Server: Server{
					Host:         "localhost",
					Port:         8080,
					ReadTimeout:  30,
					WriteTimeout: 30,
					IdleTimeout:  120,
				},
			},
			expectError: true,
		},
		{
			name: "invalid database type",
			config: Config{
				Databases: []Database{
					{
						Name:     "test",
						Type:     "invalid",
						Host:     "localhost",
						Port:     3306,
						Username: "user",
						Password: "pass",
						Database: "db",
						Tables: []Table{
							{Name: "table1", Query: "SELECT 1", Timeout: 5, CheckInterval: 30},
						},
					},
				},
				Server: Server{
					Host:         "localhost",
					Port:         8080,
					ReadTimeout:  30,
					WriteTimeout: 30,
					IdleTimeout:  120,
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("nonexistent.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Errorf("Expected 'failed to read config file' error, got: %v", err)
	}
}

func TestServerGetAddress(t *testing.T) {
	server := Server{
		Host: "localhost",
		Port: 8080,
	}

	expected := "localhost:8080"
	if addr := server.GetAddress(); addr != expected {
		t.Errorf("Expected address '%s', got '%s'", expected, addr)
	}
}