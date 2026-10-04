package connectortest_test

import (
	"slices"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/connectortest"
	"github.com/fprambs/transferly/internal/adapters/remotefs/memfs"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/testutil"
)

// full is what a connector with every capability the suite checks declares.
var full = connectors.Capabilities{Folders: true, Permissions: true, SetTimes: true, Resume: true, Rename: true}

// TestMismatches verifies that the suite catches a descriptor that does not tell the truth about its adapter.
//
// Use cases:
//   - matching declarations pass, with every capability or without the optional ones
//   - a capability declared but missing from the adapter fails, as FTP once declared permissions, and so does one the
//     adapter has but the descriptor hides
//   - a connector without real folders or renames, or with server-side copy, is refused until the suite can check it
func TestMismatches(t *testing.T) {
	if got := connectortest.Mismatches(full, remotefs.Capabilities{Chmod: true, Chtimes: true, Resume: true}); got != nil {
		t.Errorf("Mismatches(all) = %v, want none", got)
	}
	if got := connectortest.Mismatches(connectors.Capabilities{Folders: true, Rename: true}, remotefs.Capabilities{Rewrite: true}); got != nil {
		t.Errorf("Mismatches(none) = %v, want none", got)
	}
	for name, tc := range map[string]struct {
		declared connectors.Capabilities
		has      remotefs.Capabilities
		want     string
	}{
		"declared permissions":    {full, remotefs.Capabilities{Chtimes: true, Resume: true}, "declares permissions, which the adapter does not offer"},
		"hidden times":            {connectors.Capabilities{Folders: true, Rename: true, Resume: true}, remotefs.Capabilities{Resume: true, Chtimes: true}, "does not declare setting times, which the adapter offers"},
		"declared resume":         {connectors.Capabilities{Folders: true, Rename: true, Resume: true}, remotefs.Capabilities{}, "declares resumable writes, which the adapter does not offer"},
		"prefixes":                {connectors.Capabilities{Rename: true}, remotefs.Capabilities{}, "has no real folders, which the shared suite cannot check yet"},
		"no rename":               {connectors.Capabilities{Folders: true}, remotefs.Capabilities{}, "cannot rename, which the shared suite cannot check yet"},
		"declared server copying": {connectors.Capabilities{Folders: true, Rename: true, ServerCopy: true}, remotefs.Capabilities{}, "declares server-side copy, which no operation of the port performs yet"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := connectortest.Mismatches(tc.declared, tc.has); !slices.Equal(got, []string{tc.want}) {
				t.Errorf("Mismatches() = %v, want [%s]", got, tc.want)
			}
		})
	}
}

// TestRun verifies the suite end to end with the reference adapter and a descriptor that declares what it has.
//
// Use cases:
//   - an in-memory connector with every capability passes the descriptor, capability and file system checks
func TestRun(t *testing.T) {
	d := connectors.Descriptor{
		Type: "memory", Name: "Memory", Icon: "server", Maturity: connectors.Beta, Capabilities: full,
		Fields: []connectors.Field{{Name: "label", Kind: connectors.Text, Group: connectors.GroupConnection}},
	}
	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	connectortest.Run(t, d, func(t *testing.T) remotefs.RemoteFS { return memfs.New(clock) })
}
