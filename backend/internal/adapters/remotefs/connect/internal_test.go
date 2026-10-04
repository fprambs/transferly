package connect

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// TestAlgorithmsFor verifies the mapping from trusted key types to signature algorithms.
//
// Scenarios:
//   - no trusted keys means no restriction
//   - RSA keys map to their SHA-2 signature algorithms, never to the SHA-1 one
//   - other key types are used as they are, in order
func TestAlgorithmsFor(t *testing.T) {
	if got := algorithmsFor(nil); got != nil {
		t.Errorf("algorithmsFor(nil) = %v; want no restriction", got)
	}
	got := algorithmsFor([]string{"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256"})
	want := []string{"ssh-ed25519", "rsa-sha2-512", "rsa-sha2-256", "ecdsa-sha2-nistp256"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("algorithmsFor() = %v; want %v", got, want)
	}
}

// oneQuick is a store of quick connections that knows one.
type oneQuick struct{ c quick.Connection }

// Lookup returns the connection.
func (o oneQuick) Lookup(uuid.UUID, uuid.UUID, uuid.UUID) (quick.Connection, bool) { return o.c, true }

// TestConnect_UnknownConnector verifies that a connector without an opener is refused without touching the network.
func TestConnect_UnknownConnector(t *testing.T) {
	c := New(nil, oneQuick{quick.Connection{Site: site.Site{Connector: "gopher"}, Secrets: quick.Secrets{}}}, nil, nil, time.Second)

	_, err := c.Connect(t.Context(), connections.Key{}, "session")

	if !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Connect() = %v; want ErrInvalid", err)
	}
}

// TestLocation verifies the zone of listings: a valid name is loaded, and a name that cannot be loaded means UTC.
func TestLocation(t *testing.T) {
	if got := location("America/Santiago").String(); got != "America/Santiago" {
		t.Errorf("location(America/Santiago) = %s", got)
	}
	if got := location("Not/AZone"); got != time.UTC {
		t.Errorf("location(Not/AZone) = %v; want UTC", got)
	}
	if got := location(""); got != time.UTC {
		t.Errorf("location(empty) = %v; want UTC", got)
	}
}

// TestCharsetOf verifies the character sets a site can name.
//
// Scenarios:
//   - every character set of the site settings maps to an encoding that turns an accent into what that set writes
//   - UTF-8, an empty name and an unknown one need no conversion
func TestCharsetOf(t *testing.T) {
	for _, name := range connectors.Charsets {
		enc := charsetOf(name)
		if name == "utf-8" {
			if enc != nil {
				t.Errorf("charsetOf(%q) converts; UTF-8 needs no conversion", name)
			}
			continue
		}
		if enc == nil {
			t.Errorf("charsetOf(%q) = nil; want an encoding", name)
			continue
		}
		if _, err := enc.NewEncoder().String("a"); err != nil {
			t.Errorf("the encoding of %q cannot write a letter: %v", name, err)
		}
	}
	if charsetOf("") != nil || charsetOf("klingon") != nil {
		t.Error("an empty or unknown name must need no conversion")
	}
}
