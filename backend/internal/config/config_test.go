package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/config"
)

// env returns a getenv function backed by m.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestLoad_Defaults verifies that Load applies every default
// when only the required database URL is set.
func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"TRANSFERLY_DATABASE_URL": "postgres://x",
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
	if cfg.VaultRememberSessions {
		t.Error("VaultRememberSessions = true, want off by default outside development")
	}
}

// TestLoad_Overrides verifies that every environment variable overrides its default,
// including case-insensitive log levels.
func TestLoad_Overrides(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"TRANSFERLY_DATABASE_URL":     "postgres://x",
		"TRANSFERLY_HTTP_ADDR":        "127.0.0.1:9000",
		"TRANSFERLY_LOG_LEVEL":        "DEBUG",
		"TRANSFERLY_SHUTDOWN_TIMEOUT": "3s",
		"TRANSFERLY_AUTO_MIGRATE":     "false",
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
			"TRANSFERLY_DATABASE_URL": "postgres://x",
			"TRANSFERLY_AUTO_MIGRATE": value,
		}))
		if err != nil {
			t.Fatalf("Load() with %q error = %v", value, err)
		}
		if cfg.AutoMigrate != want {
			t.Errorf("AutoMigrate with %q = %v, want %v", value, cfg.AutoMigrate, want)
		}
	}
}

