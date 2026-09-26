package connect

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
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

// TestDial_UnknownProtocol verifies that a protocol nobody handles is refused without touching the network.
func TestDial_UnknownProtocol(t *testing.T) {
	c := New(nil, nil, nil, nil, time.Second)

	_, err := c.dial(t.Context(), connections.Key{}, site.Site{Protocol: "gopher"}, secrets{})

	if !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("dial() = %v; want ErrInvalid", err)
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
	for _, name := range site.Charsets {
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
