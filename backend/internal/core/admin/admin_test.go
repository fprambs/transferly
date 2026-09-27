package admin_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/admin"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/limits"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errBoom is the failure injected by the fakes.
var errBoom = errors.New("boom")

// fakeStore keeps accounts and networks in memory.
type fakeStore struct {
	accounts []admin.Account
	networks []netip.Prefix
	by       uuid.UUID
	at       time.Time
	sessions int
	reset    []uuid.UUID
	fail     map[string]error
}

// failure returns the error injected for an operation.
func (f *fakeStore) failure(op string) error { return f.fail[op] }

// Accounts returns the accounts.
func (f *fakeStore) Accounts(context.Context) ([]admin.Account, error) {
	return f.accounts, f.failure("Accounts")
}

// Account returns one account.
func (f *fakeStore) Account(_ context.Context, id uuid.UUID) (admin.Account, error) {
	if err := f.failure("Account"); err != nil {
		return admin.Account{}, err
	}
	for _, a := range f.accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return admin.Account{}, admin.ErrNotFound
}

// SetDisabled changes the state of an account.
func (f *fakeStore) SetDisabled(_ context.Context, id uuid.UUID, disabled bool) error {
	for i := range f.accounts {
		if f.accounts[i].ID == id {
			f.accounts[i].Disabled = disabled
		}
	}
	return f.failure("SetDisabled")
}

// SetAccountRole changes the role of an account.
func (f *fakeStore) SetAccountRole(_ context.Context, id uuid.UUID, role auth.Role) error {
	for i := range f.accounts {
		if f.accounts[i].ID == id {
			f.accounts[i].Role = role
		}
	}
	return f.failure("SetAccountRole")
}

// ActiveAdmins counts the administrators that are not disabled.
func (f *fakeStore) ActiveAdmins(context.Context) (int, error) {
	n := 0
	for _, a := range f.accounts {
		if a.Role == auth.RoleAdmin && !a.Disabled {
			n++
		}
	}
	return n, f.failure("ActiveAdmins")
}

// ResetSecondFactor records the reset.
func (f *fakeStore) ResetSecondFactor(_ context.Context, id uuid.UUID) error {
	f.reset = append(f.reset, id)
	return f.failure("ResetSecondFactor")
}

// DeleteUserSessions counts the calls.
func (f *fakeStore) DeleteUserSessions(context.Context, uuid.UUID) (int, error) {
	f.sessions++
	return 1, f.failure("DeleteUserSessions")
}

// Networks returns the stored networks.
func (f *fakeStore) Networks(context.Context) ([]netip.Prefix, error) {
	return f.networks, f.failure("Networks")
}

// ReplaceNetworks stores the networks.
func (f *fakeStore) ReplaceNetworks(_ context.Context, networks []netip.Prefix, by uuid.UUID, at time.Time) error {
	if err := f.failure("ReplaceNetworks"); err != nil {
		return err
	}
	f.networks, f.by, f.at = networks, by, at
	return nil
}

// fakePolicy is a policy that only remembers the extra networks.
type fakePolicy struct{ fixed, extra []netip.Prefix }

// Fixed returns the fixed networks.
func (p *fakePolicy) Fixed() []netip.Prefix { return p.fixed }

// Extra returns the extra networks.
func (p *fakePolicy) Extra() []netip.Prefix { return p.extra }

// SetExtra replaces the extra networks.
func (p *fakePolicy) SetExtra(extra []netip.Prefix) { p.extra = extra }

// fakeLimiter keeps the limits in memory.
type fakeLimiter struct {
	current, defaults limits.Limits
	fail              error
}

// Current returns the limits in force.
func (f *fakeLimiter) Current() limits.Limits { return f.current }

// Defaults returns the starting limits.
func (f *fakeLimiter) Defaults() limits.Limits { return f.defaults }

// Set puts limits in force.
func (f *fakeLimiter) Set(_ context.Context, _ uuid.UUID, l limits.Limits) (limits.Limits, error) {
	if f.fail != nil {
		return limits.Limits{}, f.fail
	}
	f.current = l
	return l, nil
}

