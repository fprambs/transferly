package postgres_test

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/adapters/store/postgres/pgtest"
	"github.com/fprambs/transferly/internal/adapters/store/storetest"
	"github.com/fprambs/transferly/internal/core/activity"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/bookmark"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/invite"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/mfa"
	"github.com/fprambs/transferly/internal/core/passkey"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/core/vault"
	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/fprambs/transferly/internal/core/wskey"
)

// openMigrated returns a store over a fresh, migrated database.
func openMigrated(t *testing.T) *postgres.Store {
	t.Helper()
	dbURL := pgtest.MigratedDatabase(t)
	store, err := postgres.Open(t.Context(), dbURL)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// TestAuthStore_Conformance runs the shared authentication store suite against Postgres.
func TestAuthStore_Conformance(t *testing.T) {
	storetest.RunAuth(t, func(t *testing.T) storetest.AuthStore { return openMigrated(t) })
}

// TestAuthStore_ReportsDatabaseFailures verifies that every operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestAuthStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id, at := uuid.New(), time.Now()
	user := auth.User{ID: id, Email: "x@example.com", Role: auth.RoleUser, CreatedAt: at}
	session := auth.Session{ID: id, UserID: id, TokenHash: []byte("h"), CreatedAt: at, LastSeenAt: at, ExpiresAt: at}

	calls := map[string]error{
		"CreateFirstUser":                store.CreateFirstUser(ctx, user),
		"CreateUser":                     store.CreateUser(ctx, user),
		"UpdatePasswordHash":             store.UpdatePasswordHash(ctx, id, "h"),
		"UpdatePreferences":              store.UpdatePreferences(ctx, id, auth.Preferences{Locale: "en", Timezone: "UTC", Theme: "transferly-dark", FileDoubleClick: auth.FileDoubleClickDownload}),
		"CreateSession":                  store.CreateSession(ctx, session),
		"TouchSession":                   store.TouchSession(ctx, id, at),
		"DeleteSession":                  store.DeleteSession(ctx, id),
		"UserByEmail":                    second(store.UserByEmail(ctx, "x@example.com")),
		"UserByID":                       second(store.UserByID(ctx, id)),
		"CountUsers":                     second(store.CountUsers(ctx)),
		"SessionByTokenHash":             second(store.SessionByTokenHash(ctx, []byte("h"))),
		"DeleteUserSessions":             second(store.DeleteUserSessions(ctx, id)),
		"DeleteExpiredSessions":          second(store.DeleteExpiredSessions(ctx, at, auth.IdleCutoffs{Browser: at, CLI: at})),
		"ListUserSessions":               second(store.ListUserSessions(ctx, id, auth.SessionCLI)),
		"DeleteUserSession":              second(store.DeleteUserSession(ctx, id, id)),
		"CreateCLIAuthorization":         store.CreateCLIAuthorization(ctx, auth.CLIAuthorization{ID: id, UserID: id, CodeHash: []byte("c"), CreatedAt: at, ExpiresAt: at}),
		"UseCLIAuthorization":            second(store.UseCLIAuthorization(ctx, []byte("c"), at)),
		"DeleteExpiredCLIAuthorizations": second(store.DeleteExpiredCLIAuthorizations(ctx, at)),
	}
	for name, err := range calls {
		if err == nil {
			t.Errorf("%s on a closed pool = nil error, want the database error", name)
		}
	}
}

