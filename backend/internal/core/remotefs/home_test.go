package remotefs_test

import (
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestHomeOf verifies how the working folder a server answers is read.
//
// Use cases:
//   - an absolute path is kept, cleaned
//   - an empty or relative answer means the root
func TestHomeOf(t *testing.T) {
	for answer, want := range map[string]string{"/home/deploy": "/home/deploy", "/srv//www/": "/srv/www", "/": "/", "": "/", "home/deploy": "/"} {
		if got := remotefs.HomeOf(answer); got != want {
			t.Errorf("HomeOf(%q) = %q, want %q", answer, got, want)
		}
	}
}
