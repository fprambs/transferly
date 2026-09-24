// Package app composes the core with the adapters of each execution mode.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
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

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &http.Server{
		Handler:           httpapi.NewRouter(httpapi.Deps{Logger: logger, Readiness: store}),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.InfoContext(ctx, "server listening", "addr", ln.Addr().String())

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
