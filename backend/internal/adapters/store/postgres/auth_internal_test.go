package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres/db"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/invite"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/core/wskey"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errStep is the failure injected by the fake transaction.
var errStep = errors.New("step failed")

// fakeTx is a transaction that fails at a chosen step of first-user or workspace creation.
type fakeTx struct {
	pgx.Tx
	failAt string
	users  int64
}

// Exec fails for the lock or an insert step when selected.
func (f fakeTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if (f.failAt == "lock" && strings.Contains(sql, "pg_advisory")) || (f.failAt == "insert" && strings.Contains(sql, "INSERT INTO users")) ||
		(f.failAt == "workspace" && strings.Contains(sql, "INSERT INTO workspaces")) || (f.failAt == "membership" && strings.Contains(sql, "INSERT INTO memberships")) ||
		(f.failAt == "site" && strings.Contains(sql, "INSERT INTO sites")) || (f.failAt == "update-site" && strings.HasPrefix(sql, "-- name: UpdateSite")) ||
		(f.failAt == "transfer-role" && strings.Contains(sql, "-- name: SetMembershipRole")) || (f.failAt == "transfer-owner" && strings.Contains(sql, "UPDATE workspaces SET owner_id")) ||
		(f.failAt == "invite-delete" && strings.Contains(sql, "DELETE FROM invitations")) || (f.failAt == "invite-insert" && strings.Contains(sql, "INSERT INTO invitations")) ||
		(f.failAt == "team-key" && strings.Contains(sql, "INSERT INTO workspace_key_grants")) || (f.failAt == "delete-member" && strings.Contains(sql, "DELETE FROM memberships")) || (f.failAt == "remove-grants" && strings.Contains(sql, "DELETE FROM workspace_key_grants WHERE workspace_id = $1 AND user_id")) ||
		(f.failAt == "stale-grants" && strings.Contains(sql, "USING workspaces")) || (f.failAt == "rekey" && strings.Contains(sql, "SET rekey_pending = true")) ||
		(f.failAt == "bump" && strings.Contains(sql, "SET key_version")) || (f.failAt == "rotate-grant" && strings.Contains(sql, "INSERT INTO workspace_key_grants")) ||
		(f.failAt == "reseal" && strings.Contains(sql, "UPDATE site_secrets")) || (f.failAt == "totp-upsert" && strings.Contains(sql, "INSERT INTO user_totp")) || (f.failAt == "totp-confirm" && strings.Contains(sql, "SET confirmed = true")) ||
		(f.failAt == "totp-delete" && strings.Contains(sql, "DELETE FROM user_totp")) || (f.failAt == "recovery-delete" && strings.Contains(sql, "DELETE FROM user_recovery_codes")) ||
		(f.failAt == "recovery-insert" && strings.Contains(sql, "INSERT INTO user_recovery_codes")) || (f.failAt == "member-if-missing" && strings.Contains(sql, "ON CONFLICT (workspace_id, user_id)")) ||
		(f.failAt == "secret" && strings.Contains(sql, "INSERT INTO site_secrets")) || (f.failAt == "remove-secret" && strings.Contains(sql, "DELETE FROM site_secrets")) {
		return pgconn.CommandTag{}, errStep
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// QueryRow returns the user count, a consumed invitation or a workspace, or a failing row when the step is selected.
func (f fakeTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	switch {
	case f.failAt == "count":
		return scanRow{err: errStep}
	case strings.Contains(sql, "DELETE FROM invitations"):
		return invitationRow{err: f.stepError("invite-consume")}
	case strings.Contains(sql, "FOR UPDATE"):
		return keyRow{err: f.stepError("rotate-lock"), version: f.users}
	case strings.Contains(sql, "FROM workspaces WHERE id"):
		return invitationRow{err: f.stepError("invite-workspace"), workspace: true}
	}
	return scanRow{n: f.users}
}

// stepError returns the injected error when the step is the selected one.
func (f fakeTx) stepError(step string) error {
	if f.failAt == step {
		return errStep
	}
	return nil
}

// keyRow is the result of locking the key version of a workspace.
type keyRow struct {
	err     error
	version int64
}

// Scan copies the version, or returns the injected error.
func (r keyRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int32)) = int32(r.version)
	return nil
}

