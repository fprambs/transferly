package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/config"
)

const healthcheckTimeout = 3 * time.Second

// Run executes the sentinel command line.
//
// Commands are serve (the default), migrate [up|status] and healthcheck. Configuration comes
// from getenv; logs and command output are written to stdout.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	switch command {
	case "serve":
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
		if err != nil {
			return fmt.Errorf("listening on %s: %w", cfg.HTTPAddr, err)
		}
		return Serve(ctx, cfg, newLogger(stdout, cfg.LogLevel), ln)
	case "migrate":
		return runMigrate(ctx, cfg, args[1:], stdout)
	case "healthcheck":
		return healthcheck(ctx, cfg.HTTPAddr)
	default:
		return fmt.Errorf("unknown command %q (want serve | migrate | healthcheck)", command)
	}
}

// healthcheck succeeds when the server listening on addr answers its liveness probe.
// It exists for container health checks, since the production image has no shell or curl.
func healthcheck(ctx context.Context, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("building health request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health request answered %d", response.StatusCode)
	}
	return nil
}

// runMigrate applies pending migrations (up, the default) or prints the schema version (status).
func runMigrate(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	subcommand := "up"
	if len(args) > 0 {
		subcommand = args[0]
	}

	switch subcommand {
	case "up":
		return postgres.Migrate(ctx, cfg.DatabaseURL)
	case "status":
		version, err := postgres.MigrationStatus(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "schema version:", version)
		return err
	default:
		return fmt.Errorf("unknown migrate subcommand %q (want up | status)", subcommand)
	}
}

// newLogger returns a JSON logger writing to w at the given level.
func newLogger(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l}))
}
