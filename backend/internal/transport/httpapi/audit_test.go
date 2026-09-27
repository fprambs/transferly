package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

// trail reads the audit trail of the team as a role, with the query given.
func (e *teamEnv) trail(as string, query url.Values) (int, []httpapi.AuditEvent) {
	e.a.t.Helper()
	path := "/api/v1/workspaces/" + e.ws.String() + "/audit"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	r := e.a.do(request{method: http.MethodGet, path: path, cookie: e.cookie[as]})
	var events []httpapi.AuditEvent
	if r.Status == http.StatusOK {
		mustDecode(e.a.t, r.Body, &events)
	}
	return r.Status, events
}

// actions lists the actions of events, newest first.
func actions(events []httpapi.AuditEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}

// TestAudit_Trail verifies that what changes in a workspace is written to its audit trail and can be read by whoever manages it.
//
// Scenarios:
//   - creating a team, inviting, revoking, joining, changing a role, removing, leaving, transferring, changing sites, trusting servers,
//     importing and rotating the key each leave one event with the actor, the target and the short facts
//   - the events come newest first, filter by group or action, page with the before cursor and honor the limit
//   - editors and viewers are refused, strangers get 404, anonymous callers 401, and a filter that is not words is 422
//   - a failing audit store never fails what was being recorded
func TestAudit_Trail(t *testing.T) {
	e := newTeamEnv(t)
	editor, viewer := e.user["editor"].String(), e.user["viewer"].String()
	for _, as := range []string{"editor", "viewer"} {
		if status, _ := e.trail(as, nil); status != http.StatusForbidden {
			t.Errorf("trail as %s = %d; want 403", as, status)
		}
	}
	e.call("owner", http.MethodPatch, "/members/"+editor, map[string]string{"role": "viewer"})
	created, _ := e.invite("owner", "newbie@example.com", "editor")
	e.invite("owner", "revoked@example.com", "viewer")
	var pending []httpapi.Invitation
	mustDecode(t, e.call("owner", http.MethodGet, "/invitations", nil).Body, &pending)
	for _, i := range pending {
		if i.Email == "revoked@example.com" {
			e.call("owner", http.MethodDelete, "/invitations/"+i.Id.String(), nil)
		}
	}
	e.anonymous("/api/v1/invitations/join", map[string]string{"token": created.Token, "password": adminPassword})
	e.call("owner", http.MethodDelete, "/members/"+viewer, nil)
	e.call("editor", http.MethodPost, "/leave", nil)

	site := e.call("owner", http.MethodPost, "/sites", siteBody("Production"))
	siteID := site.site(t).Id.String()
	e.call("owner", http.MethodPut, "/sites/"+siteID, siteBody("Production"))
	e.call("owner", http.MethodDelete, "/sites/"+siteID, nil)
	e.call("owner", http.MethodDelete, "/sites/"+uuid.NewString(), nil)
	e.call("owner", http.MethodPost, "/known-hosts", map[string]any{"host": "files.example.com", "port": 22, "keyType": "ssh-ed25519", "fingerprint": fingerprintA})
	e.a.do(request{method: http.MethodDelete, path: "/api/v1/workspaces/" + e.ws.String() + "/known-hosts?host=files.example.com&port=22&keyType=ssh-ed25519", cookie: e.cookie["owner"], headers: map[string]string{"X-CSRF-Token": e.csrf["owner"]}})
	e.a.importCall(e.ws.String(), "?dryRun=true", manager, e.cookie["owner"], e.csrf["owner"])
	e.a.importCall(e.ws.String(), "", manager, e.cookie["owner"], e.csrf["owner"])
	e.call("owner", http.MethodPost, "/transfer", map[string]string{"userId": e.user["admin"].String()})

	status, all := e.trail("admin", nil)
	if status != http.StatusOK {
		t.Fatalf("trail = %d", status)
	}
	want := []string{
		audit.OwnershipTransferred, audit.SitesImported, audit.HostKeyForgotten, audit.HostKeyTrusted, audit.SiteDeleted, audit.SiteUpdated, audit.SiteCreated,
		audit.MemberLeft, audit.KeyRotated, audit.MemberRemoved, audit.MemberJoined, audit.InvitationRevoked, audit.InvitationCreated, audit.InvitationCreated, audit.MemberRoleChanged, audit.TeamCreated,
	}
	if got := actions(all); len(got) != len(want) {
		t.Fatalf("actions = %v; want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("actions = %v; want %v", got, want)
			}
		}
	}
	byAction := map[string]httpapi.AuditEvent{}
	for _, event := range all {
		byAction[event.Action] = event
	}
	if joined := byAction[audit.MemberJoined]; joined.Target != "newbie@example.com" || joined.Meta["role"] != "editor" || joined.Actor == nil || *joined.Actor != "newbie@example.com" {
		t.Errorf("join = %+v", joined)
	}
	if role := byAction[audit.MemberRoleChanged]; role.Target != "editor@example.com" || role.Meta["role"] != "viewer" || *role.Actor != "owner@example.com" {
		t.Errorf("role change = %+v", role)
	}
	if removed := byAction[audit.MemberRemoved]; removed.Target != "viewer@example.com" {
		t.Errorf("removal = %+v", removed)
	}
	if left := byAction[audit.MemberLeft]; left.Target != "editor@example.com" {
		t.Errorf("leaving = %+v", left)
	}
	if deleted := byAction[audit.SiteDeleted]; deleted.Target != "Production" {
		t.Errorf("site deletion = %+v", deleted)
	}
	if trusted := byAction[audit.HostKeyTrusted]; trusted.Target != "files.example.com:22" || trusted.Meta["type"] != "ssh-ed25519" {
		t.Errorf("trusted key = %+v", trusted)
	}
	if imported := byAction[audit.SitesImported]; imported.Meta["sites"] != "1" {
		t.Errorf("import = %+v", imported)
	}
	if transfer := byAction[audit.OwnershipTransferred]; transfer.Target != "admin@example.com" || transfer.Ip == nil {
		t.Errorf("transfer = %+v", transfer)
	}

	_, members := e.trail("admin", url.Values{"action": {"member."}})
	if got := actions(members); len(got) != 4 || got[0] != audit.MemberLeft {
		t.Errorf("member events = %v", got)
	}
	_, one := e.trail("admin", url.Values{"action": {audit.SiteCreated}})
	if len(one) != 1 {
		t.Errorf("one action = %v", actions(one))
	}
	_, page := e.trail("admin", url.Values{"limit": {"3"}})
	_, next := e.trail("admin", url.Values{"limit": {"3"}, "before": {strconv.FormatInt(page[2].Id, 10)}})
	if len(page) != 3 || len(next) != 3 || next[0].Id >= page[2].Id {
		t.Errorf("pages = %v then %v", actions(page), actions(next))
	}

	for as, want := range map[string]int{"viewer": http.StatusNotFound, "stranger": http.StatusNotFound, "owner": http.StatusOK} {
		if status, _ := e.trail(as, nil); status != want {
			t.Errorf("trail as %s = %d; want %d", as, status, want)
		}
	}
	base := "/api/v1/workspaces/" + e.ws.String() + "/audit"
	expectProblem(t, e.a.do(request{method: http.MethodGet, path: base}), 401, "unauthenticated")
	expectProblem(t, e.a.do(request{method: http.MethodGet, path: base + "?action=Bad%20Filter", cookie: e.cookie["admin"]}), 422, "invalid_filter")
}

