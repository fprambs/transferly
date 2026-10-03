package connectors

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/site"
)

// ErrForbidden means the acting account is not an administrator of the instance.
var ErrForbidden = errors.New("administrator required")

// Store keeps which connectors administrators enabled or disabled; a connector without a setting follows its descriptor.
type Store interface {
	// ConnectorSettings returns the stored settings, enabled or not, by type.
	ConnectorSettings(ctx context.Context) (map[string]bool, error)
	// SetConnector stores whether a connector is enabled, who changed it and when.
	SetConnector(ctx context.Context, kind string, enabled bool, by uuid.UUID, at time.Time) error
}

// Entry is a connector of the catalog and whether the instance offers it.
type Entry struct {
	Descriptor
	// Enabled says sites of this connector can be created and connected.
	Enabled bool
}

// Service tells which connectors the instance offers and lets administrators change it (ADR-0019).
type Service struct {
	catalog *Catalog
	store   Store
	clock   ports.Clock
}

// NewService returns a service over a catalog and the settings of the instance.
func NewService(catalog *Catalog, store Store, clock ports.Clock) *Service {
	return &Service{catalog: catalog, store: store, clock: clock}
}

// List returns every connector of the catalog with whether it is enabled: as an administrator set it, or as its descriptor says.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	settings, err := s.store.ConnectorSettings(ctx)
	if err != nil {
		return nil, err
	}
	all := s.catalog.All()
	entries := make([]Entry, len(all))
	for i, d := range all {
		enabled, set := settings[d.Type]
		entries[i] = Entry{Descriptor: d, Enabled: enabled || (!set && d.EnabledByDefault)}
	}
	return entries, nil
}

// Enabled returns the connector of a type when the instance offers it, and [ErrUnknown] when it does not exist or is disabled.
func (s *Service) Enabled(ctx context.Context, kind string) (Descriptor, error) {
	entries, err := s.List(ctx)
	if err != nil {
		return Descriptor{}, err
	}
	for _, e := range entries {
		if e.Type == kind && e.Enabled {
			return e.Descriptor, nil
		}
	}
	return Descriptor{}, ErrUnknown
}

// CheckProtocol implements [site.Offering]: it refuses a protocol whose connector the instance does not offer with
// [site.ErrConnectorDisabled]. FTP and FTPS sites both use the FTP connector.
func (s *Service) CheckProtocol(ctx context.Context, protocol site.Protocol) error {
	kind := string(protocol)
	if protocol == site.FTPS {
		kind = string(site.FTP)
	}
	_, err := s.Enabled(ctx, kind)
	if errors.Is(err, ErrUnknown) {
		return site.ErrConnectorDisabled
	}
	return err
}

// SetEnabled enables or disables a connector for the whole instance. Only administrators may; sites of a disabled connector stay stored
// but cannot connect.
func (s *Service) SetEnabled(ctx context.Context, actor auth.User, kind string, enabled bool) (Entry, error) {
	if actor.Role != auth.RoleAdmin || actor.Disabled {
		return Entry{}, ErrForbidden
	}
	d, err := s.catalog.Lookup(kind)
	if err != nil {
		return Entry{}, err
	}
	if err := s.store.SetConnector(ctx, kind, enabled, actor.ID, s.clock.Now()); err != nil {
		return Entry{}, err
	}
	return Entry{Descriptor: d, Enabled: enabled}, nil
}
