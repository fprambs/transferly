package connect

import (
	"fmt"
	"testing"
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
