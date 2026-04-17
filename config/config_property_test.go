package config_test

import (
	"os"
	"strconv"
	"testing"

	"go-mcp-postgres-server/config"

	"pgregory.net/rapid"
)

// Feature: go-mcp-postgres-server, Property 13: Config Defaults for Unset Env Vars
// **Validates: Requirements 11.3**
func TestProperty_ConfigDefaultsForUnsetEnvVars(t *testing.T) {
	// All config env vars that Load() reads
	envVars := []string{"MCP_DB_HOST", "MCP_DB_PORT", "MCP_DB_USER", "MCP_DB_PASSWORD", "MCP_DB_NAME", "MCP_LISTEN_ADDR"}

	rapid.Check(t, func(t *rapid.T) {
		// Save current env
		saved := make(map[string]string)
		for _, key := range envVars {
			if v, ok := os.LookupEnv(key); ok {
				saved[key] = v
			} else {
				saved[key] = "\x00" // sentinel for "was unset"
			}
		}
		// Restore after test
		defer func() {
			for key, val := range saved {
				if val == "\x00" {
					os.Unsetenv(key)
				} else {
					os.Setenv(key, val)
				}
			}
		}()

		// For each env var, randomly decide to set or unset it
		setHost := rapid.Bool().Draw(t, "set_host")
		setPort := rapid.Bool().Draw(t, "set_port")
		setUser := rapid.Bool().Draw(t, "set_user")
		setPassword := rapid.Bool().Draw(t, "set_password")
		setDBName := rapid.Bool().Draw(t, "set_dbname")
		setListen := rapid.Bool().Draw(t, "set_listen")

		hostVal := rapid.StringMatching(`[a-z0-9\.]+`).Draw(t, "host_val")
		portVal := rapid.IntRange(1, 65535).Draw(t, "port_val")
		userVal := rapid.StringMatching(`[a-z][a-z0-9]+`).Draw(t, "user_val")
		passVal := rapid.StringMatching(`[a-zA-Z0-9!@#$%^&*()_+=\-]*`).Draw(t, "pass_val")
		dbNameVal := rapid.StringMatching(`[a-z][a-z0-9_]+`).Draw(t, "dbname_val")
		listenVal := rapid.StringMatching(`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+`).Draw(t, "listen_val")

		// Set or unset each env var
		if setHost {
			os.Setenv("MCP_DB_HOST", hostVal)
		} else {
			os.Unsetenv("MCP_DB_HOST")
		}
		if setPort {
			os.Setenv("MCP_DB_PORT", strconv.Itoa(portVal))
		} else {
			os.Unsetenv("MCP_DB_PORT")
		}
		if setUser {
			os.Setenv("MCP_DB_USER", userVal)
		} else {
			os.Unsetenv("MCP_DB_USER")
		}
		if setPassword {
			os.Setenv("MCP_DB_PASSWORD", passVal)
		} else {
			os.Unsetenv("MCP_DB_PASSWORD")
		}
		if setDBName {
			os.Setenv("MCP_DB_NAME", dbNameVal)
		} else {
			os.Unsetenv("MCP_DB_NAME")
		}
		if setListen {
			os.Setenv("MCP_LISTEN_ADDR", listenVal)
		} else {
			os.Unsetenv("MCP_LISTEN_ADDR")
		}

		cfg := config.Load()

		// Verify defaults for unset vars, and env values for set vars
		if !setHost && cfg.DBHost != "192.168.0.65" {
			t.Fatalf("expected default DBHost '192.168.0.65', got %q", cfg.DBHost)
		}
		if setHost && cfg.DBHost != hostVal {
			t.Fatalf("expected DBHost %q, got %q", hostVal, cfg.DBHost)
		}

		if !setPort && cfg.DBPort != 5432 {
			t.Fatalf("expected default DBPort 5432, got %d", cfg.DBPort)
		}
		if setPort && cfg.DBPort != uint16(portVal) {
			t.Fatalf("expected DBPort %d, got %d", portVal, cfg.DBPort)
		}

		if !setDBName && cfg.DBName != "mcp_db" {
			t.Fatalf("expected default DBName 'mcp_db', got %q", cfg.DBName)
		}
		if setDBName && cfg.DBName != dbNameVal {
			t.Fatalf("expected DBName %q, got %q", dbNameVal, cfg.DBName)
		}

		if !setListen && cfg.ListenAddr != "0.0.0.0:5353" {
			t.Fatalf("expected default ListenAddr '0.0.0.0:5353', got %q", cfg.ListenAddr)
		}
		if setListen && cfg.ListenAddr != listenVal {
			t.Fatalf("expected ListenAddr %q, got %q", listenVal, cfg.ListenAddr)
		}

		if !setPassword && cfg.DBPassword != "" {
			t.Fatalf("expected default DBPassword '', got %q", cfg.DBPassword)
		}
		if setPassword && cfg.DBPassword != passVal {
			t.Fatalf("expected DBPassword %q, got %q", passVal, cfg.DBPassword)
		}

		// DBUser defaults to $USER env var when MCP_DB_USER is unset
		if !setUser {
			expectedUser := os.Getenv("USER")
			if cfg.DBUser != expectedUser {
				t.Fatalf("expected default DBUser %q (from $USER), got %q", expectedUser, cfg.DBUser)
			}
		}
		if setUser && cfg.DBUser != userVal {
			t.Fatalf("expected DBUser %q, got %q", userVal, cfg.DBUser)
		}
	})
}
