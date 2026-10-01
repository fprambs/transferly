package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/scheduler"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// readLimits reads the limits as the administrator of a file environment.
func (e *fileEnv) readLimits() httpapi.InstanceLimitsView {
	e.a.t.Helper()
	r := e.a.send(http.MethodGet, "/api/v1/admin/limits", nil, e.cookie, e.csrf)
	var view httpapi.InstanceLimitsView
	mustDecode(e.a.t, r.Body, &view)
	if r.Status != http.StatusOK {
		e.a.t.Fatalf("read limits = %d %s", r.Status, r.Body)
	}
	return view
}

// setLimits replaces the limits as the administrator of a file environment.
func (e *fileEnv) setLimits(body any) reply {
	e.a.t.Helper()
	return e.a.send(http.MethodPut, "/api/v1/admin/limits", body, e.cookie, e.csrf)
}

// TestLimits_Endpoints verifies reading and replacing the caps and quotas.
//
// Scenarios:
//   - anonymous callers get 401 and regular accounts 403 on both endpoints
//   - the limits start at what the server started with, all zero here, next to the same defaults
//   - replacing them is answered with the new limits, kept for the next read and left next to the unchanged defaults
//   - negative values, values above the ceilings and values that do not fit a count are 422 invalid_limits and change nothing
//   - a replacement is written to the audit trail of the instance
//   - a store that fails to save answers 500
func TestLimits_Endpoints(t *testing.T) {
	e := newFileEnv(t)
	guest, _ := e.a.signIn("regular@example.com")
	for _, c := range []struct {
		method string
		body   any
	}{{http.MethodGet, nil}, {http.MethodPut, map[string]int{}}} {
		if r := e.a.do(request{method: c.method, path: "/api/v1/admin/limits", body: c.body}); r.Status != http.StatusUnauthorized {
			t.Errorf("anonymous %s = %d; want 401", c.method, r.Status)
		}
		if r := e.a.send(c.method, "/api/v1/admin/limits", c.body, guest, csrf(e.a, guest)); r.Status != http.StatusForbidden || !strings.Contains(string(r.Body), `"forbidden"`) {
			t.Errorf("regular %s = %d %s; want 403 forbidden", c.method, r.Status, r.Body)
		}
	}

	if view := e.readLimits(); view.Current != (httpapi.InstanceLimits{}) || view.Defaults != (httpapi.InstanceLimits{}) {
		t.Fatalf("limits at first = %+v", view)
	}
	want := httpapi.InstanceLimits{BandwidthGlobal: 5_000_000, BandwidthPerUser: 1_000_000, TransfersGlobal: 10, TransfersPerWorkspace: 5, TransfersPerUser: 3, TransfersPerSite: 2,
		MaxSitesPerWorkspace: 20, MaxTeamsPerUser: 4, MaxMembersPerTeam: 30, DailyBytesPerUser: 5 << 30}
	r := e.setLimits(want)
	var got httpapi.InstanceLimitsView
	mustDecode(t, r.Body, &got)
	if r.Status != http.StatusOK || got.Current != want || got.Defaults != (httpapi.InstanceLimits{}) {
		t.Fatalf("replace = %d %+v", r.Status, got)
	}
	if view := e.readLimits(); view.Current != want {
		t.Errorf("limits after replacing = %+v; want %+v", view.Current, want)
	}

	for _, body := range map[string]map[string]int64{
		"negative":   {"bandwidthGlobal": -1},
		"ceiling":    {"transfersGlobal": 10_001},
		"too big":    {"maxSitesPerWorkspace": 1 << 40},
		"too small":  {"maxTeamsPerUser": -(1 << 40)},
		"huge daily": {"dailyBytesPerUser": 1 << 60},
	} {
		full := map[string]int64{"bandwidthGlobal": 0, "bandwidthPerUser": 0, "transfersGlobal": 0, "transfersPerWorkspace": 0, "transfersPerUser": 0, "transfersPerSite": 0,
			"maxSitesPerWorkspace": 0, "maxTeamsPerUser": 0, "maxMembersPerTeam": 0, "dailyBytesPerUser": 0}
		for k, v := range body {
			full[k] = v
		}
		expectProblem(t, e.setLimits(full), 422, "invalid_limits")
	}
	if view := e.readLimits(); view.Current != want {
		t.Errorf("limits after refusals = %+v; want them unchanged", view.Current)
	}

	trail := e.a.send(http.MethodGet, "/api/v1/admin/audit?action=admin.limits", nil, e.cookie, e.csrf)
	var events []httpapi.AuditEvent
	mustDecode(t, trail.Body, &events)
	if len(events) != 1 || events[0].Action != audit.AdminLimitsChanged {
		t.Errorf("audit = %+v; want one limits change", actions(events))
	}
}

// brokenLimits is a store whose limits cannot be saved or counted.
type brokenLimits struct{ *postgres.Store }

// SaveLimits always fails.
func (brokenLimits) SaveLimits(context.Context, limits.Limits, uuid.UUID, time.Time) error {
	return errInjected
}

// CountOwnedTeams always fails.
func (brokenLimits) CountOwnedTeams(context.Context, uuid.UUID) (int, error) { return 0, errInjected }

