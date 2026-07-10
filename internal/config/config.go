package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the server configuration.
type Config struct {
	Addr          string
	DataDir       string
	DBPath        string
	LogLevel      string
	Location      string
	AuthDisabled  bool
	AccessKey     string
	SecretKey     string
	SecretKeyFile string
}

// Load reads configuration from flags (with environment variables as fallback).
// Precedence: command-line flags > environment variables > hardcoded defaults.
func Load() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Addr, "addr", getEnv("HOME_STORE_ADDR", "0.0.0.0:9000"), "server address (host:port)")
	flag.StringVar(&cfg.DataDir, "data-dir", getEnv("HOME_STORE_DATA_DIR", ""), "data directory for objects")
	flag.StringVar(&cfg.DBPath, "db-path", getEnv("HOME_STORE_DB_PATH", ""), "SQLite metadata database path (defaults to a sibling of data-dir)")
	flag.StringVar(&cfg.LogLevel, "log-level", getEnv("HOME_STORE_LOG_LEVEL", "info"), "log level (debug, info, warn, error)")
	flag.StringVar(&cfg.Location, "location", getEnv("HOME_STORE_LOCATION", "us-east-1"), "bucket location/region")
	flag.BoolVar(&cfg.AuthDisabled, "auth-disabled", getEnvBool("HOME_STORE_AUTH_DISABLED", false), "disable S3 signature verification")
	flag.StringVar(&cfg.AccessKey, "access-key", getEnv("HOME_STORE_ACCESS_KEY", ""), "S3 access key")
	cfg.SecretKey = getEnv("HOME_STORE_SECRET_KEY", "")
	cfg.SecretKeyFile = getEnv("HOME_STORE_SECRET_KEY_FILE", "")

	flag.Parse()

	if cfg.DBPath == "" && cfg.DataDir != "" {
		cfg.DBPath = defaultDBPath(cfg.DataDir)
	}
	if cfg.SecretKey == "" && cfg.SecretKeyFile != "" {
		value, err := os.ReadFile(cfg.SecretKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read HOME_STORE_SECRET_KEY_FILE: %w", err)
		}
		cfg.SecretKey = strings.TrimSpace(string(value))
	}

	return cfg, nil
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return NewConfigError("DataDir is required")
	}
	if c.DBPath == "" {
		return NewConfigError("DBPath is required")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return NewConfigError("Addr must be a valid host:port")
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return NewConfigError("LogLevel must be debug, info, warn, or error")
	}
	if !c.AuthDisabled && (c.AccessKey == "" || c.SecretKey == "") {
		return NewConfigError("HOME_STORE_ACCESS_KEY and HOME_STORE_SECRET_KEY (or HOME_STORE_SECRET_KEY_FILE) are required unless authentication is disabled")
	}
	return nil
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func defaultDBPath(dataDir string) string {
	cleanDataDir := filepath.Clean(dataDir)
	parentDir := filepath.Dir(cleanDataDir)
	baseName := filepath.Base(cleanDataDir)
	if baseName == "." || baseName == string(filepath.Separator) {
		baseName = "home-store-data"
	}
	return filepath.Join(parentDir, baseName+".sqlite")
}

// getEnv returns the value of the environment variable key if it exists,
// otherwise returns the default value.
func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// ConfigError is returned when configuration validation fails.
type ConfigError struct {
	msg string
}

func NewConfigError(msg string) *ConfigError {
	return &ConfigError{msg: msg}
}

func (e *ConfigError) Error() string {
	return "config error: " + e.msg
}