// invitationRow is the result of consuming an invitation or reading its workspace.
type invitationRow struct {
	err       error
	workspace bool
}

// Scan fills the columns of the consumed invitation, or of the workspace.
func (r invitationRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.workspace {
		*(dest[0].(*uuid.UUID)) = uuid.New()
		*(dest[1].(*string)) = "Ops"
		*(dest[2].(*string)) = "team"
		*(dest[3].(*uuid.UUID)) = uuid.New()
		*(dest[4].(*time.Time)) = time.Now()
		return nil
	}
	*(dest[0].(*uuid.UUID)) = uuid.New()
	*(dest[1].(*string)) = "viewer"
	return nil
}

// Commit fails when the commit step is selected.
func (f fakeTx) Commit(context.Context) error {
	if f.failAt == "commit" {
		return errStep
	}
	return nil
}

// Rollback does nothing.
func (fakeTx) Rollback(context.Context) error { return nil }

// scanRow is a query result holding one integer or an error.
type scanRow struct {
	n   int64
	err error
}

// Scan copies the integer into the destination, or returns the injected error.
func (r scanRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int64)) = r.n
	return nil
}

// TestCreateFirstUser_ReportsFailuresInsideTheTransaction verifies that a failure at any step of the
// transaction, including starting it, is returned to the caller.
//
// Scenarios:
//   - the transaction cannot start
//   - taking the creation lock, counting the users, inserting the user and committing each fail
//   - a store that already has users refuses with ErrSetupComplete without inserting
func TestCreateFirstUser_ReportsFailuresInsideTheTransaction(t *testing.T) {
	for _, step := range []string{"lock", "count", "insert", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.CreateFirstUser(context.Background(), auth.User{}); !errors.Is(err, errStep) {
			t.Errorf("failure at %s = %v, want the injected error", step, err)
		}
	}

	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.CreateFirstUser(context.Background(), auth.User{}); !errors.Is(err, errStep) {
		t.Errorf("failure to start = %v, want the injected error", err)
	}

	populated := &Store{queries: db.New(nil)}
	populated.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{users: 3}, nil }
	if err := populated.CreateFirstUser(context.Background(), auth.User{}); !errors.Is(err, auth.ErrSetupComplete) {
		t.Errorf("populated store = %v, want ErrSetupComplete", err)
	}
}

// TestCreateWorkspace_ReportsFailuresInsideTheTransaction verifies that a failure at any step of workspace creation
// is returned to the caller.
//
// Scenarios:
//   - the transaction cannot start
//   - inserting the workspace, inserting the owner membership and committing each fail
func TestCreateWorkspace_ReportsFailuresInsideTheTransaction(t *testing.T) {
	for _, step := range []string{"workspace", "membership", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.CreateWorkspace(context.Background(), workspace.Workspace{}); !errors.Is(err, errStep) {
			t.Errorf("failure at %s = %v, want the injected error", step, err)
		}
	}

	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.CreateWorkspace(context.Background(), workspace.Workspace{}); !errors.Is(err, errStep) {
		t.Errorf("failure to start = %v, want the injected error", err)
	}
}

// TestInvitationWrites_ReportFailuresInsideTheTransaction verifies that a failure at any step of creating or accepting an
// invitation is returned to the caller.
//
// Scenarios:
//   - creating fails when the transaction cannot start, when replacing the previous invitation fails, when inserting fails and when committing fails
//   - accepting fails when the transaction cannot start, when consuming, adding the membership, reading the workspace and committing fail
func TestInvitationWrites_ReportFailuresInsideTheTransaction(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	for _, step := range []string{"invite-delete", "invite-insert", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.Create(ctx, invite.Invitation{ID: id, WorkspaceID: id}, []byte("h")); !errors.Is(err, errStep) {
			t.Errorf("Create failure at %s = %v, want the injected error", step, err)
		}
	}
	for _, step := range []string{"invite-consume", "member-if-missing", "invite-workspace", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if _, err := store.Accept(ctx, id, id, time.Now()); !errors.Is(err, errStep) {
			t.Errorf("Accept failure at %s = %v, want the injected error", step, err)
		}
	}
	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.Create(ctx, invite.Invitation{}, nil); !errors.Is(err, errStep) {
		t.Errorf("Create failure to start = %v, want the injected error", err)
	}
	if _, err := failing.Accept(ctx, id, id, time.Now()); !errors.Is(err, errStep) {
		t.Errorf("Accept failure to start = %v, want the injected error", err)
	}
}

