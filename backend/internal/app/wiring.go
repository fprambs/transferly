package app

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/clock"
	"github.com/fprambs/transferly/internal/adapters/events"
	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/adapters/ratelimit"
	"github.com/fprambs/transferly/internal/adapters/remotefs/connect"
	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/config"
	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/backup"
	"github.com/fprambs/transferly/internal/core/bookmark"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/credentials"
	"github.com/fprambs/transferly/internal/core/files"
	"github.com/fprambs/transferly/internal/core/fzimport"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/invite"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/mfa"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/passkey"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/scheduler"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/core/wskey"
	"github.com/fprambs/transferly/internal/transport/httpapi"
	"github.com/fprambs/transferly/internal/transport/webui"
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
	if cfg.VaultRememberSessions {
		vaults.RememberSessions(store, rand.Reader)
	}

	hostKeys := hostkey.NewService(store, workspaces, system)
	policy := netguard.NewPolicy(cfg.AllowedNetworks, nil)
	keys := wskey.NewService(store, vaults, workspaces, rand.Reader)
	catalog := connectors.Builtin()
	sites := site.NewService(store, workspaces, keys, policy, catalog, system, rand.Reader, nil)
	hub := events.New(eventBuffer)
	activityLog := activity.NewService(store, system, cfg.ActivityRetention, nil)
	transfers := transfer.NewService(store, hub, system, transfer.Config{OnPersistenceError: persistenceLogger(logger), OnFinish: transferLogger(activityLog, logger), Limits: scheduler.Limits{
		Global: cfg.TransferLimits.Global, PerWorkspace: cfg.TransferLimits.PerWorkspace,
		PerUser: cfg.TransferLimits.PerUser, PerSite: cfg.TransferLimits.PerSite,
	}}, nil)
	invites := invite.NewService(store, workspaces, store, authService, system, rand.Reader, 0, nil)
	quotas := limits.NewService(store, transfers, startingLimits(cfg), system)
	sites.UseQuota(quotas)
	offered := connectors.NewService(catalog, store, system)
	sites.UseOffering(offered)
	invites.UseQuota(quotas)
	transfers.UseQuota(quotas)
	second := mfa.NewService(store, instanceKey, rand.Reader, system)
	rp, origins := relyingParty(cfg.AllowedOrigins)
	passkeys, err := passkey.NewService(store, passkey.Config{RPID: rp, DisplayName: "Transferly", Origins: origins}, system, rand.Reader, nil)
	if err != nil {
		return wiring{}, err
	}
	auditTrail := audit.NewService(store, workspaces, system)
	keys.OnRotate(rotationLogger(auditTrail, logger))
	quickConnections := quick.NewService(workspaces, sites, system, nil)
	askedPasswords := credentials.New(system, cfg.SessionMax)
	connectionLog := connlog.New(hub, system)
	importer := fzimport.NewService(workspaces, sites)
	broker := prompts.New(hub, prompts.DefaultWait)
	google := googleauth.New(googleauth.Config{
		ClientID: cfg.Google.ClientID, ClientSecret: cfg.Google.ClientSecret,
		AuthURL: cfg.Google.AuthURL, TokenURL: cfg.Google.TokenURL, APIURL: cfg.Google.APIURL,
	}, &http.Client{Timeout: connectTimeout, Transport: &http.Transport{DialContext: policy.Dialer(connectTimeout).DialContext}}, system.Now)
	connector := connect.New(sites, quickConnections, hostKeys, policy, connectTimeout,
		connect.AskedPasswords(askedPasswords), connect.ConnectionLog(connectionLog), connect.Prompts(broker),
		connect.Offering(offered), connect.Google(google), connect.ServerCommands(quotas.ServerCommandsAllowed), connect.StrictServers(store))
	pool := connections.New(connector, system, connections.Config{OnChange: connections.PublishTo(hub)})

	return wiring{
		pool:    pool,
		hub:     hub,
		recover: transfers.Recover,
		deps: httpapi.Deps{
			Logger:        logger,
			Readiness:     store,
			Web:           webui.Handler(),
			Auth:          authService,
			Workspaces:    workspaces,
			Vault:         vaults,
			Keys:          keys,
			Sites:         sites,
			Probe:         connector,
			HostKeys:      hostKeys,
			Bookmarks:     bookmark.NewService(store, store, workspaces, system, nil),
			Connections:   pool,
			Credentials:   askedPasswords,
			Connectors:    offered,
			Google:        google,
			Files:         files.NewService(workspaces, pool).WithCommands(quotas.ServerCommandsAllowed),
			Transfers:     transfers,
			Activity:      activityLog,
			Import:        importer,
			Backup:        backup.NewService(workspaces, sites, importer, backup.NewSealer(kdf, vault.DefaultParams, rand.Reader), system),
			Invites:       invites,
			Audit:         auditTrail,
			Admin:         admin.NewService(store, policy, quotas, system),
			Quotas:        quotas,
			MFA:           second,
			Passkeys:      passkeys,
			Quick:         quickConnections,
			Events:        hub,
			ConnectionLog: connectionLog,
			Prompts:       broker,
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
			{name: "asked passwords", run: func(context.Context) (int, error) { return askedPasswords.Sweep(openConnections(pool)), nil }},
			{name: "old activity", run: activityLog.PurgeExpired},
			{name: "expired invitations", run: invites.PurgeExpired},
			{name: "expired vault keys", run: func(context.Context) (int, error) { return keyRing.Sweep(), nil }},
		},
	}, nil
}

// openConnections reports, for the sweep of the asked passwords, whether a user still has a connection open to a site.
func openConnections(pool *connections.Manager) func(userID, workspaceID, siteID uuid.UUID) bool {
	return func(userID, workspaceID, siteID uuid.UUID) bool {
		_, total := pool.Stats(connections.Key{UserID: userID, WorkspaceID: workspaceID, SiteID: siteID})
		return total > 0
	}
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

// startingLimits turns the configuration into the limits the server starts with, before an administrator changes them.
func startingLimits(cfg config.Config) limits.Limits {
	return limits.Limits{
		BandwidthGlobal: int64(cfg.Quotas.BandwidthGlobal), BandwidthPerUser: int64(cfg.Quotas.BandwidthPerUser),
		TransfersGlobal: cfg.TransferLimits.Global, TransfersPerWorkspace: cfg.TransferLimits.PerWorkspace,
		TransfersPerUser: cfg.TransferLimits.PerUser, TransfersPerSite: cfg.TransferLimits.PerSite,
		MaxSitesPerWorkspace: cfg.Quotas.MaxSitesPerWorkspace, MaxTeamsPerUser: cfg.Quotas.MaxTeamsPerUser,
		MaxMembersPerTeam: cfg.Quotas.MaxMembersPerTeam, DailyBytesPerUser: int64(cfg.Quotas.DailyBytesPerUser),
	}
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

// persistenceLogger reports transfer history failures while keeping remote contents out of diagnostics.
func persistenceLogger(logger *slog.Logger) func(error) {
	return func(err error) { logger.Error("transfer persistence failed", "error", err) }
}