// setup returns a service with an administrator, a second administrator and two regular accounts.
func setup() (*admin.Service, *fakeStore, *fakePolicy, map[string]auth.User) {
	users := map[string]auth.User{
		"root":  {ID: uuid.New(), Email: "root@example.com", Role: auth.RoleAdmin},
		"other": {ID: uuid.New(), Email: "other@example.com", Role: auth.RoleAdmin},
		"bob":   {ID: uuid.New(), Email: "bob@example.com", Role: auth.RoleUser},
		"carol": {ID: uuid.New(), Email: "carol@example.com", Role: auth.RoleUser},
	}
	store := &fakeStore{fail: map[string]error{}}
	for _, name := range []string{"root", "other", "bob", "carol"} {
		u := users[name]
		store.accounts = append(store.accounts, admin.Account{ID: u.ID, Email: u.Email, Role: u.Role, SecondFactor: name == "bob"})
	}
	policy := &fakePolicy{fixed: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}}
	return admin.NewService(store, policy, &fakeLimiter{defaults: limits.Limits{TransfersGlobal: 32}, current: limits.Limits{TransfersGlobal: 32}}, testutil.NewFakeClock(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))), store, policy, users
}

// TestService_RefusesWhoIsNotAnAdministrator verifies that every operation checks the acting account itself.
//
// Scenarios:
//   - a regular account and a disabled administrator get ErrForbidden from every operation
//   - nothing was read, changed or stored by those calls
func TestService_RefusesWhoIsNotAnAdministrator(t *testing.T) {
	svc, store, policy, users := setup()
	ctx := context.Background()
	for _, actor := range []auth.User{users["bob"], {ID: uuid.New(), Role: auth.RoleAdmin, Disabled: true}} {
		calls := map[string]error{
			"Users":             second(svc.Users(ctx, actor)),
			"SetDisabled":       second(svc.SetDisabled(ctx, actor, users["carol"].ID, true)),
			"SetRole":           second(svc.SetRole(ctx, actor, users["carol"].ID, auth.RoleAdmin)),
			"ResetSecondFactor": second(svc.ResetSecondFactor(ctx, actor, users["carol"].ID)),
			"Networks":          second(svc.Networks(ctx, actor)),
			"SetNetworks":       second(svc.SetNetworks(ctx, actor, []string{"10.0.0.0/8"})),
			"Limits":            second(svc.Limits(actor)),
			"SetLimits":         second(svc.SetLimits(ctx, actor, limits.Limits{})),
		}
		for name, err := range calls {
			if !errors.Is(err, admin.ErrForbidden) {
				t.Errorf("%s by %+v = %v; want ErrForbidden", name, actor, err)
			}
		}
	}
	if len(store.reset) != 0 || store.networks != nil || policy.extra != nil || store.accounts[3].Disabled || store.accounts[3].Role != auth.RoleUser {
		t.Errorf("a refused call changed something: %+v", store)
	}
}

// TestService_Users verifies listing the accounts.
//
// Scenarios:
//   - an administrator gets every account, and a failing store is returned
func TestService_Users(t *testing.T) {
	svc, store, _, users := setup()
	got, err := svc.Users(context.Background(), users["root"])
	if err != nil || len(got) != 4 {
		t.Fatalf("Users() = %+v, %v; want four accounts", got, err)
	}
	store.fail["Accounts"] = errBoom
	if _, err := svc.Users(context.Background(), users["root"]); !errors.Is(err, errBoom) {
		t.Errorf("Users() failure = %v", err)
	}
}