// TestCreateTeam_ReportsFailuresInsideTheTransaction verifies that a failure at any step of creating a team is returned to the caller.
//
// Scenarios:
//   - the transaction cannot start
//   - inserting the workspace, inserting the owner membership, inserting the first copy of the key and committing each fail
func TestCreateTeam_ReportsFailuresInsideTheTransaction(t *testing.T) {
	for _, step := range []string{"workspace", "membership", "team-key", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.CreateTeam(context.Background(), workspace.Workspace{}, nil); !errors.Is(err, errStep) {
			t.Errorf("failure at %s = %v, want the injected error", step, err)
		}
	}
	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.CreateTeam(context.Background(), workspace.Workspace{}, nil); !errors.Is(err, errStep) {
		t.Errorf("failure to start = %v, want the injected error", err)
	}
}

// TestKeyWrites_ReportFailuresInsideTheTransaction verifies that a failure at any step of removing a member, rotating a key or sealing
// secrets again is returned to the caller.
//
// Scenarios:
//   - removing fails when the transaction cannot start, when deleting the membership, deleting the copies, marking the rotation and committing fail
//   - rotating fails when the transaction cannot start, when locking, bumping the version, storing the copy and committing fail, and a version that is not the old one is a conflict
//   - sealing again fails when the transaction cannot start, when updating a secret, deleting stale copies and committing fail
func TestKeyWrites_ReportFailuresInsideTheTransaction(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	for _, step := range []string{"delete-member", "remove-grants", "rekey", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.RemoveMember(ctx, id, id); !errors.Is(err, errStep) {
			t.Errorf("RemoveMember failure at %s = %v, want the injected error", step, err)
		}
	}
	for _, step := range []string{"rotate-lock", "bump", "rotate-grant", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step, users: 1}, nil }
		if err := store.Rotate(ctx, id, 1, 2, id, nil); !errors.Is(err, errStep) {
			t.Errorf("Rotate failure at %s = %v, want the injected error", step, err)
		}
	}
	clash := &Store{queries: db.New(nil)}
	clash.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{users: 3}, nil }
	if err := clash.Rotate(ctx, id, 1, 2, id, nil); !errors.Is(err, wskey.ErrConflict) {
		t.Errorf("Rotate from another version = %v, want ErrConflict", err)
	}
	for _, step := range []string{"reseal", "stale-grants", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.Reseal(ctx, id, []wskey.Resealed{{}}); !errors.Is(err, errStep) {
			t.Errorf("Reseal failure at %s = %v, want the injected error", step, err)
		}
	}
	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.RemoveMember(ctx, id, id); !errors.Is(err, errStep) {
		t.Errorf("RemoveMember failure to start = %v, want the injected error", err)
	}
	if err := failing.Rotate(ctx, id, 1, 2, id, nil); !errors.Is(err, errStep) {
		t.Errorf("Rotate failure to start = %v, want the injected error", err)
	}
	if err := failing.Reseal(ctx, id, nil); !errors.Is(err, errStep) {
		t.Errorf("Reseal failure to start = %v, want the injected error", err)
	}
}

