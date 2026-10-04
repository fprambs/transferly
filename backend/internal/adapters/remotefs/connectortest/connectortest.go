// Package connectortest is the conformance suite every connector of the catalog must pass (ADR-0019): its descriptor is
// valid, the capabilities it declares are the ones its adapter has, and the adapter passes the shared [remotefstest] suite.
package connectortest

import (
	"fmt"
	"testing"

	"github.com/fprambs/transferly/internal/adapters/remotefs/remotefstest"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// Mismatches compares the capabilities a descriptor declares with those a connection of its adapter reports, against a
// reference server that supports everything the protocol can do, and describes every difference:
//
//   - permissions, times that can be set and resumable writes must be declared exactly when the adapter has them, since
//     the interface offers them by the declaration and the adapter is what performs them;
//   - real folders and renames are exercised by the shared suite, which cannot check a connector without them yet;
//   - server-side copy has no operation in the port yet, so no connector can honor it.
func Mismatches(declared connectors.Capabilities, has remotefs.Capabilities) []string {
	var out []string
	for _, c := range []struct {
		name          string
		declared, has bool
	}{
		{"permissions", declared.Permissions, has.Chmod},
		{"setting times", declared.SetTimes, has.Chtimes},
		{"resumable writes", declared.Resume, has.Resume},
	} {
		switch {
		case c.declared && !c.has:
			out = append(out, fmt.Sprintf("declares %s, which the adapter does not offer", c.name))
		case !c.declared && c.has:
			out = append(out, fmt.Sprintf("does not declare %s, which the adapter offers", c.name))
		}
	}
	if !declared.Folders {
		out = append(out, "has no real folders, which the shared suite cannot check yet")
	}
	if !declared.Rename {
		out = append(out, "cannot rename, which the shared suite cannot check yet")
	}
	if declared.ServerCopy {
		out = append(out, "declares server-side copy, which no operation of the port performs yet")
	}
	return out
}

// Run executes the conformance suite of a connector. open returns a new, empty file system of the connector, reached the
// way Transferly reaches it, on a reference server that supports everything the protocol can do; the suite closes it.
//
// Scenarios:
//   - the descriptor passes [connectors.Descriptor.Check]
//   - the capabilities it declares are those its adapter reports, as [Mismatches] describes
//   - the adapter passes [remotefstest.Run]
func Run(t *testing.T, d connectors.Descriptor, open remotefstest.Factory) {
	t.Helper()
	t.Run("descriptor is valid", func(t *testing.T) {
		if err := d.Check(); err != nil {
			t.Error(err)
		}
	})
	t.Run("capabilities match the adapter", func(t *testing.T) {
		fsys := open(t)
		t.Cleanup(func() { _ = fsys.Close() })
		for _, mismatch := range Mismatches(d.Capabilities, fsys.Capabilities()) {
			t.Errorf("%s %s", d.Type, mismatch)
		}
	})
	t.Run("remote file system", func(t *testing.T) { remotefstest.Run(t, open) })
}
