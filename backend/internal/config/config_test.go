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

// TestLoad_DataDirAndInstanceKey verifies the data directory and instance key settings.
//
// Scenarios:
//   - the data directory defaults to /data and can be overridden
//   - a 64-character hexadecimal instance key is decoded
//   - a key that is not hexadecimal or has the wrong length is rejected
func TestLoad_DataDirAndInstanceKey(t *testing.T) {
	base := map[string]string{"SENTINEL_DATABASE_URL": "postgres://x"}

	cfg, err := config.Load(env(base))
	if err != nil || cfg.DataDir != "/data" || cfg.InstanceKey != nil {
		t.Fatalf("defaults = %+v, %v, want /data and no key", cfg, err)
	}

	base["SENTINEL_DATA_DIR"] = "/var/lib/sentinel"
	base["SENTINEL_INSTANCE_KEY"] = strings.Repeat("ab", 32)
	cfg, err = config.Load(env(base))
	if err != nil || cfg.DataDir != "/var/lib/sentinel" || len(cfg.InstanceKey) != 32 || cfg.InstanceKey[0] != 0xab {
		t.Fatalf("overrides = %+v, %v, want the directory and a 32-byte key", cfg, err)
	}

	for _, bad := range []string{"zz" + strings.Repeat("ab", 31), "abcd"} {
		base["SENTINEL_INSTANCE_KEY"] = bad
		if _, err := config.Load(env(base)); err == nil || !strings.Contains(err.Error(), "SENTINEL_INSTANCE_KEY") {
			t.Errorf("Load() with key %q = %v, want an error naming SENTINEL_INSTANCE_KEY", bad, err)
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

// TestLoad_WebSettings verifies the public URL, origins, session policy and proxy settings.
//
// Scenarios:
//   - defaults: localhost base URL, 30 minute idle and 8 hour maximum sessions, 10 minute janitor, no proxies
//   - an https base URL marks cookies as secure and a trailing slash is dropped
//   - extra allowed origins and trusted proxies (addresses and networks) are accepted and blanks ignored
//   - invalid base URLs, origins, proxies, durations and an idle time above the maximum are all reported
func TestLoad_WebSettings(t *testing.T) {
	base := map[string]string{"SENTINEL_DATABASE_URL": "postgres://x"}

	cfg, err := config.Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://localhost:8080" || cfg.CookieSecure() || cfg.SessionIdle != 30*time.Minute ||
		cfg.SessionMax != 8*time.Hour || cfg.JanitorInterval != 10*time.Minute || len(cfg.TrustedProxies) != 0 || len(cfg.AllowedNetworks) != 0 ||
		len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "http://localhost:8080" {
		t.Errorf("defaults = %+v", cfg)
	}

	base["SENTINEL_BASE_URL"] = "https://files.example.com/"
	base["SENTINEL_ALLOWED_ORIGINS"] = " http://localhost:5173 , ,https://other.example.com"
	base["SENTINEL_TRUSTED_PROXIES"] = "10.0.0.0/8, 192.168.1.5, ::1"
	base["SENTINEL_ALLOWED_NETWORKS"] = "192.168.1.7/24, 10.0.0.9"
	base["SENTINEL_SESSION_IDLE"] = "10m"
	base["SENTINEL_SESSION_MAX"] = "2h"
	base["SENTINEL_JANITOR_INTERVAL"] = "1m"
	cfg, err = config.Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://files.example.com" || !cfg.CookieSecure() || len(cfg.AllowedOrigins) != 3 ||
		cfg.AllowedOrigins[1] != "http://localhost:5173" || len(cfg.TrustedProxies) != 3 ||
		len(cfg.AllowedNetworks) != 2 || cfg.AllowedNetworks[0].String() != "192.168.1.0/24" || cfg.AllowedNetworks[1].String() != "10.0.0.9/32" ||
		cfg.SessionIdle != 10*time.Minute || cfg.SessionMax != 2*time.Hour || cfg.JanitorInterval != time.Minute {
		t.Errorf("overrides = %+v", cfg)
	}

	for name, vars := range map[string]map[string]string{
		"base url with a path":     {"SENTINEL_BASE_URL": "https://x.example.com/app"},
		"base url without scheme":  {"SENTINEL_BASE_URL": "x.example.com"},
		"base url with userinfo":   {"SENTINEL_BASE_URL": "https://u:p@x.example.com"},
		"base url not parsable":    {"SENTINEL_BASE_URL": "http://[::1"},
		"origin with a query":      {"SENTINEL_ALLOWED_ORIGINS": "https://x.example.com?a=1"},
		"proxy not an address":     {"SENTINEL_TRUSTED_PROXIES": "not-an-ip"},
		"network not an address":   {"SENTINEL_ALLOWED_NETWORKS": "lan"},
		"idle above maximum":       {"SENTINEL_SESSION_IDLE": "10h"},
		"session idle not a time":  {"SENTINEL_SESSION_IDLE": "soon"},
		"janitor interval not set": {"SENTINEL_JANITOR_INTERVAL": "0s"},
	} {
		vars["SENTINEL_DATABASE_URL"] = "postgres://x"
		if _, err := config.Load(env(vars)); err == nil {
			t.Errorf("%s: Load() = nil error, want error", name)
		}
	}
}