// TestService_SetDisabled verifies disabling and enabling accounts.
//
// Scenarios:
//   - disabling another account marks it and ends its sessions, and enabling it again marks it without touching sessions
//   - nobody disables their own account and unknown accounts are not found
//   - one of two administrators can be disabled, and one who is already disabled again, but the last active one stays
//   - failures of the store are returned at each step and a session that cannot be ended is reported
func TestService_SetDisabled(t *testing.T) {
	svc, store, _, users := setup()
	ctx := context.Background()
	root := users["root"]

	got, err := svc.SetDisabled(ctx, root, users["bob"].ID, true)
	if err != nil || !got.Disabled || got.Email != "bob@example.com" || !store.accounts[2].Disabled || store.sessions != 1 {
		t.Fatalf("SetDisabled(true) = %+v, %v; sessions %d", got, err, store.sessions)
	}
	got, err = svc.SetDisabled(ctx, root, users["bob"].ID, false)
	if err != nil || got.Disabled || store.accounts[2].Disabled || store.sessions != 1 {
		t.Fatalf("SetDisabled(false) = %+v, %v; sessions %d", got, err, store.sessions)
	}
	if _, err := svc.SetDisabled(ctx, root, root.ID, true); !errors.Is(err, admin.ErrSelf) {
		t.Errorf("disabling yourself = %v; want ErrSelf", err)
	}
	if _, err := svc.SetDisabled(ctx, root, uuid.New(), true); !errors.Is(err, admin.ErrNotFound) {
		t.Errorf("disabling an unknown account = %v; want ErrNotFound", err)
	}

	if _, err := svc.SetDisabled(ctx, root, users["other"].ID, true); err != nil {
		t.Fatalf("disabling one of two administrators = %v", err)
	}
	if _, err := svc.SetDisabled(ctx, root, users["other"].ID, true); err != nil {
		t.Errorf("disabling an administrator who is already disabled = %v; want it allowed, they are not the last active one", err)
	}
	third := auth.User{ID: uuid.New(), Email: "third@example.com", Role: auth.RoleAdmin}
	if _, err := svc.SetDisabled(ctx, third, root.ID, true); !errors.Is(err, admin.ErrLastAdmin) {
		t.Errorf("disabling the last active administrator = %v; want ErrLastAdmin", err)
	}
	if store.accounts[0].Disabled {
		t.Error("the last administrator was disabled anyway")
	}

	for _, op := range []string{"Account", "ActiveAdmins", "SetDisabled", "DeleteUserSessions"} {
		s, st, _, u := setup()
		st.accounts[1].Role = auth.RoleAdmin
		st.fail[op] = errBoom
		if _, err := s.SetDisabled(ctx, u["root"], u["other"].ID, true); !errors.Is(err, errBoom) {
			t.Errorf("SetDisabled() with %s failing = %v; want the failure", op, err)
		}
	}
}

// TestService_SetRole verifies changing who is an administrator.
//
// Scenarios:
//   - a regular account becomes an administrator and back, and an unknown role is refused
//   - nobody changes their own role and unknown accounts are not found
//   - one of two administrators can be demoted, a disabled one can be demoted, and the last active one stays
//   - failures of the store are returned
func TestService_SetRole(t *testing.T) {
	svc, store, _, users := setup()
	ctx := context.Background()
	root := users["root"]

	got, err := svc.SetRole(ctx, root, users["bob"].ID, auth.RoleAdmin)
	if err != nil || got.Role != auth.RoleAdmin || store.accounts[2].Role != auth.RoleAdmin {
		t.Fatalf("SetRole(admin) = %+v, %v", got, err)
	}
	if got, err = svc.SetRole(ctx, root, users["bob"].ID, auth.RoleUser); err != nil || got.Role != auth.RoleUser {
		t.Fatalf("SetRole(user) = %+v, %v", got, err)
	}
	if _, err := svc.SetRole(ctx, root, users["bob"].ID, "owner"); !errors.Is(err, admin.ErrInvalidRole) {
		t.Errorf("SetRole(owner) = %v; want ErrInvalidRole", err)
	}
	if _, err := svc.SetRole(ctx, root, root.ID, auth.RoleUser); !errors.Is(err, admin.ErrSelf) {
		t.Errorf("SetRole(yourself) = %v; want ErrSelf", err)
	}
	if _, err := svc.SetRole(ctx, root, uuid.New(), auth.RoleUser); !errors.Is(err, admin.ErrNotFound) {
		t.Errorf("SetRole(unknown) = %v; want ErrNotFound", err)
	}
	if _, err := svc.SetRole(ctx, root, users["other"].ID, auth.RoleUser); err != nil {
		t.Fatalf("demoting one of two administrators = %v", err)
	}
	third := auth.User{ID: uuid.New(), Email: "third@example.com", Role: auth.RoleAdmin}
	if _, err := svc.SetRole(ctx, third, root.ID, auth.RoleUser); !errors.Is(err, admin.ErrLastAdmin) {
		t.Errorf("demoting the last active administrator = %v; want ErrLastAdmin", err)
	}
	if store.accounts[0].Role != auth.RoleAdmin {
		t.Error("the last administrator was demoted anyway")
	}
	store.accounts[1].Role, store.accounts[1].Disabled = auth.RoleAdmin, true
	if _, err := svc.SetRole(ctx, third, users["other"].ID, auth.RoleUser); err != nil {
		t.Errorf("demoting a disabled administrator = %v; want it allowed, they are not the last active one", err)
	}

	for _, op := range []string{"Account", "ActiveAdmins", "SetAccountRole"} {
		s, st, _, u := setup()
		st.fail[op] = errBoom
		if _, err := s.SetRole(ctx, u["root"], u["other"].ID, auth.RoleUser); !errors.Is(err, errBoom) {
			t.Errorf("SetRole() with %s failing = %v; want the failure", op, err)
		}
	}
}

