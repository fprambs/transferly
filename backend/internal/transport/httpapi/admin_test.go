package httpapi_test

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// adminEnv is an instance with an administrator, a regular account with a second factor and another regular account.
type adminEnv struct {
	*mfaUser
	root   *mfaUser
	other  *mfaUser
	secret []byte
}

// newAdminEnv makes the accounts. The account of mfaUser is the regular one whose second factor is enrolled.
func newAdminEnv(t *testing.T, tweaks ...func(*httpapi.Deps)) *adminEnv {
	t.Helper()
	user := newMFAUser(t, tweaks...)
	secret, _ := user.enroll(t)
	register := func(email string, role string) *mfaUser {
		cookie, u := user.a.signIn(email)
		if _, err := user.a.store.DB().Exec(t.Context(), "UPDATE users SET role = $2 WHERE id = $1", u.ID, role); err != nil {
			t.Fatal(err)
		}
		return &mfaUser{a: user.a, email: email, id: u.ID, cookie: cookie, csrf: csrf(user.a, cookie), now: user.now}
	}
	return &adminEnv{mfaUser: user, root: register("root@example.com", "admin"), other: register("other@example.com", "user"), secret: secret}
}

// TestAdmin_Refusals verifies who may use the administration endpoints.
//
// Scenarios:
//   - anonymous callers get 401 on every endpoint
//   - a regular account gets 403 forbidden on every endpoint, and nothing changes
func TestAdmin_Refusals(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	id := e.id.String()
	calls := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/admin/users", nil},
		{http.MethodPatch, "/api/v1/admin/users/" + id, map[string]bool{"disabled": true}},
		{http.MethodPost, "/api/v1/admin/users/" + id + "/second-factor/reset", nil},
		{http.MethodGet, "/api/v1/admin/networks", nil},
		{http.MethodPut, "/api/v1/admin/networks", map[string][]string{"networks": {"10.0.0.0/8"}}},
		{http.MethodGet, "/api/v1/admin/audit", nil},
	}
	for _, c := range calls {
		if r := e.a.do(request{method: c.method, path: c.path, body: c.body}); r.Status != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d; want 401", c.method, c.path, r.Status)
		}
		r := e.other.call(c.method, c.path, c.body)
		if r.Status != http.StatusForbidden || !strings.Contains(string(r.Body), `"forbidden"`) {
			t.Errorf("regular %s %s = %d %s; want 403 forbidden", c.method, c.path, r.Status, r.Body)
		}
	}
	if len(e.a.policy.Extra()) != 0 {
		t.Errorf("a refused call changed the networks: %v", e.a.policy.Extra())
	}
}

// listUsers reads the accounts as the administrator and returns email to account.
func (e *adminEnv) listUsers(t *testing.T) map[string]httpapi.AdminUser {
	t.Helper()
	r := e.root.call(http.MethodGet, "/api/v1/admin/users", nil)
	var list []httpapi.AdminUser
	mustDecode(t, r.Body, &list)
	if r.Status != http.StatusOK {
		t.Fatalf("list users = %d %s", r.Status, r.Body)
	}
	out := map[string]httpapi.AdminUser{}
	for _, u := range list {
		out[u.Email] = u
	}
	return out
}

