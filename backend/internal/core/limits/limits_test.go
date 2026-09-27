package limits_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/limits"
	"github.com/fprambs/sentinel/internal/core/scheduler"
	"github.com/fprambs/sentinel/internal/testutil"
)

// errBoom is the failure injected by the fakes.
var errBoom = errors.New("boom")

// fakeStore keeps the limits and the counts in memory.
type fakeStore struct {
	stored  *limits.Limits
	saved   limits.Limits
	by      uuid.UUID
	at      time.Time
	sites   int
	teams   int
	members int
	bytes   int64
	since   time.Time
	fail    string
}

// failure returns the injected error when the operation is the chosen one.
func (f *fakeStore) failure(op string) error {
	if f.fail == op {
		return errBoom
	}
	return nil
}

// Limits returns the stored limits.
func (f *fakeStore) Limits(context.Context) (limits.Limits, bool, error) {
	if f.stored == nil {
		return limits.Limits{}, false, f.failure("Limits")
	}
	return *f.stored, true, f.failure("Limits")
}

// SaveLimits stores the limits.
func (f *fakeStore) SaveLimits(_ context.Context, l limits.Limits, by uuid.UUID, at time.Time) error {
	if err := f.failure("SaveLimits"); err != nil {
		return err
	}
	f.saved, f.by, f.at = l, by, at
	return nil
}

// CountSites returns the number of sites.
func (f *fakeStore) CountSites(context.Context, uuid.UUID) (int, error) {
	return f.sites, f.failure("CountSites")
}

// CountOwnedTeams returns the number of teams.
func (f *fakeStore) CountOwnedTeams(context.Context, uuid.UUID) (int, error) {
	return f.teams, f.failure("CountOwnedTeams")
}

// CountMembers returns the number of members and invitations.
func (f *fakeStore) CountMembers(context.Context, uuid.UUID, time.Time) (int, error) {
	return f.members, f.failure("CountMembers")
}

// BytesSince records the start of the window and returns the bytes moved.
func (f *fakeStore) BytesSince(_ context.Context, _ uuid.UUID, since time.Time) (int64, error) {
	f.since = since
	return f.bytes, f.failure("BytesSince")
}

// fakeApplier records what was put in force.
type fakeApplier struct {
	transfers scheduler.Limits
	global    int64
	perUser   int64
	calls     int
}

// SetLimits records the caps of concurrent transfers.
func (a *fakeApplier) SetLimits(l scheduler.Limits) { a.transfers, a.calls = l, a.calls+1 }

// SetBandwidth records the bandwidth caps.
func (a *fakeApplier) SetBandwidth(global, perUser int64) { a.global, a.perUser = global, perUser }

// defaults are the limits the tests start from.
var defaults = limits.Limits{TransfersGlobal: 32, TransfersPerWorkspace: 16, TransfersPerUser: 8, TransfersPerSite: 4}

// setup returns a service with its fakes.
func setup() (*limits.Service, *fakeStore, *fakeApplier) {
	store, applier := &fakeStore{}, &fakeApplier{}
	return limits.NewService(store, applier, defaults, testutil.NewFakeClock(time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC))), store, applier
}

// TestService_Load verifies the limits a server starts with.
//
// Scenarios:
//   - without stored limits the defaults are in force and reach the transfers
//   - stored limits win over the defaults and reach the transfers, with their bandwidth
//   - a failing store is reported
func TestService_Load(t *testing.T) {
	ctx := context.Background()
	svc, store, applier := setup()
	if err := svc.Load(ctx); err != nil || svc.Current() != defaults || applier.transfers.Global != 32 || applier.transfers.PerSite != 4 {
		t.Fatalf("Load() = %v, current %+v, applied %+v", err, svc.Current(), applier.transfers)
	}
	if svc.Defaults() != defaults {
		t.Errorf("Defaults() = %+v", svc.Defaults())
	}

	store.stored = &limits.Limits{BandwidthGlobal: 1000, BandwidthPerUser: 500, TransfersGlobal: 2, MaxSitesPerWorkspace: 5}
	if err := svc.Load(ctx); err != nil || svc.Current().MaxSitesPerWorkspace != 5 || applier.transfers.Global != 2 || applier.global != 1000 || applier.perUser != 500 {
		t.Fatalf("Load() with stored limits = %v, current %+v, applied %+v %d %d", err, svc.Current(), applier.transfers, applier.global, applier.perUser)
	}

	store.fail = "Limits"
	if err := svc.Load(ctx); !errors.Is(err, errBoom) {
		t.Errorf("Load() failure = %v", err)
	}
}

