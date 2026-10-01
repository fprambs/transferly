package storetest

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/invite"
	"github.com/fprambs/transferly/internal/core/limits"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/transfer"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// LimitsStore is the persistence an implementation offers to the limits package, plus what the suite needs to set up.
type LimitsStore interface {
	limits.Store
	auth.UserStore
	workspace.Store
	site.Store
	invite.Store
	transfer.Store
}

// LimitsFactory returns a new, empty store for one test.
type LimitsFactory func(t *testing.T) LimitsStore

// RunLimits executes the limits store suite against stores built by newStore.
//
// Scenarios:
//   - there are no stored limits at first, saved limits read back with every field, and saving again replaces them
//   - the sites of a workspace are counted and those of other workspaces are not
//   - the teams a person owns are counted, without their personal workspace and without the teams of others
//   - the members of a workspace are counted together with its invitations that have not expired
//   - the bytes a person moved are added up from the transfers updated since an instant, and only theirs
func RunLimits(t *testing.T, newStore LimitsFactory) {
	t.Helper()
	ctx := t.Context()
	store := newStore(t)
	alice, bob := newUser("alice@example.com"), newUser("bob@example.com")
	for _, u := range []auth.User{alice, bob} {
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	personal := newWorkspace(alice.ID, workspace.KindPersonal, "Personal")
	teamA, teamB := newWorkspace(alice.ID, workspace.KindTeam, "Ops"), newWorkspace(alice.ID, workspace.KindTeam, "Dev")
	teamC := newWorkspace(bob.ID, workspace.KindTeam, "Bob team")
	for _, w := range []workspace.Workspace{personal, teamA, teamB, teamC} {
		if err := store.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}

	if _, found, err := store.Limits(ctx); err != nil || found {
		t.Fatalf("Limits() at first = %v, %v; want none", found, err)
	}
	want := limits.Limits{BandwidthGlobal: 1, BandwidthPerUser: 2, TransfersGlobal: 3, TransfersPerWorkspace: 4, TransfersPerUser: 5, TransfersPerSite: 6,
		MaxSitesPerWorkspace: 7, MaxTeamsPerUser: 8, MaxMembersPerTeam: 9, DailyBytesPerUser: 10}
	if err := store.SaveLimits(ctx, want, alice.ID, now); err != nil {
		t.Fatalf("SaveLimits() = %v", err)
	}
	if got, found, err := store.Limits(ctx); err != nil || !found || got != want {
		t.Fatalf("Limits() = %+v, %v, %v; want %+v", got, found, err, want)
	}
	second := limits.Limits{TransfersGlobal: 99}
	if err := store.SaveLimits(ctx, second, uuid.Nil, now.Add(time.Minute)); err != nil {
		t.Fatalf("SaveLimits() again = %v", err)
	}
	if got, _, _ := store.Limits(ctx); got != second {
		t.Fatalf("Limits() = %+v; want the second, replacing the first", got)
	}

	for _, s := range []site.Site{newSite(teamA.ID, "One"), newSite(teamA.ID, "Two"), newSite(teamB.ID, "Other")} {
		if err := store.CreateSite(ctx, s, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := store.CountSites(ctx, teamA.ID); err != nil || n != 2 {
		t.Fatalf("CountSites() = %d, %v; want 2", n, err)
	}
	if n, _ := store.CountSites(ctx, uuid.New()); n != 0 {
		t.Fatalf("CountSites(unknown) = %d; want 0", n)
	}

	if n, err := store.CountOwnedTeams(ctx, alice.ID); err != nil || n != 2 {
		t.Fatalf("CountOwnedTeams() = %d, %v; want the two teams and not the personal workspace", n, err)
	}
	if n, _ := store.CountOwnedTeams(ctx, bob.ID); n != 1 {
		t.Fatalf("CountOwnedTeams(bob) = %d; want 1", n)
	}

	if n, err := store.CountMembers(ctx, teamA.ID, now); err != nil || n != 1 {
		t.Fatalf("CountMembers() = %d, %v; want the owner", n, err)
	}
	live := invite.Invitation{ID: uuid.New(), WorkspaceID: teamA.ID, Email: "new@example.com", Role: workspace.RoleViewer, InvitedBy: alice.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	dead := invite.Invitation{ID: uuid.New(), WorkspaceID: teamA.ID, Email: "old@example.com", Role: workspace.RoleViewer, InvitedBy: alice.ID, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}
	for i, inv := range []invite.Invitation{live, dead} {
		if err := store.Create(ctx, inv, []byte{byte(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := store.CountMembers(ctx, teamA.ID, now); n != 2 {
		t.Fatalf("CountMembers() = %d; want the owner and the invitation that waits, not the expired one", n)
	}

	files := newSite(personal.ID, "Files")
	if err := store.CreateSite(ctx, files, nil); err != nil {
		t.Fatal(err)
	}
	moved := func(user uuid.UUID, ws uuid.UUID, at time.Time, bytes int64) {
		t.Helper()
		tr := transfer.Transfer{ID: uuid.New(), WorkspaceID: ws, UserID: user, SiteID: files.ID, SiteName: "Files", Direction: transfer.Upload, Path: "/a", Name: "a", Status: transfer.Done,
			BytesDone: bytes, Attempts: 1, CreatedAt: at, UpdatedAt: at}
		if err := store.CreateTransfer(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}
	moved(alice.ID, personal.ID, now, 100)
	moved(alice.ID, personal.ID, now.Add(time.Hour), 50)
	moved(alice.ID, personal.ID, now.Add(-48*time.Hour), 1000)
	moved(bob.ID, teamC.ID, now, 7)
	if n, err := store.BytesSince(ctx, alice.ID, now.Add(-time.Minute)); err != nil || n != 150 {
		t.Fatalf("BytesSince() = %d, %v; want 150", n, err)
	}
	if n, _ := store.BytesSince(ctx, alice.ID, now.Add(-72*time.Hour)); n != 1150 {
		t.Fatalf("BytesSince(all) = %d; want 1150", n)
	}
	if n, _ := store.BytesSince(ctx, uuid.New(), now.Add(-72*time.Hour)); n != 0 {
		t.Fatalf("BytesSince(nobody) = %d; want 0", n)
	}
}
