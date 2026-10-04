// Package app composes the core with the adapters of each execution mode.
package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/fprambs/transferly/internal/adapters/instancekey"
	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/config"
	"github.com/fprambs/transferly/internal/transport/httpapi"
	"github.com/fprambs/transferly/internal/version"
)

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

// Serve runs the web server on ln until ctx is canceled, then shuts down gracefully.
//
// Global read and write timeouts are not set because transfers are long-lived streams.
// In-flight requests keep their own context during shutdown so they can finish.
func Serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.Listener) error {
	if cfg.AutoMigrate {
		logger.InfoContext(ctx, "applying migrations")
		if err := postgres.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
	}

	key, source, err := instancekey.Resolve(cfg.DataDir, cfg.InstanceKey, rand.Reader)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "instance key ready", "source", string(source))

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	wired, err := newWiring(cfg, logger, store, key)
	if err != nil {
		return fmt.Errorf("composing the services: %w", err)
	}
	if interrupted, err := wired.recover(ctx); err != nil {
		return fmt.Errorf("recovering transfers: %w", err)
	} else if interrupted > 0 {
		logger.WarnContext(ctx, "transfers interrupted by the previous shutdown", "count", interrupted)
	}
	if err := wired.deps.Admin.Load(ctx); err != nil {
		return fmt.Errorf("loading the allowed networks: %w", err)
	}
	if err := wired.deps.Quotas.Load(ctx); err != nil {
		return fmt.Errorf("loading the limits: %w", err)
	}
	janitorCtx, stopJanitor := context.WithCancel(ctx)
	defer stopJanitor()
	go runJanitor(janitorCtx, cfg.JanitorInterval, logger, wired.janitor)
	go wired.pool.Run(janitorCtx, poolSweepEvery)

	srv := &http.Server{
		Handler:           httpapi.NewRouter(wired.deps),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.InfoContext(ctx, "server listening", "addr", ln.Addr().String(), "version", version.Version)

	select {
	case err := <-errc:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	<-errc
	return nil
}
