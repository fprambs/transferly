package app

import (
	"context"
	"crypto/rand"
	"log/slog"
	"runtime"
	"time"

	"github.com/fprambs/sentinel/internal/adapters/clock"
	"github.com/fprambs/sentinel/internal/adapters/events"
	"github.com/fprambs/sentinel/internal/adapters/ratelimit"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/connect"
	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/core/activity"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/scheduler"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
	"github.com/fprambs/sentinel/internal/transport/webui"
)

const (
	maxTrackedClients = 10000
	connectTimeout    = 15 * time.Second
	poolSweepEvery    = 30 * time.Second
	eventBuffer       = 256
	logTimeout        = 5 * time.Second
)

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
	pool    *connections.Manager
	hub     *events.Hub
	recover func(ctx context.Context) (int, error)
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
	policy := netguard.NewPolicy(cfg.AllowedNetworks, nil)
	sites := site.NewService(store, workspaces, vaults, policy, system, rand.Reader, nil)
	hub := events.New(eventBuffer)
	activityLog := activity.NewService(store, system, cfg.ActivityRetention, nil)
	transfers := transfer.NewService(store, hub, system, transfer.Config{OnFinish: transferLogger(activityLog, logger), Limits: scheduler.Limits{
		Global: cfg.TransferLimits.Global, PerWorkspace: cfg.TransferLimits.PerWorkspace,
		PerUser: cfg.TransferLimits.PerUser, PerSite: cfg.TransferLimits.PerSite,
	}}, nil)
	quickConnections := quick.NewService(workspaces, policy, sites, system, nil)
	pool := connections.New(connect.New(sites, quickConnections, hostKeys, policy, connectTimeout), system, connections.Config{})

	return wiring{
		pool:    pool,
		hub:     hub,
		recover: transfers.Recover,
		deps: httpapi.Deps{
			Logger:      logger,
			Readiness:   store,
			Web:         webui.Handler(),
			Auth:        authService,
			Workspaces:  workspaces,
			Vault:       vaults,
			Sites:       sites,
			HostKeys:    hostKeys,
			Connections: pool,
			Files:       files.NewService(workspaces, pool),
			Transfers:   transfers,
			Activity:    activityLog,
			Import:      fzimport.NewService(workspaces, sites),
			Quick:       quickConnections,
			Events:      hub,
			Limits: httpapi.Limits{
				SetupIP:       ratelimit.New(system, setupRule, maxTrackedClients),
				LoginIP:       ratelimit.New(system, loginIPRule, maxTrackedClients),
				LoginAccount:  ratelimit.New(system, loginAccountRule, maxTrackedClients),
				QuickConnect:  ratelimit.New(system, ratelimit.Rule{Burst: cfg.QuickConnectBurst, Every: 12 * time.Second}, maxTrackedClients),
				VaultAttempts: ratelimit.New(system, vaultRule, maxTrackedClients),
			},
			InstanceKey:    instanceKey,
			Cookie:         httpapi.CookieConfig{Secure: cfg.CookieSecure(), MaxAge: cfg.SessionMax},
			AllowedOrigins: cfg.AllowedOrigins,
			TrustedProxies: cfg.TrustedProxies,
		},
		janitor: []janitorTask{
			{name: "expired sessions", run: authService.PurgeExpired},
			{name: "idle quick connections", run: func(context.Context) (int, error) { return quickConnections.Sweep(cfg.SessionIdle), nil }},
			{name: "old activity", run: activityLog.PurgeExpired},
			{name: "expired vault keys", run: func(context.Context) (int, error) { return keyRing.Sweep(), nil }},
		},
	}
}

// transferLogger returns the hook that adds every transfer that ended to the activity log; a failure to log is reported but never
// fails the transfer.
func transferLogger(log *activity.Service, logger *slog.Logger) func(transfer.Transfer) {
	return func(t transfer.Transfer) {
		ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
		defer cancel()
		err := log.Record(ctx, activity.Entry{
			UserID: t.UserID, WorkspaceID: t.WorkspaceID, SiteID: t.SiteID, SiteName: t.SiteName, Kind: activity.Transfer,
			Path: t.Path, Detail: string(t.Direction) + ":" + string(t.Status), ErrorCode: t.ErrorCode,
		})
		if err != nil {
			logger.WarnContext(ctx, "activity entry not written", "error", err)
		}
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