// TestCreateFirstUser_WaitsForTheCreationLock verifies deterministically that first-user creation is serialized
// through the advisory lock: while another transaction holds the lock the call blocks, and once the lock is
// released it completes. A statistical race test cannot detect a missing lock because the window is tiny.
func TestCreateFirstUser_WaitsForTheCreationLock(t *testing.T) {
	dbURL := pgtest.MigratedDatabase(t)
	ctx := context.Background()
	store, err := postgres.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	holder, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close(ctx) }()
	tx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(4097083627)"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.CreateFirstUser(ctx, auth.User{ID: uuid.New(), Email: "lock@example.com", Role: auth.RoleAdmin, CreatedAt: time.Now()})
	}()
	select {
	case err := <-done:
		t.Fatalf("CreateFirstUser() returned %v while the lock was held, want it to wait", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("CreateFirstUser() after the lock was released = %v, want success", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateFirstUser() did not finish after the lock was released")
	}
}

// second returns the error of a call that also returns a value.
func second[T any](_ T, err error) error { return err }

// TestWorkspaceStore_Conformance runs the shared workspace store suite against Postgres.
func TestWorkspaceStore_Conformance(t *testing.T) {
	storetest.RunWorkspace(t, func(t *testing.T) storetest.WorkspaceStore { return openMigrated(t) })
}

// TestFactorStore_Conformance runs the shared second factor store suite against Postgres.
func TestFactorStore_Conformance(t *testing.T) {
	storetest.RunFactor(t, func(t *testing.T) storetest.FactorStore { return openMigrated(t) })
}

// TestFactorStore_ReportsDatabaseFailures verifies that every second factor operation returns the database error, using a closed pool.
func TestFactorStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()
	calls := map[string]error{
		"Factor":          second(store.Factor(ctx, id)),
		"SaveFactor":      store.SaveFactor(ctx, id, nil),
		"ConfirmFactor":   store.ConfirmFactor(ctx, id, 1, nil),
		"AdvanceStep":     second(store.AdvanceStep(ctx, id, 1)),
		"DeleteFactor":    store.DeleteFactor(ctx, id),
		"ReplaceRecovery": store.ReplaceRecovery(ctx, id, nil),
		"UseRecovery":     second(store.UseRecovery(ctx, id, nil)),
		"RecoveryLeft":    second(store.RecoveryLeft(ctx, id)),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, mfa.ErrNotEnrolled) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestPasskeyStore_Conformance runs the shared passkey store suite against Postgres.
func TestPasskeyStore_Conformance(t *testing.T) {
	storetest.RunPasskey(t, func(t *testing.T) storetest.PasskeyStore { return openMigrated(t) })
}

// TestPasskeyStore_ReportsDatabaseFailures verifies that every passkey operation returns the database error, using a closed pool, and that a
// stored credential that cannot be read is reported.
func TestPasskeyStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	id := uuid.New()
	if _, err := store.DB().Exec(t.Context(), "INSERT INTO users (id, email, password_hash) VALUES ($1, 'a@example.com', 'x')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(t.Context(), "INSERT INTO user_passkeys (id, user_id, name, credential_id, credential, created_at) VALUES ($1, $1, 'x', 'c', '[1]', now())", id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Passkeys(t.Context(), id); err == nil {
		t.Error("Passkeys() read a credential that is not an object")
	}
	store.Close()
	ctx := context.Background()
	calls := map[string]error{
		"Passkeys":      second(store.Passkeys(ctx, id)),
		"AddPasskey":    store.AddPasskey(ctx, id, passkey.Passkey{ID: id}),
		"DeletePasskey": store.DeletePasskey(ctx, id, id),
		"TouchPasskey":  store.TouchPasskey(ctx, id, id, webauthn.Credential{}, time.Now()),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, passkey.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestAuditStore_Conformance runs the shared audit store suite against Postgres.
func TestAuditStore_Conformance(t *testing.T) {
	storetest.RunAudit(t, func(t *testing.T) storetest.AuditStore { return openMigrated(t) })
}

// TestAuditStore_ReportsDatabaseFailures verifies that both audit operations return the database error, using a closed pool.
func TestAuditStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()
	if err := store.InsertAudit(ctx, audit.Event{WorkspaceID: id}); err == nil {
		t.Error("InsertAudit() hid the database error")
	}
	if _, err := store.AuditByWorkspace(ctx, id, audit.Filter{}, 10); err == nil {
		t.Error("AuditByWorkspace() hid the database error")
	}
}

// TestAuditStore_RefusesUnreadableMetadata verifies that a row whose metadata is not an object is reported instead of skipped.
func TestAuditStore_RefusesUnreadableMetadata(t *testing.T) {
	store := openMigrated(t)
	id := uuid.New()
	if _, err := store.DB().Exec(t.Context(), "INSERT INTO users (id, email, password_hash) VALUES ($1, 'a@example.com', 'x')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(t.Context(), "INSERT INTO workspaces (id, name, kind, owner_id) VALUES ($1, 'W', 'team', $2)", id, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(t.Context(), "INSERT INTO audit_events (workspace_id, action, meta) VALUES ($1, 'x', '[1]')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuditByWorkspace(t.Context(), id, audit.Filter{}, 10); err == nil {
		t.Error("AuditByWorkspace() read a row whose metadata is not an object")
	}
}

// TestKeyStore_Conformance runs the shared workspace key store suite against Postgres.
func TestKeyStore_Conformance(t *testing.T) {
	storetest.RunKeys(t, func(t *testing.T) storetest.KeyStore { return openMigrated(t) })
}

// TestKeyStore_ReportsDatabaseFailures verifies that every key operation returns the database error instead of hiding it, using a
// store whose connection pool is closed.
func TestKeyStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"UserKeys":    second(store.UserKeys(ctx, id)),
		"SetUserKeys": second(store.SetUserKeys(ctx, id, wskey.UserKeys{})),
		"KeyState":    second3(store.KeyState(ctx, id)),
		"Grant":       second(store.Grant(ctx, id, id, 1)),
		"AddGrant":    store.AddGrant(ctx, id, id, 1, nil),
		"Ungranted":   second(store.Ungranted(ctx, id, 1)),
		"Granted":     second(store.Granted(ctx, id, 1)),
		"CreateTeam":  store.CreateTeam(ctx, workspace.Workspace{ID: id, OwnerID: id}, nil),
		"Rotate":      store.Rotate(ctx, id, 1, 2, id, nil),
		"Secrets":     second(store.Secrets(ctx, id, 2)),
		"Reseal":      store.Reseal(ctx, id, nil),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, wskey.ErrNoKeys) || errors.Is(err, wskey.ErrPending) || errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// second3 returns the error of a call that also returns two values.
func second3[A, B any](_ A, _ B, err error) error { return err }

// TestInviteStore_Conformance runs the shared invitation store suite against Postgres.
func TestInviteStore_Conformance(t *testing.T) {
	storetest.RunInvite(t, func(t *testing.T) storetest.InviteStore { return openMigrated(t) })
}

// TestInviteStore_ReportsDatabaseFailures verifies that every invitation operation returns the database error instead of hiding
// it, using a store whose connection pool is closed.
func TestInviteStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id, at := uuid.New(), time.Now()

	calls := map[string]error{
		"Create":       store.Create(ctx, invite.Invitation{ID: id, WorkspaceID: id}, []byte("h")),
		"ByTokenHash":  second(store.ByTokenHash(ctx, []byte("h"), at)),
		"Pending":      second(store.Pending(ctx, id, at)),
		"CountPending": second(store.CountPending(ctx, id, at)),
		"Revoke":       store.Revoke(ctx, id, id),
		"Accept":       second(store.Accept(ctx, id, id, at)),
		"PurgeExpired": second(store.PurgeExpired(ctx, at)),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, invite.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestWorkspaceStore_ReportsDatabaseFailures verifies that every workspace operation returns the database
// error instead of hiding it, using a store whose connection pool is closed.
func TestWorkspaceStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"CreateWorkspace":   store.CreateWorkspace(ctx, workspace.Workspace{ID: id, OwnerID: id}),
		"PersonalWorkspace": second(store.PersonalWorkspace(ctx, id)),
		"WorkspacesForUser": second(store.WorkspacesForUser(ctx, id)),
		"RoleOf":            second(store.RoleOf(ctx, id, id)),
		"Workspace":         second(store.Workspace(ctx, id)),
		"Members":           second(store.Members(ctx, id)),
		"AddMember":         store.AddMember(ctx, id, id, workspace.RoleViewer),
		"SetRole":           store.SetRole(ctx, id, id, workspace.RoleViewer),
		"RemoveMember":      store.RemoveMember(ctx, id, id),
		"TransferOwnership": store.TransferOwnership(ctx, id, id, id),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestVaultStore_Conformance runs the shared vault store suite against Postgres.
func TestVaultStore_Conformance(t *testing.T) {
	storetest.RunVault(t, func(t *testing.T) storetest.VaultStore { return openMigrated(t) })
}

// TestVaultStore_ReportsDatabaseFailures verifies that every vault operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestVaultStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"CreateVault": store.CreateVault(ctx, id, vault.Enrollment{}),
		"Vault":       second(store.Vault(ctx, id)),
		"UpdateVault": store.UpdateVault(ctx, id, vault.Enrollment{}),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, vault.ErrNoVault) || errors.Is(err, vault.ErrVaultExists) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestSiteStore_Conformance runs the shared site store suite against Postgres.
func TestSiteStore_Conformance(t *testing.T) {
	storetest.RunSite(t, func(t *testing.T) storetest.SiteStore { return openMigrated(t) })
}

// TestSiteStore_ReportsDatabaseFailures verifies that every site operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestSiteStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()
	s := site.Site{ID: id, WorkspaceID: id}

	calls := map[string]error{
		"CreateSite": store.CreateSite(ctx, s, nil),
		"UpdateSite": store.UpdateSite(ctx, s, nil, nil),
		"Site":       second(store.Site(ctx, id, id)),
		"Sites":      second(store.Sites(ctx, id)),
		"DeleteSite": store.DeleteSite(ctx, id, id),
		"Secret":     second(store.Secret(ctx, id, id, site.SecretPassword)),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, site.ErrNotFound) || errors.Is(err, site.ErrNoSecret) || errors.Is(err, site.ErrNameTaken) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestHostKeyStore_Conformance runs the shared host key store suite against Postgres.
func TestHostKeyStore_Conformance(t *testing.T) {
	storetest.RunHostKey(t, func(t *testing.T) storetest.HostKeyStore { return openMigrated(t) })
}

// TestHostKeyStore_ReportsDatabaseFailures verifies that every host key operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestHostKeyStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"Trusted": second(store.Trusted(ctx, id, "h", 22)),
		"List":    second(store.List(ctx, id)),
		"Add":     store.Add(ctx, id, hostkey.Key{}),
		"Delete":  store.Delete(ctx, id, "h", 22, "t", ""),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, hostkey.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestTransferStore_Conformance runs the shared transfer store suite against Postgres.
func TestTransferStore_Conformance(t *testing.T) {
	storetest.RunTransfer(t, func(t *testing.T) storetest.TransferStore { return openMigrated(t) })
}

// TestTransferStore_ReportsDatabaseFailures verifies that every transfer operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestTransferStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"Create":  store.CreateTransfer(ctx, transfer.Transfer{ID: id}),
		"Update":  store.UpdateTransfer(ctx, transfer.Transfer{ID: id}),
		"Get":     second(store.TransferByID(ctx, id, id)),
		"List":    second(store.ListTransfers(ctx, id, uuid.Nil, 5)),
		"ListWs":  second(store.ListTransfers(ctx, id, id, 5)),
		"Delete":  store.DeleteTransfer(ctx, id, id),
		"Recover": second(store.InterruptActive(ctx, time.Now())),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, transfer.ErrNotFound) || errors.Is(err, transfer.ErrExists) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestActivityStore_Conformance runs the shared activity store suite against Postgres.