// TestLoad_VaultRememberSessions verifies that the vault of sessions is remembered across restarts only when asked (ADR-0028).
func TestLoad_VaultRememberSessions(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "1": true, "false": false, "": false} {
		cfg, err := config.Load(env(map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x", "TRANSFERLY_VAULT_REMEMBER_SESSIONS": value}))
		if err != nil || cfg.VaultRememberSessions != want {
			t.Errorf("VaultRememberSessions with %q = %v, %v; want %v", value, cfg.VaultRememberSessions, err, want)
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
	base := map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x"}

	cfg, err := config.Load(env(base))
	if err != nil || cfg.DataDir != "/data" || cfg.InstanceKey != nil {
		t.Fatalf("defaults = %+v, %v, want /data and no key", cfg, err)
	}

	base["TRANSFERLY_DATA_DIR"] = "/var/lib/transferly"
	base["TRANSFERLY_INSTANCE_KEY"] = strings.Repeat("ab", 32)
	cfg, err = config.Load(env(base))
	if err != nil || cfg.DataDir != "/var/lib/transferly" || len(cfg.InstanceKey) != 32 || cfg.InstanceKey[0] != 0xab {
		t.Fatalf("overrides = %+v, %v, want the directory and a 32-byte key", cfg, err)
	}

	for _, bad := range []string{"zz" + strings.Repeat("ab", 31), "abcd"} {
		base["TRANSFERLY_INSTANCE_KEY"] = bad
		if _, err := config.Load(env(base)); err == nil || !strings.Contains(err.Error(), "TRANSFERLY_INSTANCE_KEY") {
			t.Errorf("Load() with key %q = %v, want an error naming TRANSFERLY_INSTANCE_KEY", bad, err)
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
		{"missing database url", map[string]string{}, []string{"TRANSFERLY_DATABASE_URL"}},
		{"invalid log level", map[string]string{"TRANSFERLY_DATABASE_URL": "x", "TRANSFERLY_LOG_LEVEL": "loud"}, []string{"TRANSFERLY_LOG_LEVEL"}},
		{"unparsable shutdown timeout", map[string]string{"TRANSFERLY_DATABASE_URL": "x", "TRANSFERLY_SHUTDOWN_TIMEOUT": "abc"}, []string{"TRANSFERLY_SHUTDOWN_TIMEOUT"}},
		{"non-positive shutdown timeout", map[string]string{"TRANSFERLY_DATABASE_URL": "x", "TRANSFERLY_SHUTDOWN_TIMEOUT": "0s"}, []string{"TRANSFERLY_SHUTDOWN_TIMEOUT"}},
		{"invalid auto migrate", map[string]string{"TRANSFERLY_DATABASE_URL": "x", "TRANSFERLY_AUTO_MIGRATE": "maybe"}, []string{"TRANSFERLY_AUTO_MIGRATE"}},
		{"invalid vault sessions", map[string]string{"TRANSFERLY_DATABASE_URL": "x", "TRANSFERLY_VAULT_REMEMBER_SESSIONS": "yes"}, []string{"TRANSFERLY_VAULT_REMEMBER_SESSIONS"}},
		{"multiple errors joined", map[string]string{"TRANSFERLY_LOG_LEVEL": "loud"}, []string{"TRANSFERLY_DATABASE_URL", "TRANSFERLY_LOG_LEVEL"}},
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
	base := map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x"}

	cfg, err := config.Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://localhost:8080" || cfg.CookieSecure() || cfg.SessionIdle != 30*time.Minute ||
		cfg.SessionMax != 8*time.Hour || cfg.JanitorInterval != 10*time.Minute || cfg.ActivityRetention != 30*24*time.Hour || len(cfg.TrustedProxies) != 0 || len(cfg.AllowedNetworks) != 0 ||
		len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "http://localhost:8080" {
		t.Errorf("defaults = %+v", cfg)
	}

	base["TRANSFERLY_BASE_URL"] = "https://files.example.com/"
	base["TRANSFERLY_ALLOWED_ORIGINS"] = " http://localhost:5173 , ,https://other.example.com"
	base["TRANSFERLY_TRUSTED_PROXIES"] = "10.0.0.0/8, 192.168.1.5, ::1"
	base["TRANSFERLY_ALLOWED_NETWORKS"] = "192.168.1.7/24, 10.0.0.9"
	base["TRANSFERLY_SESSION_IDLE"] = "10m"
	base["TRANSFERLY_SESSION_MAX"] = "2h"
	base["TRANSFERLY_JANITOR_INTERVAL"] = "1m"
	base["TRANSFERLY_ACTIVITY_RETENTION"] = "72h"
	cfg, err = config.Load(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://files.example.com" || !cfg.CookieSecure() || len(cfg.AllowedOrigins) != 3 ||
		cfg.AllowedOrigins[1] != "http://localhost:5173" || len(cfg.TrustedProxies) != 3 ||
		len(cfg.AllowedNetworks) != 2 || cfg.AllowedNetworks[0].String() != "192.168.1.0/24" || cfg.AllowedNetworks[1].String() != "10.0.0.9/32" ||
		cfg.SessionIdle != 10*time.Minute || cfg.SessionMax != 2*time.Hour || cfg.JanitorInterval != time.Minute || cfg.ActivityRetention != 72*time.Hour {
		t.Errorf("overrides = %+v", cfg)
	}

	for name, vars := range map[string]map[string]string{
		"base url with a path":     {"TRANSFERLY_BASE_URL": "https://x.example.com/app"},
		"base url without scheme":  {"TRANSFERLY_BASE_URL": "x.example.com"},
		"base url with userinfo":   {"TRANSFERLY_BASE_URL": "https://u:p@x.example.com"},
		"base url not parsable":    {"TRANSFERLY_BASE_URL": "http://[::1"},
		"origin with a query":      {"TRANSFERLY_ALLOWED_ORIGINS": "https://x.example.com?a=1"},
		"proxy not an address":     {"TRANSFERLY_TRUSTED_PROXIES": "not-an-ip"},
		"network not an address":   {"TRANSFERLY_ALLOWED_NETWORKS": "lan"},
		"idle above maximum":       {"TRANSFERLY_SESSION_IDLE": "10h"},
		"session idle not a time":  {"TRANSFERLY_SESSION_IDLE": "soon"},
		"janitor interval not set": {"TRANSFERLY_JANITOR_INTERVAL": "0s"},
		"retention not a time":     {"TRANSFERLY_ACTIVITY_RETENTION": "forever"},
	} {
		vars["TRANSFERLY_DATABASE_URL"] = "postgres://x"
		if _, err := config.Load(env(vars)); err == nil {
			t.Errorf("%s: Load() = nil error, want error", name)
		}
	}
}

// TestLoad_TransferLimits verifies the caps of concurrent transfers.
//
// Scenarios:
//   - defaults are 32 in total, 16 per workspace, 8 per user and 4 per site
//   - each cap can be set, and zero means no cap
//   - negative and non-numeric values are reported
//   - the burst of quick connections defaults to 5, can be raised, and must be a positive number
func TestLoad_TransferLimits(t *testing.T) {
	base := map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x"}
	cfg, err := config.Load(env(base))
	if err != nil || cfg.TransferLimits != (config.TransferLimits{Global: 32, PerWorkspace: 16, PerUser: 8, PerSite: 4}) {
		t.Fatalf("defaults = %+v, %v", cfg.TransferLimits, err)
	}
	base["TRANSFERLY_TRANSFER_LIMIT_GLOBAL"] = "0"
	base["TRANSFERLY_TRANSFER_LIMIT_PER_WORKSPACE"] = "5"
	base["TRANSFERLY_TRANSFER_LIMIT_PER_USER"] = "3"
	base["TRANSFERLY_TRANSFER_LIMIT_PER_SITE"] = "2"
	cfg, err = config.Load(env(base))
	if err != nil || cfg.TransferLimits != (config.TransferLimits{Global: 0, PerWorkspace: 5, PerUser: 3, PerSite: 2}) {
		t.Fatalf("overrides = %+v, %v", cfg.TransferLimits, err)
	}
	if cfg.QuickConnectBurst != 5 {
		t.Errorf("default burst = %d; want 5", cfg.QuickConnectBurst)
	}
	base["TRANSFERLY_QUICK_CONNECT_BURST"] = "30"
	if cfg, err = config.Load(env(base)); err != nil || cfg.QuickConnectBurst != 30 {
		t.Errorf("burst = %d, %v; want 30", cfg.QuickConnectBurst, err)
	}
	for _, value := range []string{"0", "-2", "lots"} {
		base["TRANSFERLY_QUICK_CONNECT_BURST"] = value
		if _, err := config.Load(env(base)); err == nil {
			t.Errorf("Load() with a quick connect burst of %q = nil error", value)
		}
	}
	delete(base, "TRANSFERLY_QUICK_CONNECT_BURST")
	for _, value := range []string{"-1", "many", "1.5"} {
		base["TRANSFERLY_TRANSFER_LIMIT_PER_SITE"] = value
		if _, err := config.Load(env(base)); err == nil {
			t.Errorf("Load() with a per-site limit of %q = nil error", value)
		}
	}
}

// TestLoad_Quotas verifies the starting caps on bandwidth and quotas.
//
// Scenarios:
//   - every cap defaults to zero, which means no cap
//   - each cap can be set from its variable
//   - negative and non-numeric values are reported by name
func TestLoad_Quotas(t *testing.T) {
	base := map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x"}
	cfg, err := config.Load(env(base))
	if err != nil || cfg.Quotas != (config.Quotas{}) {
		t.Fatalf("defaults = %+v, %v", cfg.Quotas, err)
	}
	names := map[string]func(config.Quotas) int{
		"TRANSFERLY_BANDWIDTH_GLOBAL":        func(q config.Quotas) int { return q.BandwidthGlobal },
		"TRANSFERLY_BANDWIDTH_PER_USER":      func(q config.Quotas) int { return q.BandwidthPerUser },
		"TRANSFERLY_MAX_SITES_PER_WORKSPACE": func(q config.Quotas) int { return q.MaxSitesPerWorkspace },
		"TRANSFERLY_MAX_TEAMS_PER_USER":      func(q config.Quotas) int { return q.MaxTeamsPerUser },
		"TRANSFERLY_MAX_MEMBERS_PER_TEAM":    func(q config.Quotas) int { return q.MaxMembersPerTeam },
		"TRANSFERLY_DAILY_BYTES_PER_USER":    func(q config.Quotas) int { return q.DailyBytesPerUser },
	}
	for name, read := range names {
		set := map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x", name: "1234"}
		if cfg, err := config.Load(env(set)); err != nil || read(cfg.Quotas) != 1234 {
			t.Errorf("%s = %d, %v; want 1234", name, read(cfg.Quotas), err)
		}
		for _, bad := range []string{"-1", "lots"} {
			set[name] = bad
			if _, err := config.Load(env(set)); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("Load() with %s=%q = %v; want an error naming the variable", name, bad, err)
			}
		}
	}
}

// TestLoad_Google verifies the OAuth client of Google (ADR-0024).
//
// Scenarios:
//   - by default no client is configured and the endpoints are Google's
//   - a client ID and secret, and endpoints of a fake, are read, the API without its trailing slash
//   - a client ID without its secret, or the reverse, is refused, and so are endpoints that are not http or https URLs
func TestLoad_Google(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{"TRANSFERLY_DATABASE_URL": "postgres://x"}))
	want := config.Google{AuthURL: "https://accounts.google.com/o/oauth2/auth", TokenURL: "https://oauth2.googleapis.com/token", APIURL: "https://www.googleapis.com"}
	if err != nil || cfg.Google != want {
		t.Fatalf("defaults = %+v, %v", cfg.Google, err)
	}
	set := map[string]string{
		"TRANSFERLY_DATABASE_URL": "postgres://x", "TRANSFERLY_GOOGLE_CLIENT_ID": "id", "TRANSFERLY_GOOGLE_CLIENT_SECRET": "secret",
		"TRANSFERLY_GOOGLE_AUTH_URL": "http://google:8090/o/oauth2/auth", "TRANSFERLY_GOOGLE_TOKEN_URL": "http://google:8090/token", "TRANSFERLY_GOOGLE_API_URL": "http://google:8090/",
	}
	cfg, err = config.Load(env(set))
	if err != nil || cfg.Google != (config.Google{ClientID: "id", ClientSecret: "secret", AuthURL: "http://google:8090/o/oauth2/auth", TokenURL: "http://google:8090/token", APIURL: "http://google:8090"}) {
		t.Errorf("configured = %+v, %v", cfg.Google, err)
	}
	for name, value := range map[string]string{"TRANSFERLY_GOOGLE_CLIENT_SECRET": "", "TRANSFERLY_GOOGLE_TOKEN_URL": "ftp://google/token", "TRANSFERLY_GOOGLE_API_URL": "not a url"} {
		bad := map[string]string{}
		for k, v := range set {
			bad[k] = v
		}
		bad[name] = value
		if _, err := config.Load(env(bad)); err == nil || !strings.Contains(err.Error(), "TRANSFERLY_GOOGLE_") {
			t.Errorf("Load() with %s=%q = %v; want an error naming the Google variables", name, value, err)
		}
	}
}