// TestFactorWrites_ReportFailuresInsideTheTransaction verifies that a failure at any step of a second factor change is returned.
//
// Scenarios:
//   - saving fails when the transaction cannot start, when storing the secret, deleting the recovery codes and committing fail
//   - confirming fails on the confirmation, on deleting the old codes, on inserting a new one and on committing
//   - replacing the recovery codes fails on deleting, on inserting and on committing
//   - deleting the factor fails on the secret, on the recovery codes and on committing
func TestFactorWrites_ReportFailuresInsideTheTransaction(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	codes := [][]byte{[]byte("a")}
	cases := map[string]struct {
		steps []string
		run   func(*Store) error
	}{
		"save":    {[]string{"totp-upsert", "recovery-delete", "commit"}, func(s *Store) error { return s.SaveFactor(ctx, id, nil) }},
		"confirm": {[]string{"totp-confirm", "recovery-delete", "recovery-insert", "commit"}, func(s *Store) error { return s.ConfirmFactor(ctx, id, 1, codes) }},
		"replace": {[]string{"recovery-delete", "recovery-insert", "commit"}, func(s *Store) error { return s.ReplaceRecovery(ctx, id, codes) }},
		"delete":  {[]string{"totp-delete", "recovery-delete", "commit"}, func(s *Store) error { return s.DeleteFactor(ctx, id) }},
	}
	for name, c := range cases {
		for _, step := range c.steps {
			store := &Store{queries: db.New(nil)}
			store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
			if err := c.run(store); !errors.Is(err, errStep) {
				t.Errorf("%s failure at %s = %v, want the injected error", name, step, err)
			}
		}
		failing := &Store{queries: db.New(nil)}
		failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
		if err := c.run(failing); !errors.Is(err, errStep) {
			t.Errorf("%s failure to start = %v, want the injected error", name, err)
		}
	}
}

// TestTransferOwnership_ReportsFailuresInsideTheTransaction verifies that a failure at any step of a transfer is
// returned to the caller.
//
// Scenarios:
//   - the transaction cannot start
//   - changing a role, changing the owner of the workspace and committing each fail
func TestTransferOwnership_ReportsFailuresInsideTheTransaction(t *testing.T) {
	ids := [3]uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for _, step := range []string{"transfer-role", "transfer-owner", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.TransferOwnership(context.Background(), ids[0], ids[1], ids[2]); !errors.Is(err, errStep) {
			t.Errorf("failure at %s = %v, want the injected error", step, err)
		}
	}
	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.TransferOwnership(context.Background(), ids[0], ids[1], ids[2]); !errors.Is(err, errStep) {
		t.Errorf("failure to start = %v, want the injected error", err)
	}
}

// TestSiteWrites_ReportFailuresInsideTheTransaction verifies that a failure at any step of creating or updating
// a site is returned to the caller.
//
// Scenarios:
//   - creating fails when the transaction cannot start, inserting the site, sealing a secret or committing fails
//   - updating fails when the transaction cannot start, updating the site, writing a secret, removing a secret
//     or committing fails
func TestSiteWrites_ReportFailuresInsideTheTransaction(t *testing.T) {
	ctx := context.Background()
	secrets := []site.Sealed{{Kind: site.SecretPassword}}
	remove := []site.SecretKind{site.SecretPassphrase}

	for _, step := range []string{"site", "secret", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.CreateSite(ctx, site.Site{}, secrets); !errors.Is(err, errStep) {
			t.Errorf("CreateSite failure at %s = %v, want the injected error", step, err)
		}
	}
	for _, step := range []string{"update-site", "secret", "remove-secret", "commit"} {
		store := &Store{queries: db.New(nil)}
		store.begin = func(context.Context) (pgx.Tx, error) { return fakeTx{failAt: step}, nil }
		if err := store.UpdateSite(ctx, site.Site{}, secrets, remove); !errors.Is(err, errStep) {
			t.Errorf("UpdateSite failure at %s = %v, want the injected error", step, err)
		}
	}

	failing := &Store{queries: db.New(nil)}
	failing.begin = func(context.Context) (pgx.Tx, error) { return nil, errStep }
	if err := failing.CreateSite(ctx, site.Site{}, nil); !errors.Is(err, errStep) {
		t.Errorf("CreateSite failure to start = %v, want the injected error", err)
	}
	if err := failing.UpdateSite(ctx, site.Site{}, nil, nil); !errors.Is(err, errStep) {
		t.Errorf("UpdateSite failure to start = %v, want the injected error", err)
	}
}

// secretsBroken is a database handle that fails every query touching the secrets table.
type secretsBroken struct{ db.DBTX }

