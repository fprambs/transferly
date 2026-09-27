// Package admin implements what an administrator of the instance does: seeing the accounts, disabling and enabling them, changing who is an
// administrator, removing the second factor of somebody who lost it, and choosing which private networks saved sites may reach.
//
// Every operation takes the acting account and refuses it unless it is an administrator, so the check does not depend on the caller.
// The rules keep the instance from locking itself out: nobody changes their own account here and the last active administrator stays.
package admin

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/limits"
	"github.com/fprambs/sentinel/internal/core/ports"
)

// MaxNetworks is how many networks an administrator may allow.
const MaxNetworks = 100

// Errors returned by the package.
var (
	// ErrForbidden means the acting account is not an administrator.
	ErrForbidden = errors.New("administrator required")
	// ErrNotFound means the account does not exist.
	ErrNotFound = errors.New("account not found")
	// ErrSelf means the administrator tried to change their own account, which they cannot do here.
	ErrSelf = errors.New("cannot change your own account")
	// ErrLastAdmin means the change would leave the instance without an active administrator.
	ErrLastAdmin = errors.New("last administrator")
	// ErrInvalidRole means the role is not one of the roles of an account.
	ErrInvalidRole = errors.New("invalid role")
	// ErrInvalidNetwork means an entry is not an address or a network, is too wide or overlaps a range that is never allowed.
	ErrInvalidNetwork = errors.New("invalid network")
	// ErrTooManyNetworks means the list is longer than [MaxNetworks].
	ErrTooManyNetworks = errors.New("too many networks")
)

// never lists the ranges no administrator may open: the "this network" block, link-local addresses with the cloud metadata service, multicast
// and reserved blocks. The servers a user reaches never live there, and they are where the attacks on the host go.
var never = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("::/128"), netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// Account is an account as an administrator sees it.
type Account struct {
	// ID identifies the account.
	ID uuid.UUID
	// Email is its address.
	Email string
	// Role is its role in the instance.
	Role auth.Role
	// Disabled tells whether it cannot sign in.
	Disabled bool
	// SecondFactor tells whether it has an authenticator app or a passkey.
	SecondFactor bool
	// CreatedAt is when it was created, in UTC.
	CreatedAt time.Time
}

// Networks is the private networks saved sites may reach.
type Networks struct {
	// Fixed are those the server was started with, which only a restart changes.
	Fixed []netip.Prefix
	// Custom are those an administrator allowed, which change while the server runs.
	Custom []netip.Prefix
}

// Store persists what administrators change.
type Store interface {
	// Accounts lists every account, oldest first.
	Accounts(ctx context.Context) ([]Account, error)
	// Account returns one account, or [ErrNotFound].
	Account(ctx context.Context, id uuid.UUID) (Account, error)
	// SetDisabled disables or enables an account, or returns [ErrNotFound].
	SetDisabled(ctx context.Context, id uuid.UUID, disabled bool) error
	// SetAccountRole changes the role of an account, or returns [ErrNotFound].
	SetAccountRole(ctx context.Context, id uuid.UUID, role auth.Role) error
	// ActiveAdmins counts the administrators who are not disabled.
	ActiveAdmins(ctx context.Context) (int, error)
	// ResetSecondFactor removes the authenticator app, the passkeys and the recovery codes of an account, in one step.
	ResetSecondFactor(ctx context.Context, id uuid.UUID) error
	// DeleteUserSessions removes every session of an account and returns how many.
	DeleteUserSessions(ctx context.Context, id uuid.UUID) (int, error)
	// Networks returns the networks an administrator allowed.
	Networks(ctx context.Context) ([]netip.Prefix, error)
	// ReplaceNetworks stores the networks an administrator allows in place of the earlier ones.
	ReplaceNetworks(ctx context.Context, networks []netip.Prefix, by uuid.UUID, at time.Time) error
}

// Policy is the network policy the servers of users are reached under.
type Policy interface {
	// Fixed returns the networks the policy was started with.
	Fixed() []netip.Prefix
	// Extra returns the networks allowed while the server runs.
	Extra() []netip.Prefix
	// SetExtra replaces the networks allowed while the server runs.
	SetExtra(extra []netip.Prefix)
}