// brokenAudit is a store whose audit operations fail.
type brokenAudit struct{ *postgres.Store }

// InsertAudit always fails.
func (brokenAudit) InsertAudit(context.Context, audit.Event) error { return errInjected }

// AuditByWorkspace always fails.
func (brokenAudit) AuditByWorkspace(context.Context, uuid.UUID, audit.Filter, int) ([]audit.Event, error) {
	return nil, errInjected
}

// AuditInstance always fails.
func (brokenAudit) AuditInstance(context.Context, audit.Filter, int) ([]audit.Event, error) {
	return nil, errInjected
}

// TestAudit_Failures verifies how a failing audit store behaves.
//
// Scenarios:
//   - a change still succeeds when its audit event cannot be written
//   - reading the trail answers 500
func TestAudit_Failures(t *testing.T) {
	e := newTeamEnv(t, func(d *httpapi.Deps) {
		d.Audit = audit.NewService(brokenAudit{d.Readiness.(*postgres.Store)}, d.Workspaces, realClock{})
	})
	if r := e.call("owner", http.MethodPost, "/sites", siteBody("Production")); r.Status != http.StatusCreated {
		t.Errorf("create site = %d %s; want the change to succeed", r.Status, r.Body)
	}
	if status, _ := e.trail("owner", nil); status != http.StatusInternalServerError {
		t.Errorf("trail = %d; want 500", status)
	}
}
