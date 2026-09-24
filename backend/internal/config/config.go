// Package config loads the Sentinel configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 15 * time.Second
)

// Config holds the instance configuration.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server.
	HTTPAddr string
	// DatabaseURL is the PostgreSQL connection URL.
	DatabaseURL string
	// LogLevel is one of debug, info, warn or error.
	LogLevel string
	// ShutdownTimeout bounds the graceful shutdown.
	ShutdownTimeout time.Duration
	// AutoMigrate applies pending migrations on startup.
	AutoMigrate bool
}

// Load builds a [Config] from getenv, typically [os.Getenv].
// SENTINEL_DATABASE_URL is required; every other variable has a default.
// All validation errors are returned joined.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:        valueOr(getenv("SENTINEL_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseURL:     getenv("SENTINEL_DATABASE_URL"),
		LogLevel:        strings.ToLower(valueOr(getenv("SENTINEL_LOG_LEVEL"), defaultLogLevel)),
		ShutdownTimeout: defaultShutdownTimeout,
		AutoMigrate:     true,
	}

	var errs []error

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("SENTINEL_DATABASE_URL is required"))
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("invalid SENTINEL_LOG_LEVEL %q (want debug|info|warn|error)", cfg.LogLevel))
	}

	if v := getenv("SENTINEL_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("invalid SENTINEL_SHUTDOWN_TIMEOUT %q (want a positive duration such as 15s)", v))
		} else {
			cfg.ShutdownTimeout = d
		}
	}

	if v := getenv("SENTINEL_AUTO_MIGRATE"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1":
			cfg.AutoMigrate = true
		case "false", "0":
			cfg.AutoMigrate = false
		default:
			errs = append(errs, fmt.Errorf("invalid SENTINEL_AUTO_MIGRATE %q (want true|false)", v))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// valueOr returns v, or def when v is empty.
func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