// Limiter keeps the caps and quotas of the instance.
type Limiter interface {
	// Current returns the limits in force.
	Current() limits.Limits
	// Defaults returns the limits the server started with.
	Defaults() limits.Limits
	// Set validates, stores and puts in force new limits.
	Set(ctx context.Context, by uuid.UUID, l limits.Limits) (limits.Limits, error)
}

// Service implements the use cases of an administrator.
type Service struct {
	store   Store
	policy  Policy
	limiter Limiter
	clock   ports.Clock
}

// NewService returns a service.
func NewService(store Store, policy Policy, limiter Limiter, clock ports.Clock) *Service {
	return &Service{store: store, policy: policy, limiter: limiter, clock: clock}
}

// LimitsView is the limits in force next to the ones the server started with, so an administrator can see what a restore would do.
type LimitsView struct {
	// Current are the limits in force.
	Current limits.Limits
	// Defaults are the limits the server started with.
	Defaults limits.Limits
}

// Limits returns the caps and quotas of the instance.
func (s *Service) Limits(actor auth.User) (LimitsView, error) {
	if err := require(actor); err != nil {
		return LimitsView{}, err
	}
	return LimitsView{Current: s.limiter.Current(), Defaults: s.limiter.Defaults()}, nil
}

// SetLimits replaces the caps and quotas of the instance. They apply to the next request; what already runs keeps running.
func (s *Service) SetLimits(ctx context.Context, actor auth.User, l limits.Limits) (LimitsView, error) {
	if err := require(actor); err != nil {
		return LimitsView{}, err
	}
	if _, err := s.limiter.Set(ctx, actor.ID, l); err != nil {
		return LimitsView{}, err
	}
	return LimitsView{Current: s.limiter.Current(), Defaults: s.limiter.Defaults()}, nil
}

// require refuses an account that is not an active administrator.
func require(actor auth.User) error {
	if actor.Role != auth.RoleAdmin || actor.Disabled {
		return ErrForbidden
	}
	return nil
}

// Load applies the networks stored by administrators to the policy. It runs when the server starts.
func (s *Service) Load(ctx context.Context) error {
	stored, err := s.store.Networks(ctx)
	if err != nil {
		return fmt.Errorf("reading allowed networks: %w", err)
	}
	s.policy.SetExtra(stored)
	return nil
}

// Users lists every account.
func (s *Service) Users(ctx context.Context, actor auth.User) ([]Account, error) {
	if err := require(actor); err != nil {
		return nil, err
	}
	return s.store.Accounts(ctx)
}

// other returns the target account when it is not the actor's own.
func (s *Service) other(ctx context.Context, actor auth.User, id uuid.UUID) (Account, error) {
	if err := require(actor); err != nil {
		return Account{}, err
	}
	if id == actor.ID {
		return Account{}, ErrSelf
	}
	return s.store.Account(ctx, id)
}

// lastAdmin tells whether the account is the only active administrator.
func (s *Service) lastAdmin(ctx context.Context, target Account) (bool, error) {
	if target.Role != auth.RoleAdmin || target.Disabled {
		return false, nil
	}
	n, err := s.store.ActiveAdmins(ctx)
	return n <= 1, err
}

// SetDisabled disables or enables another account. Disabling ends its sessions, so it stops at once.
func (s *Service) SetDisabled(ctx context.Context, actor auth.User, id uuid.UUID, disabled bool) (Account, error) {
	target, err := s.other(ctx, actor, id)
	if err != nil {
		return Account{}, err
	}
	if disabled {
		if last, err := s.lastAdmin(ctx, target); err != nil || last {
			return Account{}, orLastAdmin(err)
		}
	}
	if err := s.store.SetDisabled(ctx, id, disabled); err != nil {
		return Account{}, err
	}
	if disabled {
		if _, err := s.store.DeleteUserSessions(ctx, id); err != nil {
			return Account{}, err
		}
	}
	target.Disabled = disabled
	return target, nil
}

// orLastAdmin returns err, or [ErrLastAdmin] when there is none.
func orLastAdmin(err error) error {
	if err != nil {
		return err
	}
	return ErrLastAdmin
}

