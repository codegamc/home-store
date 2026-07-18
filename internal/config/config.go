package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

// Config holds the server configuration.
type Config struct {
	Addr              string
	DataDir           string
	LogLevel          string
	AccessKey         string
	SecretKey         string
	Region            string
	TLSCertFile       string
	TLSKeyFile        string
	InsecureHTTP      bool
	MaxObjectSize     int64
	MaxStorageSize    int64
	MultipartExpiry   time.Duration
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Load reads configuration from flags (with environment variables as fallback).
// Precedence: command-line flags > environment variables > hardcoded defaults.
func Load() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Addr, "addr", getEnv("HOME_STORE_ADDR", "0.0.0.0:9000"), "server address (host:port)")
	flag.StringVar(&cfg.DataDir, "data-dir", getEnv("HOME_STORE_DATA_DIR", ""), "data directory for objects")
	flag.StringVar(&cfg.LogLevel, "log-level", getEnv("HOME_STORE_LOG_LEVEL", "info"), "log level (debug, info, warn, error)")
	flag.StringVar(&cfg.AccessKey, "access-key", getEnv("HOME_STORE_ACCESS_KEY", ""), "S3 access key ID (required)")
	flag.StringVar(&cfg.SecretKey, "secret-key", getEnv("HOME_STORE_SECRET_KEY", ""), "S3 secret access key (required)")
	flag.StringVar(&cfg.Region, "region", getEnv("HOME_STORE_REGION", "us-east-1"), "S3 signing region")
	flag.StringVar(&cfg.TLSCertFile, "tls-cert", getEnv("HOME_STORE_TLS_CERT", ""), "PEM TLS certificate file")
	flag.StringVar(&cfg.TLSKeyFile, "tls-key", getEnv("HOME_STORE_TLS_KEY", ""), "PEM TLS private key file")
	flag.BoolVar(&cfg.InsecureHTTP, "insecure-http", getEnv("HOME_STORE_INSECURE_HTTP", "") == "true", "allow plaintext HTTP (development only)")
	flag.Int64Var(&cfg.MaxObjectSize, "max-object-size", getEnvInt64("HOME_STORE_MAX_OBJECT_SIZE", 0), "maximum object size in bytes (0 is unlimited)")
	flag.Int64Var(&cfg.MaxStorageSize, "max-storage-size", getEnvInt64("HOME_STORE_MAX_STORAGE_SIZE", 0), "maximum total stored data in bytes (0 is unlimited)")
	flag.DurationVar(&cfg.MultipartExpiry, "multipart-expiry", getEnvDuration("HOME_STORE_MULTIPART_EXPIRY", 7*24*time.Hour), "remove incomplete multipart uploads older than this duration (0 disables expiry)")
	flag.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", getEnvDuration("HOME_STORE_READ_HEADER_TIMEOUT", 10*time.Second), "maximum time to read request headers")
	flag.DurationVar(&cfg.ReadTimeout, "read-timeout", getEnvDuration("HOME_STORE_READ_TIMEOUT", 0), "maximum time to read a request body (0 is unlimited for large uploads)")
	flag.DurationVar(&cfg.WriteTimeout, "write-timeout", getEnvDuration("HOME_STORE_WRITE_TIMEOUT", 0), "maximum time to write a response (0 is unlimited for large downloads)")
	flag.DurationVar(&cfg.IdleTimeout, "idle-timeout", getEnvDuration("HOME_STORE_IDLE_TIMEOUT", 60*time.Second), "maximum idle keep-alive time")

	flag.Parse()

	return cfg, nil
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return NewConfigError("DataDir is required")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return NewConfigError("access key and secret key are required")
	}
	if c.Region == "" {
		return NewConfigError("region is required")
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return NewConfigError("tls-cert and tls-key must be provided together")
	}
	if c.TLSCertFile == "" && !c.InsecureHTTP {
		return NewConfigError("TLS is required; configure tls-cert and tls-key, or use insecure-http only for local development")
	}
	if c.MaxObjectSize < 0 || c.MaxStorageSize < 0 || c.MultipartExpiry < 0 {
		return NewConfigError("storage limits cannot be negative")
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

func getEnvInt64(key string, defaultValue int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return defaultValue
	}
	return parsed
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
