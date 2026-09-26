package storetest

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// AuditStore is the persistence an implementation offers to the audit package, plus what the suite needs to set up.
type AuditStore interface {
	audit.Store
	workspace.Store
	auth.UserStore
}

// AuditFactory returns a new, empty store for one test.
type AuditFactory func(t *testing.T) AuditStore

// RunAudit executes the audit store suite against stores built by newStore.
//
// Scenarios:
//   - events come back newest first with the email of the actor, the metadata and UTC times, and only those of the workspace asked for
//   - the action filter keeps a group by its prefix or one action, and the before cursor and the limit page through the events
//   - an event without metadata reads back with empty metadata, and an actor whose account was deleted keeps the event with no email
func RunAudit(t *testing.T, newStore AuditFactory) {
	t.Helper()
	ctx := t.Context()
	store := newStore(t)
	alice := newUser("alice@example.com")
	if err := store.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	team, other := newWorkspace(alice.ID, workspace.KindTeam, "Ops"), newWorkspace(alice.ID, workspace.KindTeam, "Other")
	for _, w := range []workspace.Workspace{team, other} {
		if err := store.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	events := []audit.Event{
		{WorkspaceID: team.ID, ActorID: alice.ID, Action: audit.InvitationCreated, Target: "bob@example.com", IP: "203.0.113.5", Meta: map[string]string{"role": "editor"}},
		{WorkspaceID: team.ID, ActorID: alice.ID, Action: audit.MemberJoined, Target: "bob@example.com"},
		{WorkspaceID: team.ID, ActorID: alice.ID, Action: audit.MemberRemoved, Target: "bob@example.com"},
		{WorkspaceID: team.ID, ActorID: alice.ID, Action: audit.SiteCreated, Target: "Production"},
		{WorkspaceID: other.ID, ActorID: alice.ID, Action: audit.SiteCreated, Target: "Elsewhere"},
	}
	for i, e := range events {
		e.At = now.Add(time.Duration(i) * time.Minute)
		if err := store.InsertAudit(ctx, e); err != nil {
			t.Fatalf("InsertAudit() = %v", err)
		}
	}

	all, err := store.AuditByWorkspace(ctx, team.ID, audit.Filter{}, 100)
	if err != nil || len(all) != 4 || all[0].Action != audit.SiteCreated || all[3].Action != audit.InvitationCreated {
		t.Fatalf("AuditByWorkspace() = %+v, %v; want the four events of the team, newest first", all, err)
	}
	oldest := all[3]
	if oldest.ActorEmail != "alice@example.com" || oldest.Target != "bob@example.com" || oldest.IP != "203.0.113.5" || oldest.Meta["role"] != "editor" || oldest.ActorID != alice.ID || oldest.At.Location() != time.UTC || !oldest.At.Equal(now) {
		t.Fatalf("the oldest event = %+v; want every field intact", oldest)
	}
	if all[0].Meta == nil || len(all[0].Meta) != 0 {
		t.Fatalf("an event without metadata reads back as %v; want empty", all[0].Meta)
	}

	members, _ := store.AuditByWorkspace(ctx, team.ID, audit.Filter{Action: "member."}, 100)
	if len(members) != 2 || members[0].Action != audit.MemberRemoved {
		t.Fatalf("member events = %+v; want the two of the group", members)
	}
	one, _ := store.AuditByWorkspace(ctx, team.ID, audit.Filter{Action: audit.SiteCreated}, 100)
	if len(one) != 1 {
		t.Fatalf("one action = %+v; want 1", one)
	}
	page, _ := store.AuditByWorkspace(ctx, team.ID, audit.Filter{}, 2)
	next, _ := store.AuditByWorkspace(ctx, team.ID, audit.Filter{Before: page[1].ID}, 100)
	if len(page) != 2 || len(next) != 2 || next[0].ID >= page[1].ID {
		t.Fatalf("pages = %+v then %+v; want 2 and 2, the second older", page, next)
	}
	if elsewhere, _ := store.AuditByWorkspace(ctx, other.ID, audit.Filter{}, 100); len(elsewhere) != 1 || elsewhere[0].Target != "Elsewhere" {
		t.Fatalf("events of another workspace = %+v", elsewhere)
	}
	if none, _ := store.AuditByWorkspace(ctx, uuid.New(), audit.Filter{}, 100); len(none) != 0 {
		t.Fatalf("events of an unknown workspace = %+v; want none", none)
	}

	orphan := audit.Event{WorkspaceID: team.ID, Action: audit.MemberLeft, Target: "gone@example.com", At: now.Add(time.Hour)}
	if err := store.InsertAudit(ctx, orphan); err != nil {
		t.Fatalf("InsertAudit() without an actor = %v", err)
	}
	got, _ := store.AuditByWorkspace(ctx, team.ID, audit.Filter{Action: audit.MemberLeft}, 1)
	if len(got) != 1 || got[0].ActorEmail != "" || got[0].ActorID != uuid.Nil {
		t.Fatalf("an event without an actor = %+v; want an empty email", got)
	}
}