// TestService_ResetSecondFactor verifies removing the second factor of another account.
//
// Scenarios:
//   - the factor of another account is reset and the account is returned without it
//   - nobody resets their own, unknown accounts are not found, and a failing store is returned
func TestService_ResetSecondFactor(t *testing.T) {
	svc, store, _, users := setup()
	ctx := context.Background()

	got, err := svc.ResetSecondFactor(ctx, users["root"], users["bob"].ID)
	if err != nil || got.SecondFactor || len(store.reset) != 1 || store.reset[0] != users["bob"].ID {
		t.Fatalf("ResetSecondFactor() = %+v, %v; reset %v", got, err, store.reset)
	}
	if _, err := svc.ResetSecondFactor(ctx, users["root"], users["root"].ID); !errors.Is(err, admin.ErrSelf) {
		t.Errorf("resetting yourself = %v; want ErrSelf", err)
	}
	if _, err := svc.ResetSecondFactor(ctx, users["root"], uuid.New()); !errors.Is(err, admin.ErrNotFound) {
		t.Errorf("resetting an unknown account = %v; want ErrNotFound", err)
	}
	store.fail["ResetSecondFactor"] = errBoom
	if _, err := svc.ResetSecondFactor(ctx, users["root"], users["bob"].ID); !errors.Is(err, errBoom) {
		t.Errorf("ResetSecondFactor() failure = %v", err)
	}
}

// TestParseNetworks verifies reading networks written as text.
//
// Scenarios:
//   - networks and single addresses are accepted, masked, without repeats and sorted by address, IPv4-mapped addresses as IPv4
//   - entries that are not networks, networks wider than /8 (IPv4) or /7 (IPv6), and every range that is never allowed are refused, including
//     wider networks that contain one
//   - more than the maximum is refused
func TestParseNetworks(t *testing.T) {
	got, err := admin.ParseNetworks([]string{" 192.168.1.77/24 ", "10.0.0.1", "192.168.1.0/24", "::ffff:10.1.1.1", "fd12::/16", "8.0.0.0/8"})
	want := []string{"8.0.0.0/8", "10.0.0.1/32", "10.1.1.1/32", "192.168.1.0/24", "fd12::/16"}
	if err != nil || len(got) != len(want) {
		t.Fatalf("ParseNetworks() = %v, %v; want %v", got, err, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("ParseNetworks()[%d] = %s; want %s", i, got[i], want[i])
		}
	}
	for _, bad := range []string{
		"", "nope", "10.0.0.0/33", "0.0.0.0/0", "10.0.0.0/7", "::/0", "fc00::/6", "0.0.0.5", "169.254.169.254", "169.254.0.0/16", "169.254.10.0/24",
		"127.0.0.1", "127.0.0.0/8", "::1", "224.0.0.1", "239.0.0.0/8", "240.0.0.0/8", "255.255.255.255", "::", "fe80::1", "fe80::/10", "ff02::1", "fe80::1%eth0",
	} {
		if _, err := admin.ParseNetworks([]string{bad}); !errors.Is(err, admin.ErrInvalidNetwork) {
			t.Errorf("ParseNetworks(%q) = %v; want ErrInvalidNetwork", bad, err)
		}
	}
	many := make([]string, admin.MaxNetworks+1)
	for i := range many {
		many[i] = "10.0.0.1"
	}
	if _, err := admin.ParseNetworks(many); !errors.Is(err, admin.ErrTooManyNetworks) {
		t.Errorf("ParseNetworks(101 entries) = %v; want ErrTooManyNetworks", err)
	}
	if got, err := admin.ParseNetworks(many[:admin.MaxNetworks]); err != nil || len(got) != 1 {
		t.Errorf("ParseNetworks(100 entries) = %v, %v; want the one network they repeat", got, err)
	}
}

