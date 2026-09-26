// Package config loads the Sentinel configuration from environment variables.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 15 * time.Second
	defaultDataDir         = "/data"
	defaultBaseURL         = "http://localhost:8080"
	defaultSessionIdle     = 30 * time.Minute
	defaultSessionMax      = 8 * time.Hour
	defaultJanitor         = 10 * time.Minute
	instanceKeyLength      = 32
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
	// DataDir is the directory holding instance state such as the instance key.
	DataDir string
	// InstanceKey is an optional 32-byte instance key; when nil one is stored in DataDir.
	InstanceKey []byte
	// BaseURL is the public origin of the application, such as https://files.example.com.
	BaseURL string
	// AllowedOrigins lists the origins allowed to make state-changing requests: the base URL and extra ones.
	AllowedOrigins []string
	// SessionIdle ends a session left unused for this long.
	SessionIdle time.Duration
	// SessionMax ends a session this long after it started.
	SessionMax time.Duration
	// AllowedNetworks lists the private networks sites may point to; public addresses are always allowed.
	AllowedNetworks []netip.Prefix
	// TrustedProxies lists the IP addresses or networks whose X-Forwarded-For header is believed.
	TrustedProxies []string
	// JanitorInterval is how often expired sessions and keys are purged.
	JanitorInterval time.Duration
	// TransferLimits caps how many transfers run at once; the rest wait in the queue.
	TransferLimits TransferLimits
}

// TransferLimits are the caps of concurrent transfers; zero means no cap.
type TransferLimits struct {
	// Global caps the transfers of the whole instance.
	Global int
	// PerWorkspace caps the transfers of one workspace.
	PerWorkspace int
	// PerUser caps the transfers of one user.
	PerUser int
	// PerSite caps the transfers that use one site or quick connection.
	PerSite int
}

// CookieSecure reports whether cookies must be marked Secure, which is the case for an https base URL.
func (c Config) CookieSecure() bool {
	return strings.HasPrefix(c.BaseURL, "https://")
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
		DataDir:         valueOr(getenv("SENTINEL_DATA_DIR"), defaultDataDir),
		BaseURL:         valueOr(getenv("SENTINEL_BASE_URL"), defaultBaseURL),
		SessionIdle:     defaultSessionIdle,
		SessionMax:      defaultSessionMax,
		JanitorInterval: defaultJanitor,
		TransferLimits:  TransferLimits{Global: 32, PerWorkspace: 16, PerUser: 8, PerSite: 4},
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

	for _, d := range []struct {
		name string
		into *time.Duration
	}{
		{"SENTINEL_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
		{"SENTINEL_SESSION_IDLE", &cfg.SessionIdle},
		{"SENTINEL_SESSION_MAX", &cfg.SessionMax},
		{"SENTINEL_JANITOR_INTERVAL", &cfg.JanitorInterval},
	} {
		if err := parseDuration(getenv(d.name), d.name, d.into); err != nil {
			errs = append(errs, err)
		}
	}
	for _, l := range []struct {
		name string
		into *int
	}{
		{"SENTINEL_TRANSFER_LIMIT_GLOBAL", &cfg.TransferLimits.Global},
		{"SENTINEL_TRANSFER_LIMIT_PER_WORKSPACE", &cfg.TransferLimits.PerWorkspace},
		{"SENTINEL_TRANSFER_LIMIT_PER_USER", &cfg.TransferLimits.PerUser},
		{"SENTINEL_TRANSFER_LIMIT_PER_SITE", &cfg.TransferLimits.PerSite},
	} {
		if err := parseLimit(getenv(l.name), l.name, l.into); err != nil {
			errs = append(errs, err)
		}
	}
	if cfg.SessionIdle > cfg.SessionMax {
		errs = append(errs, errors.New("SENTINEL_SESSION_IDLE must not exceed SENTINEL_SESSION_MAX"))
	}

	origin, err := parseOrigin(cfg.BaseURL)
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid SENTINEL_BASE_URL %q: %w", cfg.BaseURL, err))
	}
	cfg.BaseURL = origin
	cfg.AllowedOrigins = []string{origin}
	for _, extra := range splitList(getenv("SENTINEL_ALLOWED_ORIGINS")) {
		o, err := parseOrigin(extra)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid SENTINEL_ALLOWED_ORIGINS entry %q: %w", extra, err))
			continue
		}
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
	}
	for _, proxy := range splitList(getenv("SENTINEL_TRUSTED_PROXIES")) {
		if _, err := netip.ParsePrefix(proxy); err != nil {
			if _, addrErr := netip.ParseAddr(proxy); addrErr != nil {
				errs = append(errs, fmt.Errorf("invalid SENTINEL_TRUSTED_PROXIES entry %q (want an IP address or CIDR)", proxy))
				continue
			}
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, proxy)
	}

	for _, network := range splitList(getenv("SENTINEL_ALLOWED_NETWORKS")) {
		prefix, err := netip.ParsePrefix(network)
		if err != nil {
			addr, addrErr := netip.ParseAddr(network)
			if addrErr != nil {
				errs = append(errs, fmt.Errorf("invalid SENTINEL_ALLOWED_NETWORKS entry %q (want an IP address or CIDR)", network))
				continue
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		cfg.AllowedNetworks = append(cfg.AllowedNetworks, prefix.Masked())
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

	if v := getenv("SENTINEL_INSTANCE_KEY"); v != "" {
		key, err := hex.DecodeString(v)
		if err != nil || len(key) != instanceKeyLength {
			errs = append(errs, errors.New("invalid SENTINEL_INSTANCE_KEY (want 64 hexadecimal characters)"))
		} else {
			cfg.InstanceKey = key
		}
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parseDuration sets into from a positive duration in raw, leaving it unchanged when raw is empty.
func parseDuration(raw, name string, into *time.Duration) error {
	if raw == "" {
		return nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid %s %q (want a positive duration such as 15s)", name, raw)
	}
	*into = d
	return nil
}

// parseLimit sets into from a non-negative whole number in raw, leaving it unchanged when raw is empty.
func parseLimit(raw, name string, into *int) error {
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid %s %q (want a whole number, 0 for no limit)", name, raw)
	}
	*into = n
	return nil
}

// parseOrigin returns the scheme and host of a URL, rejecting anything with a path, query or credentials.
func parseOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("want an http or https origin such as https://files.example.com")
	}
	return u.Scheme + "://" + u.Host, nil
}

// splitList splits a comma separated list, trimming blanks and dropping empty items.
func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// valueOr returns v, or def when v is empty.
func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