// TestAdmin_Accounts verifies listing and changing accounts.
//
// Scenarios:
//   - the list shows every account with its role, whether it is disabled and whether it has a second factor
//   - disabling an account ends its session at once and refuses its sign-in, and enabling lets it back in
//   - changing the role works both ways, and a request that changes nothing, an unknown role and an unknown account are refused
//   - an administrator cannot change their own account
//   - each change is written to the audit trail of the instance
func TestAdmin_Accounts(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	users := e.listUsers(t)
	if len(users) != 3 || users["root@example.com"].Role != "admin" || !users["mfa@example.com"].SecondFactor || users["other@example.com"].SecondFactor || users["other@example.com"].Disabled {
		t.Fatalf("users = %+v", users)
	}
	otherPath := "/api/v1/admin/users/" + e.other.id.String()

	if r := e.root.call(http.MethodPatch, otherPath, map[string]bool{"disabled": true}); r.Status != http.StatusNoContent {
		t.Fatalf("disable = %d %s", r.Status, r.Body)
	}
	if r := e.other.call(http.MethodGet, "/api/v1/me", nil); r.Status != http.StatusUnauthorized {
		t.Errorf("session of a disabled account = %d; want 401", r.Status)
	}
	if r := e.a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": "other@example.com", "password": adminPassword}}); r.Status != http.StatusForbidden {
		t.Errorf("sign-in of a disabled account = %d %s; want 403", r.Status, r.Body)
	}
	if !e.listUsers(t)["other@example.com"].Disabled {
		t.Error("the list does not show the account as disabled")
	}
	if r := e.root.call(http.MethodPatch, otherPath, map[string]bool{"disabled": false}); r.Status != http.StatusNoContent {
		t.Fatalf("enable = %d %s", r.Status, r.Body)
	}
	e.a.signIn2(t, "other@example.com")

	if r := e.root.call(http.MethodPatch, otherPath, map[string]string{"role": "admin"}); r.Status != http.StatusNoContent {
		t.Fatalf("promote = %d %s", r.Status, r.Body)
	}
	if e.listUsers(t)["other@example.com"].Role != "admin" {
		t.Error("the list does not show the new role")
	}
	if r := e.root.call(http.MethodPatch, otherPath, map[string]any{"role": "user", "disabled": true}); r.Status != http.StatusNoContent {
		t.Fatalf("demote and disable = %d %s", r.Status, r.Body)
	}

	for name, c := range map[string]struct {
		path string
		body any
		want int
		code string
	}{
		"nothing to change": {otherPath, map[string]string{}, http.StatusUnprocessableEntity, "empty_patch"},
		"unknown role":      {otherPath, map[string]string{"role": "owner"}, http.StatusUnprocessableEntity, "invalid_role"},
		"unknown account":   {"/api/v1/admin/users/00000000-0000-0000-0000-000000000000", map[string]bool{"disabled": true}, http.StatusNotFound, "account_not_found"},
		"own role":          {"/api/v1/admin/users/" + e.root.id.String(), map[string]string{"role": "user"}, http.StatusConflict, "own_account"},
		"own disabling":     {"/api/v1/admin/users/" + e.root.id.String(), map[string]bool{"disabled": true}, http.StatusConflict, "own_account"},
	} {
		if r := e.root.call(http.MethodPatch, c.path, c.body); r.Status != c.want || !strings.Contains(string(r.Body), `"`+c.code+`"`) {
			t.Errorf("%s = %d %s; want %d %s", name, r.Status, r.Body, c.want, c.code)
		}
	}
	if r := e.root.call(http.MethodPatch, "/api/v1/admin/users/not-a-uuid", map[string]bool{"disabled": true}); r.Status != http.StatusBadRequest {
		t.Errorf("a malformed identifier = %d; want 400", r.Status)
	}

	r := e.root.call(http.MethodGet, "/api/v1/admin/audit", nil)
	var trail []httpapi.AuditEvent
	mustDecode(t, r.Body, &trail)
	want := []string{audit.AdminUserDisabled, audit.AdminRoleChanged, audit.AdminRoleChanged, audit.AdminUserEnabled, audit.AdminUserDisabled}
	if r.Status != http.StatusOK || len(trail) != len(want) {
		t.Fatalf("audit = %d %+v; want %v", r.Status, actions(trail), want)
	}
	for i := range want {
		if trail[i].Action != want[i] {
			t.Fatalf("audit actions = %v; want %v", actions(trail), want)
		}
	}
	if trail[4].Target != "other@example.com" || trail[4].Actor == nil || *trail[4].Actor != "root@example.com" || trail[4].Ip == nil || trail[3].Meta == nil || trail[2].Meta["role"] != "admin" || trail[1].Meta["role"] != "user" {
		t.Errorf("audit events = %+v; want the actor, the address and the new role", trail)
	}
}

