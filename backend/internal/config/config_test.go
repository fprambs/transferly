package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/config"
)

// env returns a getenv function backed by m.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestLoad_Defaults verifies that Load applies every default
// when only the required database URL is set.
func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"SENTINEL_DATABASE_URL": "postgres://x",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
	if !cfg.AutoMigrate {
		t.Error("AutoMigrate = false, want true by default")
	}
}

// TestLoad_Overrides verifies that every environment variable overrides its default,
// including case-insensitive log levels.
func TestLoad_Overrides(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"SENTINEL_DATABASE_URL":     "postgres://x",
		"SENTINEL_HTTP_ADDR":        "127.0.0.1:9000",
		"SENTINEL_LOG_LEVEL":        "DEBUG",
		"SENTINEL_SHUTDOWN_TIMEOUT": "3s",
		"SENTINEL_AUTO_MIGRATE":     "false",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.LogLevel != "debug" ||
		cfg.ShutdownTimeout != 3*time.Second || cfg.AutoMigrate {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

// TestLoad_AutoMigrateFlag verifies that the auto-migrate flag accepts true, 1, false and 0.
func TestLoad_AutoMigrateFlag(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "1": true, "false": false, "0": false} {
		cfg, err := config.Load(env(map[string]string{
			"SENTINEL_DATABASE_URL": "postgres://x",
			"SENTINEL_AUTO_MIGRATE": value,
		}))
		if err != nil {
			t.Fatalf("Load() with %q error = %v", value, err)
		}
		if cfg.AutoMigrate != want {
			t.Errorf("AutoMigrate with %q = %v, want %v", value, cfg.AutoMigrate, want)
		}
	}
}

// TestLoad_Errors verifies that Load rejects invalid configuration and names each offending variable.
//
// Scenarios:
//   - missing database URL
//   - unknown log level
//   - unparsable shutdown timeout
//   - non-positive shutdown timeout
//   - invalid auto-migrate flag
//   - several problems reported together in one error
func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"missing database url", map[string]string{}, []string{"SENTINEL_DATABASE_URL"}},
		{"invalid log level", map[string]string{"SENTINEL_DATABASE_URL": "x", "SENTINEL_LOG_LEVEL": "loud"}, []string{"SENTINEL_LOG_LEVEL"}},
		{"unparsable shutdown timeout", map[string]string{"SENTINEL_DATABASE_URL": "x", "SENTINEL_SHUTDOWN_TIMEOUT": "abc"}, []string{"SENTINEL_SHUTDOWN_TIMEOUT"}},
		{"non-positive shutdown timeout", map[string]string{"SENTINEL_DATABASE_URL": "x", "SENTINEL_SHUTDOWN_TIMEOUT": "0s"}, []string{"SENTINEL_SHUTDOWN_TIMEOUT"}},
		{"invalid auto migrate", map[string]string{"SENTINEL_DATABASE_URL": "x", "SENTINEL_AUTO_MIGRATE": "maybe"}, []string{"SENTINEL_AUTO_MIGRATE"}},
		{"multiple errors joined", map[string]string{"SENTINEL_LOG_LEVEL": "loud"}, []string{"SENTINEL_DATABASE_URL", "SENTINEL_LOG_LEVEL"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(env(tt.env))
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %s", err, w)
				}
			}
		})
	}
}
