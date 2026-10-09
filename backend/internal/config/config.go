// Package config loads the Transferly configuration from environment variables.
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
	defaultHTTPAddr          = ":8080"
	defaultLogLevel          = "info"
	defaultShutdownTimeout   = 15 * time.Second
	defaultQuickBurst        = 5
	defaultDataDir           = "/data"
	defaultBaseURL           = "http://localhost:8080"
	defaultSessionIdle       = 30 * time.Minute
	defaultSessionMax        = 8 * time.Hour
	defaultJanitor           = 10 * time.Minute
	defaultActivityRetention = 30 * 24 * time.Hour
	instanceKeyLength        = 32
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
	// VaultRememberSessions keeps the vault of each session unlocked across restarts (ADR-0028); development turns it on, since
	// its server restarts with every change. Off by default.
	VaultRememberSessions bool
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
	// Quotas are the caps on bandwidth and on what accounts may have or move; an administrator can change them while the server runs.
	Quotas Quotas
	// ActivityRetention is how long the activity log keeps its entries.
	ActivityRetention time.Duration
	// QuickConnectBurst is how many quick connections one client may open in a burst; afterwards one more every 12 seconds.
	QuickConnectBurst int
	// Google is the OAuth client the installation registered with Google, for the Google Drive connector (ADR-0024).
	Google Google
}