// noApplier puts no limit in force anywhere.
type noApplier struct{}

// SetLimits does nothing.
func (noApplier) SetLimits(scheduler.Limits) {}

// SetBandwidth does nothing.
func (noApplier) SetBandwidth(int64, int64) {}

// TestLimits_StoreFailures verifies how a failing store behaves.
//
// Scenarios:
//   - a limits change that cannot be saved answers 500
//   - creating a team answers 500 when the teams the person owns cannot be counted
func TestLimits_StoreFailures(t *testing.T) {
	e := newAdminEnv(t, func(d *httpapi.Deps) {
		store := d.Readiness.(*postgres.Store)
		d.Quotas = limits.NewService(brokenLimits{store}, noApplier{}, limits.Limits{MaxTeamsPerUser: 1}, realClock{})
		d.Admin = admin.NewService(store, netguard.NewPolicy(nil, nil), d.Quotas, realClock{})
	})
	if r := e.root.call(http.MethodPut, "/api/v1/admin/limits", map[string]int{"bandwidthGlobal": 0, "bandwidthPerUser": 0, "transfersGlobal": 0, "transfersPerWorkspace": 0,
		"transfersPerUser": 0, "transfersPerSite": 0, "maxSitesPerWorkspace": 0, "maxTeamsPerUser": 0, "maxMembersPerTeam": 0, "dailyBytesPerUser": 0}); r.Status != http.StatusInternalServerError {
		t.Errorf("saving with a failing store = %d %s; want 500", r.Status, r.Body)
	}
	if r := e.other.call(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": "Team"}); r.Status != http.StatusInternalServerError {
		t.Errorf("creating a team with a failing count = %d %s; want 500", r.Status, r.Body)
	}
}

// TestLimits_Quotas verifies that the quotas are enforced where things are created.
//
// Scenarios:
//   - a workspace with as many sites as its quota refuses another with 409 site_quota_reached, and works again once the quota is lifted
//   - a person who moved their volume of the day is refused new transfers with 403 daily_quota_reached, and small transfers below it work
//   - a person who owns as many teams as their quota cannot create another (409 team_quota_reached)
//   - a team whose members and waiting invitations reach its quota refuses another invitation (409 member_quota_reached)
func TestLimits_Quotas(t *testing.T) {
	e := newFileEnv(t)
	if r := e.setLimits(httpapi.InstanceLimits{MaxSitesPerWorkspace: 1}); r.Status != http.StatusOK {
		t.Fatalf("set limits = %d %s", r.Status, r.Body)
	}
	expectProblem(t, e.a.send(http.MethodPost, sitesPath(e.ws), siteBody("Second"), e.cookie, e.csrf), 409, "site_quota_reached")
	if r := e.setLimits(httpapi.InstanceLimits{MaxSitesPerWorkspace: 2}); r.Status != http.StatusOK {
		t.Fatal(r.Status)
	}
	if r := e.a.send(http.MethodPost, sitesPath(e.ws), siteBody("Second"), e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Errorf("a second site under a quota of two = %d %s", r.Status, r.Body)
	}

	if r := e.setLimits(httpapi.InstanceLimits{DailyBytesPerUser: 10}); r.Status != http.StatusOK {
		t.Fatal(r.Status)
	}
	if r := e.a.upload(e.ws, e.site, q("path", "/a.txt"), "hello", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("first upload under the daily quota = %d %s", r.Status, r.Body)
	}
	if r := e.a.upload(e.ws, e.site, q("path", "/b.txt"), "world!!!", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Fatalf("second upload under the daily quota = %d %s", r.Status, r.Body)
	}
	expectProblem(t, e.a.upload(e.ws, e.site, q("path", "/c.txt"), "x", e.cookie, e.csrf), 403, "daily_quota_reached")
	if r := e.setLimits(httpapi.InstanceLimits{}); r.Status != http.StatusOK {
		t.Fatal(r.Status)
	}
	if r := e.a.upload(e.ws, e.site, q("path", "/c.txt"), "x", e.cookie, e.csrf); r.Status != http.StatusCreated {
		t.Errorf("upload after lifting the quota = %d %s", r.Status, r.Body)
	}

	var quotas *limits.Service
	team := newTeamEnv(t, func(d *httpapi.Deps) { quotas = d.Quotas })
	set := func(l limits.Limits) {
		t.Helper()
		if _, err := quotas.Set(context.Background(), uuid.Nil, l); err != nil {
			t.Fatal(err)
		}
	}
	set(limits.Limits{MaxTeamsPerUser: 1})
	expectProblem(t, team.a.send(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": "Another"}, team.cookie["owner"], team.csrf["owner"]), 409, "team_quota_reached")

	set(limits.Limits{MaxMembersPerTeam: 5})
	if r := team.call("owner", http.MethodPost, "/invitations", map[string]string{"email": "one@example.com", "role": "viewer"}); r.Status != http.StatusCreated {
		t.Fatalf("invitation under the member quota = %d %s", r.Status, r.Body)
	}
	expectProblem(t, team.call("owner", http.MethodPost, "/invitations", map[string]string{"email": "two@example.com", "role": "viewer"}), 409, "member_quota_reached")
}
