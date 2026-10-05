package sftpfs

import (
	"os/exec"
	"strings"
	"testing"
)

// TestQuote verifies that a POSIX shell reads every quoted value back as it is, whatever it holds.
//
// Use cases:
//   - quotes, spaces, substitutions, separators, newlines, globs, backslashes and leading dashes stay one literal word
func TestQuote(t *testing.T) {
	for _, value := range []string{"plain", "with space", "it's", "''", `$(id)`, "`id`", "a;b", "a\nb", "*", `back\slash`, `"x"`, "-rf", "$HOME", "a&&b|c>d"} {
		out, err := exec.CommandContext(t.Context(), "sh", "-c", "printf %s "+quote(value)).Output()
		if err != nil || string(out) != value {
			t.Errorf("sh read quote(%q) as %q, %v", value, out, err)
		}
	}
}

// TestCapped verifies that output beyond the cap is dropped while writes still succeed.
func TestCapped(t *testing.T) {
	c := &capped{limit: 4}
	for _, chunk := range []string{"ab", "cdef", "gh"} {
		if n, err := c.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Errorf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	if c.buf.String() != "abcd" {
		t.Errorf("kept %q, want abcd", c.buf.String())
	}
}

// TestCommandError verifies the message of a failed command and that failures other than commands keep their error.
func TestCommandError(t *testing.T) {
	if got := (&CommandError{Status: 2, Stderr: " oops\n"}).Error(); got != "the command exited with status 2: oops" {
		t.Errorf("Error() = %q", got)
	}
	if got := commandError("copy", "/a", exec.ErrNotFound); !strings.Contains(got.Error(), exec.ErrNotFound.Error()) {
		t.Errorf("commandError(other) = %v", got)
	}
}