func TestActivityStore_Conformance(t *testing.T) {
	storetest.RunActivity(t, func(t *testing.T) storetest.ActivityStore { return openMigrated(t) })
}

// TestActivityStore_ReportsDatabaseFailures verifies that every activity operation returns the database error instead of hiding it,
// using a store whose connection pool is closed.
func TestActivityStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"Insert": store.InsertActivity(ctx, activity.Entry{ID: id}),
		"List":   second(store.ActivityByUser(ctx, id, id, uuid.Nil, 5)),
		"Purge":  second(store.PurgeActivity(ctx, time.Now())),
	}
	for name, err := range calls {
		if err == nil {
			t.Errorf("%s = nil; want a database error", name)
		}
	}
}

// TestCollectionStore_Conformance runs the shared collection store suite against Postgres.
func TestCollectionStore_Conformance(t *testing.T) {
	storetest.RunCollection(t, func(t *testing.T) storetest.SiteStore { return openMigrated(t) })
}

// TestCollectionStore_ReportsDatabaseFailures verifies that every collection operation returns the database error
// instead of hiding it, using a store whose connection pool is closed.
func TestCollectionStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()
	c := site.Collection{ID: id, WorkspaceID: id, Name: "x"}

	calls := map[string]error{
		"CreateCollection": store.CreateCollection(ctx, c),
		"UpdateCollection": store.UpdateCollection(ctx, c),
		"Collection":       second(store.Collection(ctx, id, id)),
		"Collections":      second(store.Collections(ctx, id)),
		"DeleteCollection": store.DeleteCollection(ctx, id, id),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, site.ErrCollectionNotFound) || errors.Is(err, site.ErrCollectionNameTaken) || errors.Is(err, site.ErrCollectionNotEmpty) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestBookmarkStore_Conformance runs the shared bookmark store suite against Postgres.