// Google is the OAuth client of the installation and the endpoints of Google; the endpoints only change for tests.
type Google struct {
	// ClientID and ClientSecret identify the OAuth client; both empty disable signing in with Google.
	ClientID, ClientSecret string
	// AuthURL, TokenURL and APIURL are the consent page, the token endpoint and the origin of the APIs of Google.
	AuthURL, TokenURL, APIURL string
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

// Quotas are the starting values of the caps on bandwidth and quotas; zero means no cap.
type Quotas struct {
	// BandwidthGlobal caps the bytes per second all transfers move together.
	BandwidthGlobal int
	// BandwidthPerUser caps the bytes per second the transfers of one user move together.
	BandwidthPerUser int
	// MaxSitesPerWorkspace caps the sites of a workspace.
	MaxSitesPerWorkspace int
	// MaxTeamsPerUser caps the teams a user owns.
	MaxTeamsPerUser int
	// MaxMembersPerTeam caps the members of a team, with the invitations that wait.
	MaxMembersPerTeam int
	// DailyBytesPerUser caps the bytes a user moves in a day.
	DailyBytesPerUser int
}

// CookieSecure reports whether cookies must be marked Secure, which is the case for an https base URL.
func (c Config) CookieSecure() bool {
	return strings.HasPrefix(c.BaseURL, "https://")
}

// Load builds a [Config] from getenv, typically [os.Getenv].
// TRANSFERLY_DATABASE_URL is required; every other variable has a default.
// All validation errors are returned joined.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:          valueOr(getenv("TRANSFERLY_HTTP_ADDR"), defaultHTTPAddr),
		DatabaseURL:       getenv("TRANSFERLY_DATABASE_URL"),
		LogLevel:          strings.ToLower(valueOr(getenv("TRANSFERLY_LOG_LEVEL"), defaultLogLevel)),
		ShutdownTimeout:   defaultShutdownTimeout,
		AutoMigrate:       true,
		DataDir:           valueOr(getenv("TRANSFERLY_DATA_DIR"), defaultDataDir),
		BaseURL:           valueOr(getenv("TRANSFERLY_BASE_URL"), defaultBaseURL),
		SessionIdle:       defaultSessionIdle,
		SessionMax:        defaultSessionMax,
		JanitorInterval:   defaultJanitor,
		ActivityRetention: defaultActivityRetention,
		TransferLimits:    TransferLimits{Global: 32, PerWorkspace: 16, PerUser: 8, PerSite: 4},
		QuickConnectBurst: defaultQuickBurst,
		Google: Google{
			ClientID:     getenv("TRANSFERLY_GOOGLE_CLIENT_ID"),
			ClientSecret: getenv("TRANSFERLY_GOOGLE_CLIENT_SECRET"),
			AuthURL:      valueOr(getenv("TRANSFERLY_GOOGLE_AUTH_URL"), "https://accounts.google.com/o/oauth2/auth"),
			TokenURL:     valueOr(getenv("TRANSFERLY_GOOGLE_TOKEN_URL"), "https://oauth2.googleapis.com/token"),
			APIURL:       strings.TrimSuffix(valueOr(getenv("TRANSFERLY_GOOGLE_API_URL"), "https://www.googleapis.com"), "/"),
		},
	}

	var errs []error

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("TRANSFERLY_DATABASE_URL is required"))
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("invalid TRANSFERLY_LOG_LEVEL %q (want debug|info|warn|error)", cfg.LogLevel))
	}

	for _, d := range []struct {
		name string
		into *time.Duration
	}{
		{"TRANSFERLY_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
		{"TRANSFERLY_SESSION_IDLE", &cfg.SessionIdle},
		{"TRANSFERLY_SESSION_MAX", &cfg.SessionMax},
		{"TRANSFERLY_JANITOR_INTERVAL", &cfg.JanitorInterval},
		{"TRANSFERLY_ACTIVITY_RETENTION", &cfg.ActivityRetention},
	} {
		if err := parseDuration(getenv(d.name), d.name, d.into); err != nil {
			errs = append(errs, err)
		}
	}
	for _, l := range []struct {
		name string
		into *int
	}{
		{"TRANSFERLY_TRANSFER_LIMIT_GLOBAL", &cfg.TransferLimits.Global},
		{"TRANSFERLY_TRANSFER_LIMIT_PER_WORKSPACE", &cfg.TransferLimits.PerWorkspace},
		{"TRANSFERLY_TRANSFER_LIMIT_PER_USER", &cfg.TransferLimits.PerUser},
		{"TRANSFERLY_TRANSFER_LIMIT_PER_SITE", &cfg.TransferLimits.PerSite},
		{"TRANSFERLY_BANDWIDTH_GLOBAL", &cfg.Quotas.BandwidthGlobal},
		{"TRANSFERLY_BANDWIDTH_PER_USER", &cfg.Quotas.BandwidthPerUser},
		{"TRANSFERLY_MAX_SITES_PER_WORKSPACE", &cfg.Quotas.MaxSitesPerWorkspace},
		{"TRANSFERLY_MAX_TEAMS_PER_USER", &cfg.Quotas.MaxTeamsPerUser},
		{"TRANSFERLY_MAX_MEMBERS_PER_TEAM", &cfg.Quotas.MaxMembersPerTeam},
		{"TRANSFERLY_DAILY_BYTES_PER_USER", &cfg.Quotas.DailyBytesPerUser},
	} {
		if err := parseLimit(getenv(l.name), l.name, l.into); err != nil {
			errs = append(errs, err)
		}
	}
	if err := parseLimit(getenv("TRANSFERLY_QUICK_CONNECT_BURST"), "TRANSFERLY_QUICK_CONNECT_BURST", &cfg.QuickConnectBurst); err != nil {
		errs = append(errs, err)
	} else if cfg.QuickConnectBurst < 1 {
		errs = append(errs, errors.New("TRANSFERLY_QUICK_CONNECT_BURST must be at least 1"))
	}
	if (cfg.Google.ClientID == "") != (cfg.Google.ClientSecret == "") {
		errs = append(errs, errors.New("TRANSFERLY_GOOGLE_CLIENT_ID and TRANSFERLY_GOOGLE_CLIENT_SECRET must be set together"))
	}
	for name, raw := range map[string]string{
		"TRANSFERLY_GOOGLE_AUTH_URL": cfg.Google.AuthURL, "TRANSFERLY_GOOGLE_TOKEN_URL": cfg.Google.TokenURL, "TRANSFERLY_GOOGLE_API_URL": cfg.Google.APIURL,
	} {
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, fmt.Errorf("invalid %s %q (want an http or https URL)", name, raw))
		}
	}
	if cfg.SessionIdle > cfg.SessionMax {
		errs = append(errs, errors.New("TRANSFERLY_SESSION_IDLE must not exceed TRANSFERLY_SESSION_MAX"))
	}

	origin, err := parseOrigin(cfg.BaseURL)
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid TRANSFERLY_BASE_URL %q: %w", cfg.BaseURL, err))
	}
	cfg.BaseURL = origin
	if domain := getenv("TRANSFERLY_DOMAIN"); domain != "" && !strings.EqualFold(origin, "https://"+domain) {
		errs = append(errs, errors.New("TRANSFERLY_BASE_URL must match https://TRANSFERLY_DOMAIN when a domain is configured"))
	}
	cfg.AllowedOrigins = []string{origin}
	for _, extra := range splitList(getenv("TRANSFERLY_ALLOWED_ORIGINS")) {
		o, err := parseOrigin(extra)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid TRANSFERLY_ALLOWED_ORIGINS entry %q: %w", extra, err))
			continue
		}
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
	}
	for _, proxy := range splitList(getenv("TRANSFERLY_TRUSTED_PROXIES")) {
		if _, err := netip.ParsePrefix(proxy); err != nil {
			addr, addrErr := netip.ParseAddr(proxy)
			if addrErr != nil {
				errs = append(errs, fmt.Errorf("invalid TRANSFERLY_TRUSTED_PROXIES entry %q (want an IP address or CIDR)", proxy))
				continue
			}
			proxy = netip.PrefixFrom(addr, addr.BitLen()).String()
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, proxy)
	}

	for _, network := range splitList(getenv("TRANSFERLY_ALLOWED_NETWORKS")) {
		prefix, err := netip.ParsePrefix(network)
		if err != nil {
			addr, addrErr := netip.ParseAddr(network)
			if addrErr != nil {
				errs = append(errs, fmt.Errorf("invalid TRANSFERLY_ALLOWED_NETWORKS entry %q (want an IP address or CIDR)", network))
				continue
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		cfg.AllowedNetworks = append(cfg.AllowedNetworks, prefix.Masked())
	}

	for _, b := range []struct {
		name string
		into *bool
	}{
		{"TRANSFERLY_AUTO_MIGRATE", &cfg.AutoMigrate},
		{"TRANSFERLY_VAULT_REMEMBER_SESSIONS", &cfg.VaultRememberSessions},
	} {
		v := getenv(b.name)
		switch strings.ToLower(v) {
		case "":
		case "true", "1":
			*b.into = true
		case "false", "0":
			*b.into = false
		default:
			errs = append(errs, fmt.Errorf("invalid %s %q (want true|false)", b.name, v))
		}
	}

	if v := getenv("TRANSFERLY_INSTANCE_KEY"); v != "" {
		key, err := hex.DecodeString(v)
		if err != nil || len(key) != instanceKeyLength {
			errs = append(errs, errors.New("invalid TRANSFERLY_INSTANCE_KEY (want 64 hexadecimal characters)"))
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
