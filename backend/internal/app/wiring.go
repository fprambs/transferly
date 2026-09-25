package app

import (
	"context"
	"crypto/rand"
	"log/slog"
	"runtime"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/clock"
	"github.com/fprambs/sentinel/internal/adapters/ratelimit"
	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
	"github.com/fprambs/sentinel/internal/transport/webui"
)

const maxTrackedClients = 10000

var (
	setupRule        = ratelimit.Rule{Burst: 5, Every: time.Minute}
	loginIPRule      = ratelimit.Rule{Burst: 10, Every: 6 * time.Second}
	loginAccountRule = ratelimit.Rule{Burst: 5, Every: 12 * time.Second}
	vaultRule        = ratelimit.Rule{Burst: 5, Every: 12 * time.Second}
)

// janitorTask is a periodic cleanup that reports how many items it removed.
type janitorTask struct {
	name string
	run  func(ctx context.Context) (int, error)
}

// wiring holds what Serve builds from the configuration and the store.
type wiring struct {
	deps    httpapi.Deps
	janitor []janitorTask
}

// newWiring composes the core services with the store and the configuration.
func newWiring(cfg config.Config, logger *slog.Logger, store *postgres.Store, instanceKey []byte) wiring {
	system := clock.System{}
	kdf := vault.NewKDF(max(runtime.NumCPU(), 2))
	hasher := auth.NewHasher(kdf, vault.DefaultParams, rand.Reader)
	authService := auth.NewService(store, store, hasher, system, rand.Reader,
		auth.Config{IdleTimeout: cfg.SessionIdle, AbsoluteTimeout: cfg.SessionMax})

	workspaces := workspace.NewService(store, system, nil)
	keyRing := vault.NewKeyRing(system, cfg.SessionIdle)
	vaults := vault.NewService(vault.NewDefault(kdf, rand.Reader), store, keyRing)

	hostKeys := hostkey.NewService(store, workspaces, system)
	sites := site.NewService(store, workspaces, vaults, netguard.NewPolicy(cfg.AllowedNetworks, nil), system, rand.Reader, nil)

	return wiring{
		deps: httpapi.Deps{
			Logger:     logger,
			Readiness:  store,
			Web:        webui.Handler(),
			Auth:       authService,
			Workspaces: workspaces,
			Vault:      vaults,
			Sites:      sites,
			HostKeys:   hostKeys,
			Limits: httpapi.Limits{
				SetupIP:       ratelimit.New(system, setupRule, maxTrackedClients),
				LoginIP:       ratelimit.New(system, loginIPRule, maxTrackedClients),
				LoginAccount:  ratelimit.New(system, loginAccountRule, maxTrackedClients),
				VaultAttempts: ratelimit.New(system, vaultRule, maxTrackedClients),
			},
			InstanceKey:    instanceKey,
			Cookie:         httpapi.CookieConfig{Secure: cfg.CookieSecure(), MaxAge: cfg.SessionMax},
			AllowedOrigins: cfg.AllowedOrigins,
			TrustedProxies: cfg.TrustedProxies,
		},
		janitor: []janitorTask{
			{name: "expired sessions", run: authService.PurgeExpired},
			{name: "expired vault keys", run: func(context.Context) (int, error) { return keyRing.Sweep(), nil }},
		},
	}
}

// runJanitor runs the tasks every interval until ctx is canceled, logging failures and non-empty cleanups.
func runJanitor(ctx context.Context, every time.Duration, logger *slog.Logger, tasks []janitorTask) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, task := range tasks {
				removed, err := task.run(ctx)
				if err != nil {
					logger.ErrorContext(ctx, "janitor task failed", "task", task.name, "error", err)
					continue
				}
				if removed > 0 {
					logger.DebugContext(ctx, "janitor removed items", "task", task.name, "count", removed)
				}
			}
		}
	}
}