// SetRole makes another account an administrator or a regular account. The last active administrator cannot be demoted.
func (s *Service) SetRole(ctx context.Context, actor auth.User, id uuid.UUID, role auth.Role) (Account, error) {
	if role != auth.RoleAdmin && role != auth.RoleUser {
		return Account{}, ErrInvalidRole
	}
	target, err := s.other(ctx, actor, id)
	if err != nil {
		return Account{}, err
	}
	if role == auth.RoleUser {
		if last, err := s.lastAdmin(ctx, target); err != nil || last {
			return Account{}, orLastAdmin(err)
		}
	}
	if err := s.store.SetAccountRole(ctx, id, role); err != nil {
		return Account{}, err
	}
	target.Role = role
	return target, nil
}

// ResetSecondFactor removes the second factor of another account, for somebody who lost their device and their recovery codes. The next
// sign-in asks for the password only, and the account can turn the second factor on again from its settings.
func (s *Service) ResetSecondFactor(ctx context.Context, actor auth.User, id uuid.UUID) (Account, error) {
	target, err := s.other(ctx, actor, id)
	if err != nil {
		return Account{}, err
	}
	if err := s.store.ResetSecondFactor(ctx, id); err != nil {
		return Account{}, err
	}
	target.SecondFactor = false
	return target, nil
}

// Networks returns the networks saved sites may reach beyond the public internet. The ones an administrator allowed are read from the
// store, where every instance of the server finds them.
func (s *Service) Networks(ctx context.Context, actor auth.User) (Networks, error) {
	if err := require(actor); err != nil {
		return Networks{}, err
	}
	custom, err := s.store.Networks(ctx)
	if err != nil {
		return Networks{}, err
	}
	return Networks{Fixed: s.policy.Fixed(), Custom: sorted(custom)}, nil
}

// sorted orders networks by address and then by length, IPv4 before IPv6, so every answer lists them the same way.
func sorted(networks []netip.Prefix) []netip.Prefix {
	out := slices.Clone(networks)
	slices.SortFunc(out, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	return out
}

// ParseNetworks reads addresses and networks written as text, such as `192.168.1.10` or `10.20.0.0/16`, and returns them masked, without
// repeats and sorted. Ranges that are never allowed and networks wider than /8 (IPv4) or /7 (IPv6) are refused.
func ParseNetworks(entries []string) ([]netip.Prefix, error) {
	if len(entries) > MaxNetworks {
		return nil, ErrTooManyNetworks
	}
	seen := make(map[netip.Prefix]bool, len(entries))
	out := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		prefix, err := parseNetwork(strings.TrimSpace(entry))
		if err != nil {
			return nil, err
		}
		if !seen[prefix] {
			seen[prefix] = true
			out = append(out, prefix)
		}
	}
	return sorted(out), nil
}

// parseNetwork reads one address or network and applies the limits of [ParseNetworks].
func parseNetwork(entry string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(entry)
	if err != nil {
		addr, addrErr := netip.ParseAddr(entry)
		if addrErr != nil {
			return netip.Prefix{}, fmt.Errorf("%w: %q", ErrInvalidNetwork, entry)
		}
		prefix = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
	}
	prefix = prefix.Masked()
	minBits := 8
	if prefix.Addr().Is6() {
		minBits = 7
	}
	if prefix.Bits() < minBits {
		return netip.Prefix{}, fmt.Errorf("%w: %q", ErrInvalidNetwork, entry)
	}
	for _, banned := range never {
		if prefix.Overlaps(banned) {
			return netip.Prefix{}, fmt.Errorf("%w: %q", ErrInvalidNetwork, entry)
		}
	}
	return prefix, nil
}

// SetNetworks replaces the networks an administrator allows. They are stored and take effect for the next connection.
func (s *Service) SetNetworks(ctx context.Context, actor auth.User, entries []string) (Networks, error) {
	if err := require(actor); err != nil {
		return Networks{}, err
	}
	networks, err := ParseNetworks(entries)
	if err != nil {
		return Networks{}, err
	}
	if err := s.store.ReplaceNetworks(ctx, networks, actor.ID, s.clock.Now().UTC()); err != nil {
		return Networks{}, err
	}
	s.policy.SetExtra(networks)
	return Networks{Fixed: s.policy.Fixed(), Custom: s.policy.Extra()}, nil
}
