package config

import (
	"flag"
	"os"
)

// Config holds the server configuration.
type Config struct {
	Addr         string
	DataDir      string
	LogLevel     string
	AuthDisabled bool
}

// Load reads configuration from flags (with environment variables as fallback).
// Precedence: command-line flags > environment variables > hardcoded defaults.
func Load() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Addr, "addr", getEnv("HOME_STORE_ADDR", "0.0.0.0:9000"), "server address (host:port)")
	flag.StringVar(&cfg.DataDir, "data-dir", getEnv("HOME_STORE_DATA_DIR", ""), "data directory for objects")
	flag.StringVar(&cfg.LogLevel, "log-level", getEnv("HOME_STORE_LOG_LEVEL", "info"), "log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.AuthDisabled, "auth-disabled", false, "disable S3 signature verification")

	flag.Parse()

	return cfg, nil
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return NewConfigError("DataDir is required")
	}
	return nil
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