func TestBookmarkStore_Conformance(t *testing.T) {
	storetest.RunBookmark(t, func(t *testing.T) storetest.BookmarkStore { return openMigrated(t) })
}

// TestBookmarkStore_ReportsDatabaseFailures verifies that every bookmark operation returns the database error instead of hiding it,
// using a store whose connection pool is closed.
func TestBookmarkStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()
	b := bookmark.Bookmark{ID: id, WorkspaceID: id, SiteID: id, Name: "x", Path: "/"}

	calls := map[string]error{
		"CreateBookmark": store.CreateBookmark(ctx, b),
		"UpdateBookmark": store.UpdateBookmark(ctx, b),
		"Bookmark":       second(store.Bookmark(ctx, id, id, id)),
		"Bookmarks":      second(store.Bookmarks(ctx, id, id)),
		"DeleteBookmark": store.DeleteBookmark(ctx, id, id, id),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, bookmark.ErrNotFound) || errors.Is(err, bookmark.ErrNameTaken) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestAdminStore_Conformance runs the shared administration store suite against Postgres.
func TestAdminStore_Conformance(t *testing.T) {
	storetest.RunAdmin(t, func(t *testing.T) storetest.AdminStore { return openMigrated(t) })
}

// TestAdminStore_ReportsDatabaseFailures verifies that every administration operation returns the database error, using a closed pool.
func TestAdminStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"Accounts":          second(store.Accounts(ctx)),
		"Account":           second(store.Account(ctx, id)),
		"SetDisabled":       store.SetDisabled(ctx, id, true),
		"SetAccountRole":    store.SetAccountRole(ctx, id, auth.RoleUser),
		"ActiveAdmins":      second(store.ActiveAdmins(ctx)),
		"ResetSecondFactor": store.ResetSecondFactor(ctx, id),
		"Networks":          second(store.Networks(ctx)),
		"ReplaceNetworks":   store.ReplaceNetworks(ctx, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, id, time.Now()),
		"AuditInstance":     second(store.AuditInstance(ctx, audit.Filter{}, 10)),
	}
	for name, err := range calls {
		if err == nil || errors.Is(err, admin.ErrNotFound) {
			t.Errorf("%s = %v; want a database error", name, err)
		}
	}
}

