package app

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/clock"
	"github.com/fprambs/sentinel/internal/adapters/events"
	"github.com/fprambs/sentinel/internal/adapters/ratelimit"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/connect"
	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/config"
	"github.com/fprambs/sentinel/internal/core/activity"
	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/invite"
	"github.com/fprambs/sentinel/internal/core/mfa"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/passkey"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/scheduler"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/transfer"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
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
	mfaRule          = ratelimit.Rule{Burst: 5, Every: 30 * time.Second}
	invitationRule   = ratelimit.Rule{Burst: 10, Every: 6 * time.Second}
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
func newWiring(cfg config.Config, logger *slog.Logger, store *postgres.Store, instanceKey []byte) (wiring, error) {
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
	keys := wskey.NewService(store, vaults, workspaces, rand.Reader)
	sites := site.NewService(store, workspaces, keys, policy, system, rand.Reader, nil)
	hub := events.New(eventBuffer)
	activityLog := activity.NewService(store, system, cfg.ActivityRetention, nil)
	transfers := transfer.NewService(store, hub, system, transfer.Config{OnFinish: transferLogger(activityLog, logger), Limits: scheduler.Limits{
		Global: cfg.TransferLimits.Global, PerWorkspace: cfg.TransferLimits.PerWorkspace,
		PerUser: cfg.TransferLimits.PerUser, PerSite: cfg.TransferLimits.PerSite,
	}}, nil)
	invites := invite.NewService(store, workspaces, store, authService, system, rand.Reader, 0, nil)
	second := mfa.NewService(store, instanceKey, rand.Reader, system)
	rp, origins := relyingParty(cfg.AllowedOrigins)
	passkeys, err := passkey.NewService(store, passkey.Config{RPID: rp, DisplayName: "Sentinel", Origins: origins}, system, rand.Reader, nil)
	if err != nil {
		return wiring{}, err
	}
	auditTrail := audit.NewService(store, workspaces, system)
	keys.OnRotate(rotationLogger(auditTrail, logger))
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
			Keys:        keys,
			Sites:       sites,
			HostKeys:    hostKeys,
			Connections: pool,
			Files:       files.NewService(workspaces, pool),
			Transfers:   transfers,
			Activity:    activityLog,
			Import:      fzimport.NewService(workspaces, sites),
			Invites:     invites,
			Audit:       auditTrail,
			MFA:         second,
			Passkeys:    passkeys,
			Quick:       quickConnections,
			Events:      hub,
			Limits: httpapi.Limits{
				SetupIP:       ratelimit.New(system, setupRule, maxTrackedClients),
				LoginIP:       ratelimit.New(system, loginIPRule, maxTrackedClients),
				LoginAccount:  ratelimit.New(system, loginAccountRule, maxTrackedClients),
				QuickConnect:  ratelimit.New(system, ratelimit.Rule{Burst: cfg.QuickConnectBurst, Every: 12 * time.Second}, maxTrackedClients),
				VaultAttempts: ratelimit.New(system, vaultRule, maxTrackedClients),
				Invitations:   ratelimit.New(system, invitationRule, maxTrackedClients),
				MFA:           ratelimit.New(system, mfaRule, maxTrackedClients),
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
			{name: "expired invitations", run: invites.PurgeExpired},
			{name: "expired vault keys", run: func(context.Context) (int, error) { return keyRing.Sweep(), nil }},
		},
	}, nil
}

// relyingParty derives the WebAuthn relying party from the allowed origins: the host of the first one, and every origin. Without any it
// is localhost, which browsers accept over plain http for development.
func relyingParty(origins []string) (string, []string) {
	if len(origins) == 0 {
		return "localhost", []string{"http://localhost"}
	}
	host := origins[0]
	if u, err := url.Parse(origins[0]); err == nil {
		host = u.Hostname()
	}
	return host, origins
}

// rotationLogger returns the hook that adds every rotation of a workspace key to the audit trail; a failure to log is reported but never
// fails the rotation.
func rotationLogger(trail *audit.Service, logger *slog.Logger) func(context.Context, uuid.UUID, uuid.UUID, int) {
	return func(ctx context.Context, workspaceID, actor uuid.UUID, version int) {
		err := trail.Record(ctx, audit.Event{WorkspaceID: workspaceID, ActorID: actor, Action: audit.KeyRotated, Meta: map[string]string{"version": strconv.Itoa(version)}})
		if err != nil {
			logger.WarnContext(ctx, "audit event not recorded", "action", audit.KeyRotated, "error", err)
		}
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
