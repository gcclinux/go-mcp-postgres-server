package config_test

import (
	"os"
	"strings"
	"testing"

	"go-mcp-postgres-server/config"
)

// TestConfigLoadFromEnvVars sets all config env vars and verifies Load() returns them.
// Validates: Requirements 11.1, 11.2, 11.3
func TestConfigLoadFromEnvVars(t *testing.T) {
	// Save and restore all env vars
	envVars := []string{"MCP_DB_HOST", "MCP_DB_PORT", "MCP_DB_USER", "MCP_DB_PASSWORD", "MCP_DB_NAME", "MCP_LISTEN_ADDR"}
	saved := make(map[string]string)
	for _, key := range envVars {
		if v, ok := os.LookupEnv(key); ok {
			saved[key] = v
		} else {
			saved[key] = "\x00"
		}
	}
	t.Cleanup(func() {
		for key, val := range saved {
			if val == "\x00" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, val)
			}
		}
	})

	// Set all env vars to specific values
	os.Setenv("MCP_DB_HOST", "10.0.0.1")
	os.Setenv("MCP_DB_PORT", "9999")
	os.Setenv("MCP_DB_USER", "testuser")
	os.Setenv("MCP_DB_PASSWORD", "s3cret")
	os.Setenv("MCP_DB_NAME", "mydb")
	os.Setenv("MCP_LISTEN_ADDR", "127.0.0.1:8080")

	cfg := config.Load()

	if cfg.DBHost != "10.0.0.1" {
		t.Errorf("DBHost: got %q, want %q", cfg.DBHost, "10.0.0.1")
	}
	if cfg.DBPort != 9999 {
		t.Errorf("DBPort: got %d, want %d", cfg.DBPort, 9999)
	}
	if cfg.DBUser != "testuser" {
		t.Errorf("DBUser: got %q, want %q", cfg.DBUser, "testuser")
	}
	if cfg.DBPassword != "s3cret" {
		t.Errorf("DBPassword: got %q, want %q", cfg.DBPassword, "s3cret")
	}
	if cfg.DBName != "mydb" {
		t.Errorf("DBName: got %q, want %q", cfg.DBName, "mydb")
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("ListenAddr: got %q, want %q", cfg.ListenAddr, "127.0.0.1:8080")
	}
}

// TestConfigDSN verifies the DSN format produced by Config.DSN().
// Validates: Requirements 11.1
func TestConfigDSN(t *testing.T) {
	cfg := config.Config{
		DBHost:     "myhost",
		DBPort:     5432,
		DBUser:     "admin",
		DBPassword: "pass123",
		DBName:     "testdb",
		ListenAddr: "0.0.0.0:5353",
	}

	dsn := cfg.DSN()
	expected := "postgres://admin:pass123@myhost:5432/testdb"
	if dsn != expected {
		t.Errorf("DSN(): got %q, want %q", dsn, expected)
	}
}

// TestConfigLogSafe verifies the password is not present in LogSafe output.
// Validates: Requirements 11.4, 12.3, 12.4
func TestConfigLogSafe(t *testing.T) {
	cfg := config.Config{
		DBHost:     "dbhost.local",
		DBPort:     5432,
		DBUser:     "operator",
		DBPassword: "SuperSecret123!",
		DBName:     "prod_db",
		ListenAddr: "0.0.0.0:5353",
	}

	logSafe := cfg.LogSafe()

	// Password must NOT appear in the output
	if strings.Contains(logSafe, "SuperSecret123!") {
		t.Error("LogSafe() output contains the password")
	}

	// Key config values should appear
	if !strings.Contains(logSafe, "dbhost.local") {
		t.Error("LogSafe() output missing DBHost")
	}
	if !strings.Contains(logSafe, "5432") {
		t.Error("LogSafe() output missing DBPort")
	}
	if !strings.Contains(logSafe, "operator") {
		t.Error("LogSafe() output missing DBUser")
	}
	if !strings.Contains(logSafe, "prod_db") {
		t.Error("LogSafe() output missing DBName")
	}
	if !strings.Contains(logSafe, "0.0.0.0:5353") {
		t.Error("LogSafe() output missing ListenAddr")
	}
}