// TestService_Networks verifies reading and replacing the allowed networks.
//
// Scenarios:
//   - replacing stores the parsed networks with the actor and the time, applies them to the policy and returns both lists
//   - reading returns the fixed networks of the policy and the custom ones of the store, sorted, and a failing store is returned
//   - a refused list changes nothing, and a failing store leaves the policy as it was
//   - loading at start applies the stored networks to the policy, and a failing store is reported
func TestService_Networks(t *testing.T) {
	svc, store, policy, users := setup()
	ctx := context.Background()
	root := users["root"]

	got, err := svc.SetNetworks(ctx, root, []string{"192.168.1.0/24", "10.20.0.0/16"})
	if err != nil || len(got.Custom) != 2 || len(got.Fixed) != 1 || len(policy.extra) != 2 || store.by != root.ID || !store.at.Equal(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("SetNetworks() = %+v, %v; policy %v, by %v at %v", got, err, policy.extra, store.by, store.at)
	}
	read, err := svc.Networks(ctx, root)
	if err != nil || len(read.Custom) != 2 || read.Fixed[0].String() != "172.16.0.0/12" {
		t.Fatalf("Networks() = %+v, %v", read, err)
	}
	if _, err := svc.SetNetworks(ctx, root, []string{"0.0.0.0/0"}); !errors.Is(err, admin.ErrInvalidNetwork) || len(policy.extra) != 2 {
		t.Errorf("SetNetworks(everything) = %v, policy %v; want a refusal and no change", err, policy.extra)
	}
	store.fail["Networks"] = errBoom
	if _, err := svc.Networks(ctx, root); !errors.Is(err, errBoom) {
		t.Errorf("Networks() failure = %v", err)
	}
	delete(store.fail, "Networks")
	store.fail["ReplaceNetworks"] = errBoom
	if _, err := svc.SetNetworks(ctx, root, nil); !errors.Is(err, errBoom) || len(policy.extra) != 2 {
		t.Errorf("SetNetworks() failure = %v, policy %v; want the failure and no change", err, policy.extra)
	}

	store.fail = map[string]error{}
	store.networks = []netip.Prefix{netip.MustParsePrefix("fd00::/8"), netip.MustParsePrefix("10.0.0.0/16"), netip.MustParsePrefix("10.0.0.0/8")}
	order, _ := svc.Networks(ctx, root)
	if len(order.Custom) != 3 || order.Custom[0].String() != "10.0.0.0/8" || order.Custom[1].String() != "10.0.0.0/16" || order.Custom[2].String() != "fd00::/8" {
		t.Errorf("Networks() = %v; want them sorted by address, then by length, IPv4 first", order.Custom)
	}

	fresh, freshStore, freshPolicy, _ := setup()
	freshStore.networks = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	if err := fresh.Load(ctx); err != nil || len(freshPolicy.extra) != 1 {
		t.Fatalf("Load() = %v, policy %v", err, freshPolicy.extra)
	}
	freshStore.fail["Networks"] = errBoom
	if err := fresh.Load(ctx); !errors.Is(err, errBoom) {
		t.Errorf("Load() failure = %v", err)
	}
}

// second returns the error of a call whose first result does not matter.
func second[T any](_ T, err error) error { return err }

// TestService_Limits verifies reading and replacing the caps and quotas.
//
// Scenarios:
//   - an administrator reads the limits in force next to the starting ones
//   - replacing them returns the new ones next to the starting ones
//   - a refusal of the limiter is returned and changes nothing
func TestService_Limits(t *testing.T) {
	ctx := context.Background()
	limiter := &fakeLimiter{defaults: limits.Limits{TransfersGlobal: 32}, current: limits.Limits{TransfersGlobal: 32}}
	svc := admin.NewService(&fakeStore{}, &fakePolicy{}, limiter, testutil.NewFakeClock(time.Now()))
	root := auth.User{ID: uuid.New(), Role: auth.RoleAdmin}

	view, err := svc.Limits(root)
	if err != nil || view.Current.TransfersGlobal != 32 || view.Defaults.TransfersGlobal != 32 {
		t.Fatalf("Limits() = %+v, %v", view, err)
	}
	view, err = svc.SetLimits(ctx, root, limits.Limits{TransfersGlobal: 4})
	if err != nil || view.Current.TransfersGlobal != 4 || view.Defaults.TransfersGlobal != 32 {
		t.Fatalf("SetLimits() = %+v, %v", view, err)
	}
	limiter.fail = limits.ErrInvalid
	if _, err := svc.SetLimits(ctx, root, limits.Limits{TransfersGlobal: 9}); !errors.Is(err, limits.ErrInvalid) || limiter.current.TransfersGlobal != 4 {
		t.Errorf("SetLimits() refused = %v, current %+v", err, limiter.current)
	}
}