// Query fails for secret queries and delegates the rest.
func (s secretsBroken) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "site_secrets") {
		return nil, errStep
	}
	return s.DBTX.Query(ctx, sql, args...)
}

// TestSiteReads_ReportSecretKindFailures verifies that reads fail when the secret kinds cannot be loaded.
//
// Scenarios:
//   - reading one site and listing sites return the error of the secrets query
func TestSiteReads_ReportSecretKindFailures(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	if err := Migrate(t.Context(), dbURL); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	at := time.Now().UTC()
	owner := auth.User{ID: uuid.New(), Email: "a@example.com", PasswordHash: "h", Role: auth.RoleUser, Locale: "en", Timezone: "UTC", CreatedAt: at}
	if err := store.CreateUser(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Workspace{ID: uuid.New(), Name: "Personal", Kind: workspace.KindPersonal, OwnerID: owner.ID, CreatedAt: at}
	if err := store.CreateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	created := site.Site{ID: uuid.New(), WorkspaceID: ws.ID, Name: "S", Protocol: site.SFTP, Host: "h", Port: 22, Username: "u", AuthMethod: site.AuthPassword, ListTimezone: "UTC", CreatedAt: at, UpdatedAt: at}
	if err := store.CreateSite(t.Context(), created, nil); err != nil {
		t.Fatal(err)
	}
	store.queries = db.New(secretsBroken{store.pool})

	if _, err := store.Site(t.Context(), ws.ID, created.ID); !errors.Is(err, errStep) {
		t.Errorf("Site() = %v, want the secrets error", err)
	}
	if _, err := store.Sites(t.Context(), ws.ID); !errors.Is(err, errStep) {
		t.Errorf("Sites() = %v, want the secrets error", err)
	}
}

// hooked is a database handle that fails or short-circuits chosen statements.
type hooked struct {
	db.DBTX
	failContents bool
	failDelete   bool
	zeroDelete   bool
}

// QueryRow fails the contents check when configured to.
func (h hooked) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if h.failContents && strings.Contains(sql, "has_contents") {
		return scanRow{err: errStep}
	}
	return h.DBTX.QueryRow(ctx, sql, args...)
}

// Exec fails the deletion, or reports that it removed nothing, when configured to.
func (h hooked) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "DELETE FROM site_folders") {
		if h.failDelete {
			return pgconn.CommandTag{}, errStep
		}
		if h.zeroDelete {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
	}
	return h.DBTX.Exec(ctx, sql, args...)
}

// TestDeleteCollection_ReportsFailuresAfterTheLookup verifies the steps of deleting a collection that follow finding it.
//
// Scenarios:
//   - a failing contents check, a failing deletion and a deletion that removes nothing (the collection vanished
//     meanwhile) are each reported
func TestDeleteCollection_ReportsFailuresAfterTheLookup(t *testing.T) {
	dbURL := testutil.NewDatabase(t)
	if err := Migrate(t.Context(), dbURL); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	at := time.Now().UTC()
	owner := auth.User{ID: uuid.New(), Email: "a@example.com", PasswordHash: "h", Role: auth.RoleUser, Locale: "en", Timezone: "UTC", CreatedAt: at}
	if err := store.CreateUser(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Workspace{ID: uuid.New(), Name: "Personal", Kind: workspace.KindPersonal, OwnerID: owner.ID, CreatedAt: at}
	if err := store.CreateWorkspace(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	c := site.Collection{ID: uuid.New(), WorkspaceID: ws.ID, Name: "C"}
	if err := store.CreateCollection(t.Context(), c); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		db   hooked
		want error
	}{
		{"contents check", hooked{failContents: true}, errStep},
		{"deletion", hooked{failDelete: true}, errStep},
		{"vanished", hooked{zeroDelete: true}, site.ErrCollectionNotFound},
	}
	for _, tt := range tests {
		tt.db.DBTX = store.pool
		store.queries = db.New(tt.db)
		if err := store.DeleteCollection(t.Context(), ws.ID, c.ID); !errors.Is(err, tt.want) {
			t.Errorf("%s: DeleteCollection() = %v; want %v", tt.name, err, tt.want)
		}
	}
}
