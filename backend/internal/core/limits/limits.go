// Package limits holds what an administrator caps on the instance: how fast transfers may go, how many run at once, and quotas on
// sites, teams, members and the volume a person moves in a day.
//
// A zero value means no limit. The values the server starts with come from its configuration; an administrator can replace them while it
// runs, and the replacement is stored, applied at once and kept across restarts. The services that enforce a quota ask this package
// before they act, so a change takes effect for the next request and never interrupts what already runs.
package limits

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/ports"
	"github.com/fprambs/sentinel/internal/core/scheduler"
)

const (
	// Day is the window the daily volume of a person is counted over.
	Day = 24 * time.Hour
	// maxConcurrent is the largest number of transfers that may be allowed at once.
	maxConcurrent = 10_000
	// maxCount is the largest quota on things that are counted.
	maxCount = 1_000_000
	// minRate is the smallest bandwidth cap, in bytes per second (1 KiB): below it a single chunk of data would keep a request waiting for hours.
	minRate = 1024
	// maxRate is the largest bandwidth cap, in bytes per second (1 TiB).
	maxRate = 1 << 40
	// maxVolume is the largest daily volume, in bytes (1 PiB).
	maxVolume = 1 << 50
)

// Errors returned by the package.
var (
	// ErrInvalid means a value is negative or larger than the instance accepts.
	ErrInvalid = errors.New("invalid limits")
	// ErrSiteQuota means the workspace already has as many sites as it may.
	ErrSiteQuota = errors.New("site quota reached")
	// ErrTeamQuota means the person already owns as many teams as they may.
	ErrTeamQuota = errors.New("team quota reached")
	// ErrMemberQuota means the team already has as many members, with the invitations that wait, as it may.
	ErrMemberQuota = errors.New("member quota reached")
	// ErrDailyQuota means the person already moved as much as they may in the last day.
	ErrDailyQuota = errors.New("daily transfer quota reached")
)

// Limits are the caps of the instance.
type Limits struct {
	// BandwidthGlobal caps the bytes per second all transfers of the server move together.
	BandwidthGlobal int64 `json:"bandwidthGlobal"`
	// BandwidthPerUser caps the bytes per second the transfers of one person move together.
	BandwidthPerUser int64 `json:"bandwidthPerUser"`
	// TransfersGlobal caps the transfers that run at once on the server.
	TransfersGlobal int `json:"transfersGlobal"`
	// TransfersPerWorkspace caps the transfers that run at once in one workspace.
	TransfersPerWorkspace int `json:"transfersPerWorkspace"`
	// TransfersPerUser caps the transfers that run at once for one person.
	TransfersPerUser int `json:"transfersPerUser"`
	// TransfersPerSite caps the transfers that run at once against one site.
	TransfersPerSite int `json:"transfersPerSite"`
	// MaxSitesPerWorkspace caps the sites a workspace keeps.
	MaxSitesPerWorkspace int `json:"maxSitesPerWorkspace"`
	// MaxTeamsPerUser caps the teams a person owns.
	MaxTeamsPerUser int `json:"maxTeamsPerUser"`
	// MaxMembersPerTeam caps the members of a team, counting the invitations that wait.
	MaxMembersPerTeam int `json:"maxMembersPerTeam"`
	// DailyBytesPerUser caps the bytes a person moves in a day.
	DailyBytesPerUser int64 `json:"dailyBytesPerUser"`
}

// Scheduler returns the caps of concurrent transfers in the form the scheduler takes.
func (l Limits) Scheduler() scheduler.Limits {
	return scheduler.Limits{Global: l.TransfersGlobal, PerWorkspace: l.TransfersPerWorkspace, PerUser: l.TransfersPerUser, PerSite: l.TransfersPerSite}
}

// Validate refuses negative values and values larger than the instance accepts.
func (l Limits) Validate() error {
	within := func(name string, value, top int64) error {
		if value < 0 || value > top {
			return fmt.Errorf("%w: %s must be between 0 and %d", ErrInvalid, name, top)
		}
		return nil
	}
	rate := func(name string, value int64) error {
		if value != 0 && value < minRate {
			return fmt.Errorf("%w: %s must be 0 or at least %d", ErrInvalid, name, minRate)
		}
		return within(name, value, maxRate)
	}
	return errors.Join(
		rate("bandwidthGlobal", l.BandwidthGlobal),
		rate("bandwidthPerUser", l.BandwidthPerUser),
		within("transfersGlobal", int64(l.TransfersGlobal), maxConcurrent),
		within("transfersPerWorkspace", int64(l.TransfersPerWorkspace), maxConcurrent),
		within("transfersPerUser", int64(l.TransfersPerUser), maxConcurrent),
		within("transfersPerSite", int64(l.TransfersPerSite), maxConcurrent),
		within("maxSitesPerWorkspace", int64(l.MaxSitesPerWorkspace), maxCount),
		within("maxTeamsPerUser", int64(l.MaxTeamsPerUser), maxCount),
		within("maxMembersPerTeam", int64(l.MaxMembersPerTeam), maxCount),
		within("dailyBytesPerUser", l.DailyBytesPerUser, maxVolume),
	)
}