// TestService_Set verifies changing the limits.
//
// Scenarios:
//   - valid limits are stored with the actor and the time, put in force and returned
//   - negative values and values above the ceilings are refused, name every wrong field and change nothing
//   - a failing store leaves the limits as they were
func TestService_Set(t *testing.T) {
	ctx := context.Background()
	svc, store, applier := setup()
	admin := uuid.New()
	want := limits.Limits{BandwidthGlobal: 2_000_000, BandwidthPerUser: 1_000_000, TransfersGlobal: 4, DailyBytesPerUser: 1 << 30, MaxTeamsPerUser: 3}

	got, err := svc.Set(ctx, admin, want)
	if err != nil || got != want || svc.Current() != want || store.saved != want || store.by != admin || !store.at.Equal(time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("Set() = %+v, %v; current %+v, saved %+v", got, err, svc.Current(), store.saved)
	}
	if applier.global != 2_000_000 || applier.perUser != 1_000_000 || applier.transfers.Global != 4 {
		t.Errorf("applied = %+v %d %d", applier.transfers, applier.global, applier.perUser)
	}

	bad := limits.Limits{BandwidthGlobal: -1, BandwidthPerUser: 1 << 41, TransfersGlobal: 10_001, TransfersPerWorkspace: -1, TransfersPerUser: -1, TransfersPerSite: -1,
		MaxSitesPerWorkspace: 1_000_001, MaxTeamsPerUser: -1, MaxMembersPerTeam: -1, DailyBytesPerUser: 1 << 51}
	_, err = svc.Set(ctx, admin, bad)
	if !errors.Is(err, limits.ErrInvalid) || svc.Current() != want {
		t.Fatalf("Set(bad) = %v; current %+v; want ErrInvalid and no change", err, svc.Current())
	}
	for _, name := range []string{"bandwidthGlobal", "bandwidthPerUser", "transfersGlobal", "transfersPerWorkspace", "transfersPerUser", "transfersPerSite", "maxSitesPerWorkspace", "maxTeamsPerUser", "maxMembersPerTeam", "dailyBytesPerUser"} {
		if !contains(err.Error(), name) {
			t.Errorf("the refusal %q does not name %s", err, name)
		}
	}

	store.fail = "SaveLimits"
	if _, err := svc.Set(ctx, admin, defaults); !errors.Is(err, errBoom) || svc.Current() != want {
		t.Errorf("Set() with a failing store = %v; current %+v; want the failure and no change", err, svc.Current())
	}
}

// contains reports whether text has part in it.
func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

// TestService_Quotas verifies the questions the other services ask.
//
// Scenarios:
//   - with no quota nothing is counted and nothing is refused
//   - a quota refuses once the count reaches it and allows below it, for sites, teams, members and the volume of the last day
//   - the volume is counted over the last 24 hours
//   - a failing store is reported by each question
func TestService_Quotas(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	svc, store, _ := setup()
	store.sites, store.teams, store.members, store.bytes = 99, 99, 99, 1<<40
	store.fail = "everything"
	for name, err := range map[string]error{"site": svc.CheckSite(ctx, id), "team": svc.CheckTeam(ctx, id), "member": svc.CheckMember(ctx, id), "transfer": svc.CheckTransfer(ctx, id)} {
		if err != nil {
			t.Errorf("%s without a quota = %v; want allowed", name, err)
		}
	}

	if _, err := svc.Set(ctx, id, limits.Limits{MaxSitesPerWorkspace: 3, MaxTeamsPerUser: 2, MaxMembersPerTeam: 5, DailyBytesPerUser: 1000}); err != nil {
		t.Fatal(err)
	}
	store.fail = ""
	check := func(sites, teams, members int, bytes int64) map[string]error {
		store.sites, store.teams, store.members, store.bytes = sites, teams, members, bytes
		return map[string]error{"site": svc.CheckSite(ctx, id), "team": svc.CheckTeam(ctx, id), "member": svc.CheckMember(ctx, id), "transfer": svc.CheckTransfer(ctx, id)}
	}
	for name, err := range check(2, 1, 4, 999) {
		if err != nil {
			t.Errorf("%s below the quota = %v; want allowed", name, err)
		}
	}
	want := map[string]error{"site": limits.ErrSiteQuota, "team": limits.ErrTeamQuota, "member": limits.ErrMemberQuota, "transfer": limits.ErrDailyQuota}
	for name, err := range check(3, 2, 5, 1000) {
		if !errors.Is(err, want[name]) {
			t.Errorf("%s at the quota = %v; want %v", name, err, want[name])
		}
	}
	if !store.since.Equal(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("the volume was counted since %v; want the last 24 hours", store.since)
	}

	for _, op := range []string{"CountSites", "CountOwnedTeams", "CountMembers", "BytesSince"} {
		store.fail = op
		results := map[string]error{"CountSites": svc.CheckSite(ctx, id), "CountOwnedTeams": svc.CheckTeam(ctx, id), "CountMembers": svc.CheckMember(ctx, id), "BytesSince": svc.CheckTransfer(ctx, id)}
		if !errors.Is(results[op], errBoom) {
			t.Errorf("%s failing = %v; want the failure", op, results[op])
		}
	}
}