// TestAdmin_SecondFactorReset verifies removing the second factor of somebody who lost it.
//
// Scenarios:
//   - the password alone signs the account in after the reset, and its status and the list show no second factor
//   - the reset is written to the audit trail with the account as the target
//   - an administrator cannot reset their own, and an unknown account is not found
func TestAdmin_SecondFactorReset(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	login := func() reply {
		return e.a.do(request{method: http.MethodPost, path: "/api/v1/auth/login", body: map[string]string{"email": e.email, "password": adminPassword}})
	}
	if r := login(); r.Status != http.StatusAccepted {
		t.Fatalf("sign-in with a second factor = %d; want 202", r.Status)
	}
	if r := e.root.call(http.MethodPost, "/api/v1/admin/users/"+e.id.String()+"/second-factor/reset", nil); r.Status != http.StatusNoContent {
		t.Fatalf("reset = %d %s", r.Status, r.Body)
	}
	if r := login(); r.Status != http.StatusOK {
		t.Errorf("sign-in after the reset = %d %s; want 200 with the password alone", r.Status, r.Body)
	}
	if e.listUsers(t)["mfa@example.com"].SecondFactor {
		t.Error("the list still shows a second factor")
	}
	status := e.call(http.MethodGet, "/api/v1/me/security", nil)
	var security httpapi.SecurityStatus
	mustDecode(t, status.Body, &security)
	if security.Totp || security.RecoveryCodesLeft != 0 || len(security.Passkeys) != 0 {
		t.Errorf("security = %+v; want nothing left", security)
	}
	r := e.root.call(http.MethodGet, "/api/v1/admin/audit?"+url.Values{"action": {"admin.second"}}.Encode(), nil)
	var trail []httpapi.AuditEvent
	mustDecode(t, r.Body, &trail)
	if len(trail) != 1 || trail[0].Action != audit.AdminSecondFactorReset || trail[0].Target != "mfa@example.com" {
		t.Errorf("audit = %+v", trail)
	}
	if r := e.root.call(http.MethodPost, "/api/v1/admin/users/"+e.root.id.String()+"/second-factor/reset", nil); r.Status != http.StatusConflict {
		t.Errorf("resetting yourself = %d; want 409", r.Status)
	}
	if r := e.root.call(http.MethodPost, "/api/v1/admin/users/"+"00000000-0000-0000-0000-000000000000"+"/second-factor/reset", nil); r.Status != http.StatusNotFound {
		t.Errorf("resetting an unknown account = %d; want 404", r.Status)
	}
}

// TestAdmin_Networks verifies the private networks saved sites may reach.
//
// Scenarios:
//   - reading shows the fixed and the custom networks, empty at first
//   - replacing stores them, applies them to the policy at once and returns the masked, deduplicated list, and replacing again with nothing closes them
//   - entries that are not networks, are too wide or are never allowed, and more than the maximum, are refused with their codes and change nothing
//   - each replacement is written to the audit trail with the count
func TestAdmin_Networks(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t)
	read := func() httpapi.Networks {
		r := e.root.call(http.MethodGet, "/api/v1/admin/networks", nil)
		var n httpapi.Networks
		mustDecode(t, r.Body, &n)
		if r.Status != http.StatusOK {
			t.Fatalf("read networks = %d %s", r.Status, r.Body)
		}
		return n
	}
	if n := read(); len(n.Fixed) != 0 || len(n.Custom) != 0 || n.Custom == nil {
		t.Fatalf("networks = %+v; want empty lists", n)
	}
	r := e.root.call(http.MethodPut, "/api/v1/admin/networks", map[string][]string{"networks": {"192.168.1.9/24", "192.168.1.0/24", "10.0.0.5"}})
	var set httpapi.Networks
	mustDecode(t, r.Body, &set)
	if r.Status != http.StatusOK || len(set.Custom) != 2 || set.Custom[0] != "10.0.0.5/32" || set.Custom[1] != "192.168.1.0/24" {
		t.Fatalf("replace = %d %+v", r.Status, set)
	}
	if got := e.a.policy.Extra(); len(got) != 2 {
		t.Errorf("policy = %v; want the two networks applied", got)
	}
	if n := read(); len(n.Custom) != 2 {
		t.Errorf("networks after replacing = %+v", n)
	}

	many := make([]string, 101)
	for i := range many {
		many[i] = "10.0.0.1"
	}
	for name, c := range map[string]struct {
		entries []string
		code    string
	}{
		"text":     {[]string{"nope"}, "invalid_network"},
		"wide":     {[]string{"0.0.0.0/0"}, "invalid_network"},
		"metadata": {[]string{"169.254.169.254"}, "invalid_network"},
		"many":     {many, "too_many_networks"},
	} {
		if r := e.root.call(http.MethodPut, "/api/v1/admin/networks", map[string][]string{"networks": c.entries}); r.Status != http.StatusUnprocessableEntity || !strings.Contains(string(r.Body), `"`+c.code+`"`) {
			t.Errorf("%s = %d %s; want 422 %s", name, r.Status, r.Body, c.code)
		}
	}
	if got := e.a.policy.Extra(); len(got) != 2 {
		t.Errorf("policy = %v; want it unchanged by the refusals", got)
	}
	if r := e.root.call(http.MethodPut, "/api/v1/admin/networks", map[string][]string{"networks": {}}); r.Status != http.StatusOK || len(e.a.policy.Extra()) != 0 {
		t.Errorf("clearing = %d; policy %v", r.Status, e.a.policy.Extra())
	}

	trail := e.root.call(http.MethodGet, "/api/v1/admin/audit?limit=1", nil)
	var events []httpapi.AuditEvent
	mustDecode(t, trail.Body, &events)
	if len(events) != 1 || events[0].Action != audit.AdminNetworksChanged || events[0].Meta["count"] != "0" {
		t.Errorf("audit = %+v; want the last change with its count", events)
	}
	if r := e.root.call(http.MethodGet, "/api/v1/admin/audit?action=Bad+Filter", nil); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a filter that is not words = %d; want 422", r.Status)
	}
	older := e.root.call(http.MethodGet, "/api/v1/admin/audit?limit=5&before="+strconv.FormatInt(events[0].Id, 10), nil)
	var page []httpapi.AuditEvent
	mustDecode(t, older.Body, &page)
	if len(page) != 1 || page[0].Meta["count"] != "2" {
		t.Errorf("the page before = %+v; want the first replacement", page)
	}
}