// Store persists the limits an administrator set and counts what the quotas are measured against.
type Store interface {
	// Limits returns the limits an administrator stored, and whether there are any.
	Limits(ctx context.Context) (Limits, bool, error)
	// SaveLimits stores the limits in place of the earlier ones.
	SaveLimits(ctx context.Context, limits Limits, by uuid.UUID, at time.Time) error
	// CountSites counts the sites of a workspace.
	CountSites(ctx context.Context, workspaceID uuid.UUID) (int, error)
	// CountOwnedTeams counts the team workspaces a person owns.
	CountOwnedTeams(ctx context.Context, userID uuid.UUID) (int, error)
	// CountMembers counts the members of a workspace plus the invitations that wait and have not expired.
	CountMembers(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error)
	// BytesSince adds up the bytes a person moved in transfers updated since an instant.
	BytesSince(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error)
}

// Applier puts the limits that act on running transfers in force.
type Applier interface {
	// SetLimits changes how many transfers run at once.
	SetLimits(limits scheduler.Limits)
	// SetBandwidth changes the caps on the bytes per second, for the whole server and for each person.
	SetBandwidth(global, perUser int64)
}

// Service keeps the limits in force and answers the quota questions.
type Service struct {
	store    Store
	applier  Applier
	clock    ports.Clock
	defaults Limits

	mu      sync.RWMutex
	current Limits
}

// NewService returns a service whose limits are defaults until [Service.Load] finds stored ones.
func NewService(store Store, applier Applier, defaults Limits, clock ports.Clock) *Service {
	return &Service{store: store, applier: applier, clock: clock, defaults: defaults, current: defaults}
}

// apply puts limits in force.
func (s *Service) apply(l Limits) {
	s.mu.Lock()
	s.current = l
	s.mu.Unlock()
	s.applier.SetLimits(l.Scheduler())
	s.applier.SetBandwidth(l.BandwidthGlobal, l.BandwidthPerUser)
}

// Load puts in force the limits an administrator stored, or the defaults when there are none. It runs when the server starts.
func (s *Service) Load(ctx context.Context) error {
	stored, found, err := s.store.Limits(ctx)
	if err != nil {
		return fmt.Errorf("reading limits: %w", err)
	}
	if !found {
		stored = s.defaults
	}
	s.apply(stored)
	return nil
}

// Current returns the limits in force.
func (s *Service) Current() Limits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Defaults returns the limits the server started with.
func (s *Service) Defaults() Limits { return s.defaults }

// Set validates, stores and puts in force new limits. What already runs keeps running.
func (s *Service) Set(ctx context.Context, by uuid.UUID, l Limits) (Limits, error) {
	if err := l.Validate(); err != nil {
		return Limits{}, err
	}
	if err := s.store.SaveLimits(ctx, l, by, s.clock.Now().UTC()); err != nil {
		return Limits{}, err
	}
	s.apply(l)
	return l, nil
}

// over reports whether a count has reached its quota; a quota of zero means none.
func over(used, quota int) bool { return quota > 0 && used >= quota }

// CheckSite refuses a new site in a workspace that already has as many as it may.
func (s *Service) CheckSite(ctx context.Context, workspaceID uuid.UUID) error {
	quota := s.Current().MaxSitesPerWorkspace
	if quota == 0 {
		return nil
	}
	used, err := s.store.CountSites(ctx, workspaceID)
	if err != nil {
		return err
	}
	if over(used, quota) {
		return ErrSiteQuota
	}
	return nil
}

// CheckTeam refuses a new team to a person who already owns as many as they may.
func (s *Service) CheckTeam(ctx context.Context, userID uuid.UUID) error {
	quota := s.Current().MaxTeamsPerUser
	if quota == 0 {
		return nil
	}
	used, err := s.store.CountOwnedTeams(ctx, userID)
	if err != nil {
		return err
	}
	if over(used, quota) {
		return ErrTeamQuota
	}
	return nil
}

// CheckMember refuses a new invitation to a team whose members and waiting invitations already reach its quota.
func (s *Service) CheckMember(ctx context.Context, workspaceID uuid.UUID) error {
	quota := s.Current().MaxMembersPerTeam
	if quota == 0 {
		return nil
	}
	used, err := s.store.CountMembers(ctx, workspaceID, s.clock.Now().UTC())
	if err != nil {
		return err
	}
	if over(used, quota) {
		return ErrMemberQuota
	}
	return nil
}

// CheckTransfer refuses a new transfer to a person who already moved their volume of the last day.
func (s *Service) CheckTransfer(ctx context.Context, userID uuid.UUID) error {
	quota := s.Current().DailyBytesPerUser
	if quota == 0 {
		return nil
	}
	used, err := s.store.BytesSince(ctx, userID, s.clock.Now().UTC().Add(-Day))
	if err != nil {
		return err
	}
	if used >= quota {
		return ErrDailyQuota
	}
	return nil
}
