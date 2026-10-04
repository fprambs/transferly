package connectors_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/testutil"
)

// errStore is what a failing store answers.
var errStore = errors.New("store down")

// memorySettings keeps the settings of the instance in memory, failing when asked to.
type memorySettings struct {
	settings map[string]bool
	by       uuid.UUID
	at       time.Time
	fail     bool
}

// ConnectorSettings implements [connectors.Store].
func (m *memorySettings) ConnectorSettings(context.Context) (map[string]bool, error) {
	if m.fail {
		return nil, errStore
	}
	return m.settings, nil
}

// SetConnector implements [connectors.Store].
func (m *memorySettings) SetConnector(_ context.Context, kind string, enabled bool, by uuid.UUID, at time.Time) error {
	if m.fail {
		return errStore
	}
	m.settings[kind], m.by, m.at = enabled, by, at
	return nil
}

// TestService verifies which connectors the instance offers and who may change it.
//
// Use cases:
//   - without settings, a connector is offered as its descriptor says: SFTP and FTP yes, a beta connector no
//   - an administrator disables SFTP and enables the beta connector, recorded with who and when, and the list follows
//   - asking for a connector by type finds an offered one and refuses a disabled or unknown one, and checking one for a site refuses both
//     as disabled
//   - a member, a disabled administrator, an unknown type and a failing store are refused
func TestService(t *testing.T) {
	ctx := context.Background()
	catalog := connectors.Builtin()
	if err := catalog.Register(sample); err != nil {
		t.Fatal(err)
	}
	store := &memorySettings{settings: map[string]bool{}}
	clock := testutil.NewFakeClock(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	svc := connectors.NewService(catalog, store, clock)
	admin := auth.User{ID: uuid.New(), Role: auth.RoleAdmin}

	offered := func() map[string]bool {
		t.Helper()
		entries, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, e := range entries {
			out[e.Type] = e.Enabled
		}
		return out
	}
	if got := offered(); !got["sftp"] || !got["ftp"] || got["sample"] {
		t.Errorf("offered by default = %v; want sftp and ftp only", got)
	}

	entry, err := svc.SetEnabled(ctx, admin, "sftp", false)
	if err != nil || entry.Type != "sftp" || entry.Enabled {
		t.Fatalf("SetEnabled(sftp, false) = %+v, %v", entry, err)
	}
	if store.by != admin.ID || !store.at.Equal(clock.Now()) {
		t.Errorf("recorded by %s at %s; want the administrator now", store.by, store.at)
	}
	if _, err := svc.SetEnabled(ctx, admin, "sample", true); err != nil {
		t.Fatal(err)
	}
	if got := offered(); got["sftp"] || !got["ftp"] || !got["sample"] {
		t.Errorf("offered after the changes = %v; want ftp and sample", got)
	}

	if d, err := svc.Enabled(ctx, "sample"); err != nil || d.Type != "sample" {
		t.Errorf("Enabled(sample) = %+v, %v", d, err)
	}
	for _, kind := range []string{"sftp", "nope"} {
		if _, err := svc.Enabled(ctx, kind); !errors.Is(err, connectors.ErrUnknown) {
			t.Errorf("Enabled(%s) = %v; want ErrUnknown", kind, err)
		}
	}

	for _, kind := range []string{"sftp", "nope"} {
		if err := svc.CheckConnector(ctx, kind); !errors.Is(err, connectors.ErrDisabled) {
			t.Errorf("CheckConnector(%s) = %v; want ErrDisabled", kind, err)
		}
	}
	if err := svc.CheckConnector(ctx, "ftp"); err != nil {
		t.Errorf("CheckConnector(ftp) = %v; want it offered", err)
	}

	for name, actor := range map[string]auth.User{"member": {ID: uuid.New(), Role: auth.RoleUser}, "disabled admin": {ID: uuid.New(), Role: auth.RoleAdmin, Disabled: true}} {
		if _, err := svc.SetEnabled(ctx, actor, "ftp", false); !errors.Is(err, connectors.ErrForbidden) {
			t.Errorf("SetEnabled() by a %s = %v; want ErrForbidden", name, err)
		}
	}
	if _, err := svc.SetEnabled(ctx, admin, "nope", true); !errors.Is(err, connectors.ErrUnknown) {
		t.Errorf("SetEnabled(nope) = %v; want ErrUnknown", err)
	}
	store.fail = true
	if _, err := svc.List(ctx); !errors.Is(err, errStore) {
		t.Errorf("List() with a failing store = %v", err)
	}
	if err := svc.CheckConnector(ctx, "ftp"); !errors.Is(err, errStore) {
		t.Errorf("CheckConnector() with a failing store = %v", err)
	}
	if _, err := svc.Enabled(ctx, "ftp"); !errors.Is(err, errStore) {
		t.Errorf("Enabled() with a failing store = %v", err)
	}
	if _, err := svc.SetEnabled(ctx, admin, "ftp", true); !errors.Is(err, errStore) {
		t.Errorf("SetEnabled() with a failing store = %v", err)
	}
}