// brokenAdmin is a store whose administration operations fail.
type brokenAdmin struct{ *postgres.Store }

// Accounts always fails.
func (brokenAdmin) Accounts(context.Context) ([]admin.Account, error) { return nil, errInjected }

// Account always fails.
func (brokenAdmin) Account(context.Context, uuid.UUID) (admin.Account, error) {
	return admin.Account{}, errInjected
}

// Networks always fails.
func (brokenAdmin) Networks(context.Context) ([]netip.Prefix, error) { return nil, errInjected }

// ReplaceNetworks always fails.
func (brokenAdmin) ReplaceNetworks(context.Context, []netip.Prefix, uuid.UUID, time.Time) error {
	return errInjected
}

// TestAdmin_Failures verifies how a failing store behaves.
//
// Scenarios:
//   - every endpoint answers 500 when the store fails
//   - a change still succeeds when its audit event cannot be written
func TestAdmin_Failures(t *testing.T) {
	t.Parallel()
	e := newAdminEnv(t, func(d *httpapi.Deps) {
		d.Admin = admin.NewService(brokenAdmin{d.Readiness.(*postgres.Store)}, netguard.NewPolicy(nil, nil), d.Quotas, realClock{})
	})
	id := e.other.id.String()
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/admin/users", nil},
		{http.MethodPatch, "/api/v1/admin/users/" + id, map[string]bool{"disabled": true}},
		{http.MethodPatch, "/api/v1/admin/users/" + id, map[string]string{"role": "admin"}},
		{http.MethodPost, "/api/v1/admin/users/" + id + "/second-factor/reset", nil},
		{http.MethodGet, "/api/v1/admin/networks", nil},
		{http.MethodPut, "/api/v1/admin/networks", map[string][]string{"networks": {"10.0.0.0/8"}}},
	} {
		if r := e.root.call(c.method, c.path, c.body); r.Status != http.StatusInternalServerError {
			t.Errorf("%s %s with a failing store = %d %s; want 500", c.method, c.path, r.Status, r.Body)
		}
	}

	audited := newAdminEnv(t, func(d *httpapi.Deps) {
		d.Audit = audit.NewService(brokenAudit{d.Readiness.(*postgres.Store)}, d.Workspaces, realClock{})
	})
	if r := audited.root.call(http.MethodPatch, "/api/v1/admin/users/"+audited.other.id.String(), map[string]bool{"disabled": true}); r.Status != http.StatusNoContent {
		t.Errorf("disable with a failing audit store = %d %s; want the change to succeed", r.Status, r.Body)
	}
	if r := audited.root.call(http.MethodGet, "/api/v1/admin/audit", nil); r.Status != http.StatusInternalServerError {
		t.Errorf("reading a failing trail = %d; want 500", r.Status)
	}
}
