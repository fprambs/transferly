package auth_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/auth"
)

// TestCSRF verifies the session-bound anti-CSRF token.
//
// Scenarios:
//   - the token is stable for a session and key, and verifies
//   - a token from another session, another key, a modified token and an empty token are rejected
func TestCSRF(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	session, other := uuid.New(), uuid.New()

	token := auth.CSRFToken(key, session)

	if token != auth.CSRFToken(key, session) || !auth.VerifyCSRF(key, session, token) {
		t.Error("the token is not stable or does not verify")
	}
	if auth.VerifyCSRF(key, other, token) {
		t.Error("a token verified for another session")
	}
	if auth.VerifyCSRF([]byte("another key another key another!"), session, token) {
		t.Error("a token verified under another key")
	}
	if auth.VerifyCSRF(key, session, token+"x") || auth.VerifyCSRF(key, session, "") {
		t.Error("a modified or empty token verified")
	}
}