// TestAdminStore_RefusesUnreadableData verifies that a stored network that is not a network, and instance events whose metadata is not an
// object, are reported instead of skipped.
func TestAdminStore_RefusesUnreadableData(t *testing.T) {
	store := openMigrated(t)
	ctx := t.Context()
	if _, err := store.DB().Exec(ctx, "INSERT INTO network_allowances (cidr, created_at) VALUES ('not a network', now())"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Networks(ctx); err == nil {
		t.Error("Networks() read a value that is not a network")
	}
	if _, err := store.DB().Exec(ctx, "INSERT INTO audit_events (action, meta) VALUES ('admin.x', '[1]')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuditInstance(ctx, audit.Filter{}, 10); err == nil {
		t.Error("AuditInstance() read a row whose metadata is not an object")
	}
}

// TestLimitsStore_Conformance runs the shared limits store suite against Postgres.
func TestLimitsStore_Conformance(t *testing.T) {
	storetest.RunLimits(t, func(t *testing.T) storetest.LimitsStore { return openMigrated(t) })
}

// TestLimitsStore_ReportsDatabaseFailures verifies that every limits operation returns the database error, using a closed pool.
func TestLimitsStore_ReportsDatabaseFailures(t *testing.T) {
	store := openMigrated(t)
	store.Close()
	ctx := context.Background()
	id := uuid.New()

	calls := map[string]error{
		"Limits":          second3(store.Limits(ctx)),
		"SaveLimits":      store.SaveLimits(ctx, limits.Limits{}, id, time.Now()),
		"CountSites":      second(store.CountSites(ctx, id)),
		"CountOwnedTeams": second(store.CountOwnedTeams(ctx, id)),
		"CountMembers":    second(store.CountMembers(ctx, id, time.Now())),
		"BytesSince":      second(store.BytesSince(ctx, id, time.Now())),
	}
	for name, err := range calls {
		if err == nil {
			t.Errorf("%s hid the database error", name)
		}
	}
}

// TestLimitsStore_RefusesUnreadableLimits verifies that stored limits that are not an object of limits are reported instead of ignored.
func TestLimitsStore_RefusesUnreadableLimits(t *testing.T) {
	store := openMigrated(t)
	if _, err := store.DB().Exec(t.Context(), "INSERT INTO instance_limits (data, updated_at) VALUES ('[1]', now())"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Limits(t.Context()); err == nil {
		t.Error("Limits() read a value that is not an object of limits")
	}
}

// TestAdminStore_TheLastAdministratorSurvivesRaces verifies that two administrators who remove each other at the same moment do not leave
// the instance without one.
//
// Scenarios:
//   - with two active administrators, disabling both at once succeeds for exactly one of them and refuses the other with ErrLastAdmin
//   - the same holds when both are demoted at once
//   - the administrator who stays is still active and still an administrator, in every round
func TestAdminStore_TheLastAdministratorSurvivesRaces(t *testing.T) {
	ctx := t.Context()
	store := openMigrated(t)
	for round := range 15 {
		a, b := uuid.New(), uuid.New()
		for i, id := range []uuid.UUID{a, b} {
			email := "admin-" + strconv.Itoa(round) + "-" + strconv.Itoa(i) + "@example.com"
			if _, err := store.DB().Exec(ctx, "INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, 'x', 'admin')", id, email); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.DB().Exec(ctx, "UPDATE users SET role = 'user' WHERE role = 'admin' AND id NOT IN ($1, $2)", a, b); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		for _, id := range []uuid.UUID{a, b} {
			go func() {
				if round%2 == 0 {
					results <- store.SetDisabled(ctx, id, true)
					return
				}
				results <- store.SetAccountRole(ctx, id, auth.RoleUser)
			}()
		}
		first, second := <-results, <-results
		refused := 0
		for _, err := range []error{first, second} {
			if errors.Is(err, admin.ErrLastAdmin) {
				refused++
			} else if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if refused != 1 {
			t.Fatalf("round %d: %d changes were refused, want exactly one (%v, %v)", round, refused, first, second)
		}
		if n, err := store.ActiveAdmins(ctx); err != nil || n != 1 {
			t.Fatalf("round %d: %d active administrators, %v; want 1", round, n, err)
		}
	}
}
