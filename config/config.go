package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Config holds all server configuration values, loaded from environment variables.
type Config struct {
	DBHost     string // MCP_DB_HOST, default "192.168.0.65"
	DBPort     uint16 // MCP_DB_PORT, default 5432
	DBUser     string // MCP_DB_USER, default from $USER
	DBPassword string // MCP_DB_PASSWORD, default ""
	DBName     string // MCP_DB_NAME, default "mcp_db"
	ListenAddr string // MCP_LISTEN_ADDR, default "0.0.0.0:5353"
}

// Load reads configuration from environment variables, falling back to
// a .env file and then to documented defaults for any variable that is not set.
//
// Priority order (highest to lowest):
//  1. Environment variables already set in the process
//  2. Values from .env file (only for vars not already set)
//  3. Hard-coded defaults
func Load() Config {
	// Load .env file first — it only fills in vars that aren't already set.
	loadDotEnv(".env")

	c := Config{
		DBHost:     envOrDefault("MCP_DB_HOST", "192.168.0.65"),
		DBUser:     envOrDefault("MCP_DB_USER", os.Getenv("USER")),
		DBPassword: os.Getenv("MCP_DB_PASSWORD"),
		DBName:     envOrDefault("MCP_DB_NAME", "mcp_db"),
		ListenAddr: envOrDefault("MCP_LISTEN_ADDR", "0.0.0.0:5353"),
	}

	portStr := os.Getenv("MCP_DB_PORT")
	if portStr == "" {
		c.DBPort = 5432
	} else {
		p, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			c.DBPort = 5432
		} else {
			c.DBPort = uint16(p)
		}
	}

	return c
}

// DSN returns a PostgreSQL connection string in postgres:// URI format.
func (c Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

// LogSafe returns a human-readable config summary with the password redacted.
func (c Config) LogSafe() string {
	return fmt.Sprintf("host=%s port=%d user=%s dbname=%s listen=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBName, c.ListenAddr)
}

// envOrDefault returns the value of the named environment variable,
// or fallback if the variable is empty or unset.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads a .env file and sets environment variables for any keys
// that are not already present in the environment. This ensures that real
// environment variables (e.g. from Docker, systemd, shell exports) always
// take priority over the file.
//
// The parser supports:
//   - KEY=VALUE
//   - KEY="VALUE" and KEY='VALUE' (quotes stripped)
//   - Blank lines and lines starting with # (comments)
//   - Inline comments after unquoted values (KEY=value # comment)
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		// Missing .env is perfectly fine — not an error.
		return
	}
	defer f.Close()

	slog.Info("loading .env file", "path", path)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := parseDotEnvLine(line)
		if !ok {
			continue
		}

		// Only set if not already present in the environment.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

// parseDotEnvLine extracts a key-value pair from a single .env line.
func parseDotEnvLine(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, '=')
	if idx < 1 {
		return "", "", false
	}

	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])

	// Strip matching quotes.
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		} else {
			// Remove inline comments for unquoted values.
			if ci := strings.IndexByte(value, '#'); ci > 0 {
				value = strings.TrimSpace(value[:ci])
			}
		}
	}

	return key, value, true
}
