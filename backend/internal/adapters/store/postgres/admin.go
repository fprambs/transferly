package postgres

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/store/postgres/db"
	"github.com/fprambs/transferly/internal/core/admin"
	"github.com/fprambs/transferly/internal/core/auth"
)

var _ admin.Store = (*Store)(nil)

// Accounts implements [admin.Store].
func (s *Store) Accounts(ctx context.Context) ([]admin.Account, error) {
	rows, err := s.queries.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]admin.Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, admin.Account{
			ID: r.ID, Email: r.Email, Role: auth.Role(r.Role), Disabled: r.Status == statusDisabled, SecondFactor: r.SecondFactor, CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return out, nil
}

// Account implements [admin.Store].
func (s *Store) Account(ctx context.Context, id uuid.UUID) (admin.Account, error) {
	accounts, err := s.Accounts(ctx)
	if err != nil {
		return admin.Account{}, err
	}
	for _, a := range accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return admin.Account{}, admin.ErrNotFound
}

// changeAdmin runs a change that may take an active administrator away, and refuses it with [admin.ErrLastAdmin] when that administrator is
// the last one. The check and the change happen under a lock that every such change takes, so two administrators cannot each remove the
// other at the same moment and leave the instance with none.
func (s *Store) changeAdmin(ctx context.Context, id uuid.UUID, removes bool, apply func(q *db.Queries) (int64, error)) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if removes {
			if err := q.LockAdminChanges(ctx); err != nil {
				return err
			}
			current, err := q.AccountRoleAndStatus(ctx, id)
			if err != nil && !errors.Is(err, errNoRows) {
				return err
			}
			if err == nil && current.Role == string(auth.RoleAdmin) && current.Status == statusActive {
				n, err := q.CountActiveAdmins(ctx)
				if err != nil {
					return err
				}
				if n <= 1 {
					return admin.ErrLastAdmin
				}
			}
		}
		n, err := apply(q)
		if err == nil && n == 0 {
			return admin.ErrNotFound
		}
		return err
	})
}

// SetDisabled implements [admin.Store].
func (s *Store) SetDisabled(ctx context.Context, id uuid.UUID, disabled bool) error {
	status := statusActive
	if disabled {
		status = statusDisabled
	}
	return s.changeAdmin(ctx, id, disabled, func(q *db.Queries) (int64, error) {
		return q.SetUserStatus(ctx, db.SetUserStatusParams{ID: id, Status: status})
	})
}

// SetAccountRole implements [admin.Store].
func (s *Store) SetAccountRole(ctx context.Context, id uuid.UUID, role auth.Role) error {
	return s.changeAdmin(ctx, id, role != auth.RoleAdmin, func(q *db.Queries) (int64, error) {
		return q.SetUserRole(ctx, db.SetUserRoleParams{ID: id, Role: string(role)})
	})
}

// ActiveAdmins implements [admin.Store].
func (s *Store) ActiveAdmins(ctx context.Context) (int, error) {
	n, err := s.queries.CountActiveAdmins(ctx)
	return int(n), err
}

// ResetSecondFactor implements [admin.Store].
func (s *Store) ResetSecondFactor(ctx context.Context, id uuid.UUID) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.DeleteTotp(ctx, id); err != nil {
			return err
		}
		if err := q.DeleteUserPasskeys(ctx, id); err != nil {
			return err
		}
		return q.DeleteRecoveryCodes(ctx, id)
	})
}

// Networks implements [admin.Store].
func (s *Store) Networks(ctx context.Context) ([]netip.Prefix, error) {
	rows, err := s.queries.ListNetworkAllowances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(rows))
	for _, cidr := range rows {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, errors.New("stored network is not valid: " + cidr)
		}
		out = append(out, prefix)
	}
	return out, nil
}

// ReplaceNetworks implements [admin.Store].
func (s *Store) ReplaceNetworks(ctx context.Context, networks []netip.Prefix, by uuid.UUID, at time.Time) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.DeleteNetworkAllowances(ctx); err != nil {
			return err
		}
		for _, n := range networks {
			if err := q.InsertNetworkAllowance(ctx, db.InsertNetworkAllowanceParams{Cidr: n.String(), CreatedBy: uuid.NullUUID{UUID: by, Valid: by != uuid.Nil}, CreatedAt: at}); err != nil {
				return err
			}
		}
		return nil
	})
}
