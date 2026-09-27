package storetest

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/admin"
	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/mfa"
	"github.com/fprambs/sentinel/internal/core/passkey"
)

// AdminStore is the persistence an implementation offers to the admin package, plus what the suite needs to set up.
type AdminStore interface {
	admin.Store
	audit.Store
	mfa.Store
	passkey.Store
	auth.UserStore
	auth.SessionStore
}

// AdminFactory returns a new, empty store for one test.
type AdminFactory func(t *testing.T) AdminStore

// RunAdmin executes the administration store suite against stores built by newStore.
//
// Scenarios:
//   - accounts come back oldest first with their role, whether they are disabled and whether they have an authenticator app or a passkey
//   - one account is found by its identifier and an unknown one is ErrNotFound
//   - disabling and enabling change the state, changing the role works both ways, and unknown accounts are ErrNotFound
//   - the active administrators are counted without the disabled ones
//   - resetting the second factor removes the app, the passkeys and the recovery codes of that account and leaves the others alone
//   - the sessions of an account are removed together
//   - the allowed networks are replaced as a whole, read back in text order, and replacing them with nothing empties the list
//   - the audit trail of the instance holds only the events that belong to no workspace and start with `admin.`, newest first, filtered and paged
func RunAdmin(t *testing.T, newStore AdminFactory) {
	t.Helper()
	ctx := t.Context()
	store := newStore(t)
	root, bob, carol := newUser("root@example.com"), newUser("bob@example.com"), newUser("carol@example.com")
	root.Role = auth.RoleAdmin
	for i, u := range []*auth.User{&root, &bob, &carol} {
		u.CreatedAt = now.Add(time.Duration(i) * time.Minute)
		if err := store.CreateUser(ctx, *u); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveFactor(ctx, bob.ID, []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if accounts, _ := store.Accounts(ctx); accounts[1].SecondFactor {
		t.Fatalf("an unconfirmed authenticator app counts as a second factor: %+v", accounts)
	}
	if err := store.ConfirmFactor(ctx, bob.ID, 5, [][]byte{bytes.Repeat([]byte{1}, 32)}); err != nil {
		t.Fatal(err)
	}
	key := passkey.Passkey{ID: uuid.New(), Name: "Laptop", CreatedAt: now, Credential: webauthn.Credential{ID: bytes.Repeat([]byte{7}, 32), PublicKey: bytes.Repeat([]byte{8}, 65)}}
	if err := store.AddPasskey(ctx, carol.ID, key); err != nil {
		t.Fatal(err)
	}

	accounts, err := store.Accounts(ctx)
	if err != nil || len(accounts) != 3 {
		t.Fatalf("Accounts() = %+v, %v; want three", accounts, err)
	}
	if accounts[0].Email != root.Email || accounts[0].Role != auth.RoleAdmin || accounts[0].SecondFactor || accounts[0].Disabled || !accounts[0].CreatedAt.Equal(root.CreatedAt) || accounts[0].CreatedAt.Location() != time.UTC {
		t.Fatalf("the first account = %+v; want the administrator without a second factor", accounts[0])
	}
	if accounts[1].Email != bob.Email || !accounts[1].SecondFactor || accounts[2].Email != carol.Email || !accounts[2].SecondFactor {
		t.Fatalf("Accounts() = %+v; want the app of bob and the passkey of carol to count", accounts)
	}
	if got, err := store.Account(ctx, bob.ID); err != nil || got.Email != bob.Email {
		t.Fatalf("Account() = %+v, %v", got, err)
	}
	if _, err := store.Account(ctx, uuid.New()); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("Account(unknown) = %v; want ErrNotFound", err)
	}

	if n, _ := store.ActiveAdmins(ctx); n != 1 {
		t.Fatalf("ActiveAdmins() = %d; want 1", n)
	}
	if err := store.SetDisabled(ctx, bob.ID, true); err != nil {
		t.Fatalf("SetDisabled() = %v", err)
	}
	if got, _ := store.Account(ctx, bob.ID); !got.Disabled {
		t.Fatal("bob is not disabled")
	}
	if err := store.SetDisabled(ctx, bob.ID, false); err != nil {
		t.Fatalf("SetDisabled(false) = %v", err)
	}
	if err := store.SetAccountRole(ctx, bob.ID, auth.RoleAdmin); err != nil {
		t.Fatalf("SetAccountRole() = %v", err)
	}
	if n, _ := store.ActiveAdmins(ctx); n != 2 {
		t.Fatalf("ActiveAdmins() = %d; want 2", n)
	}
	if err := store.SetDisabled(ctx, bob.ID, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.ActiveAdmins(ctx); n != 1 {
		t.Fatalf("ActiveAdmins() = %d; want the disabled one left out", n)
	}
	if err := store.SetAccountRole(ctx, bob.ID, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Account(ctx, bob.ID); got.Role != auth.RoleUser {
		t.Fatalf("bob = %+v; want a regular account", got)
	}
	if err := store.SetDisabled(ctx, uuid.New(), true); !errors.Is(err, admin.ErrNotFound) {
		t.Errorf("SetDisabled(unknown) = %v; want ErrNotFound", err)
	}
	if err := store.SetAccountRole(ctx, uuid.New(), auth.RoleUser); !errors.Is(err, admin.ErrNotFound) {
		t.Errorf("SetAccountRole(unknown) = %v; want ErrNotFound", err)
	}

	if err := store.ResetSecondFactor(ctx, bob.ID); err != nil {
		t.Fatalf("ResetSecondFactor() = %v", err)
	}
	if _, err := store.Factor(ctx, bob.ID); !errors.Is(err, mfa.ErrNotEnrolled) {
		t.Errorf("Factor() after the reset = %v; want ErrNotEnrolled", err)
	}
	if n, _ := store.RecoveryLeft(ctx, bob.ID); n != 0 {
		t.Errorf("RecoveryLeft() after the reset = %d; want 0", n)
	}
	if list, _ := store.Passkeys(ctx, carol.ID); len(list) != 1 {
		t.Fatalf("the passkeys of carol = %+v; want them untouched by the reset of bob", list)
	}
	if err := store.ResetSecondFactor(ctx, carol.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := store.Passkeys(ctx, carol.ID); len(list) != 0 {
		t.Fatalf("the passkeys of carol = %+v; want none after the reset", list)
	}

	for _, hash := range []string{"a", "b"} {
		if err := store.CreateSession(ctx, newSession(carol.ID, hash)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := store.DeleteUserSessions(ctx, carol.ID); err != nil || n != 2 {
		t.Fatalf("DeleteUserSessions() = %d, %v; want 2", n, err)
	}

	if got, err := store.Networks(ctx); err != nil || len(got) != 0 {
		t.Fatalf("Networks() = %v, %v; want none at first", got, err)
	}
	first := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24"), netip.MustParsePrefix("10.0.0.0/8")}
	if err := store.ReplaceNetworks(ctx, first, root.ID, now); err != nil {
		t.Fatalf("ReplaceNetworks() = %v", err)
	}
	if got, _ := store.Networks(ctx); len(got) != 2 || got[0].String() != "10.0.0.0/8" || got[1].String() != "192.168.1.0/24" {
		t.Fatalf("Networks() = %v; want both, in text order", got)
	}
	if err := store.ReplaceNetworks(ctx, []netip.Prefix{netip.MustParsePrefix("fd00::/8")}, uuid.Nil, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Networks(ctx); len(got) != 1 || got[0].String() != "fd00::/8" {
		t.Fatalf("Networks() = %v; want only the new one", got)
	}
	if err := store.ReplaceNetworks(ctx, nil, root.ID, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Networks(ctx); len(got) != 0 {
		t.Fatalf("Networks() = %v; want none", got)
	}

	events := []audit.Event{
		{ActorID: root.ID, Action: audit.AdminUserDisabled, Target: "bob@example.com", IP: "203.0.113.9"},
		{ActorID: root.ID, Action: audit.AdminNetworksChanged, Meta: map[string]string{"count": "2"}},
		{Action: audit.AdminRoleChanged, Target: "carol@example.com"},
		{Action: audit.MemberLeft, Target: "not-instance"},
	}
	for i, e := range events {
		e.At = now.Add(time.Duration(i) * time.Minute)
		if err := store.InsertAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	trail, err := store.AuditInstance(ctx, audit.Filter{}, 100)
	if err != nil || len(trail) != 3 || trail[0].Action != audit.AdminRoleChanged || trail[2].Action != audit.AdminUserDisabled {
		t.Fatalf("AuditInstance() = %+v, %v; want the three admin events, newest first", trail, err)
	}
	if trail[2].ActorEmail != root.Email || trail[2].IP != "203.0.113.9" || trail[2].WorkspaceID != uuid.Nil || trail[1].Meta["count"] != "2" || trail[0].ActorEmail != "" {
		t.Fatalf("AuditInstance() = %+v; want the actor, the address and the metadata intact", trail)
	}
	if one, _ := store.AuditInstance(ctx, audit.Filter{Action: "admin.user_"}, 100); len(one) != 1 {
		t.Fatalf("filtered = %+v; want 1", one)
	}
	page, _ := store.AuditInstance(ctx, audit.Filter{}, 2)
	next, _ := store.AuditInstance(ctx, audit.Filter{Before: page[1].ID}, 100)
	if len(page) != 2 || len(next) != 1 || next[0].ID >= page[1].ID {
		t.Fatalf("pages = %+v then %+v; want 2 and 1", page, next)
	}
}
