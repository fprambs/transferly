package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"github.com/google/uuid"
)

// CSRFToken returns the anti-CSRF token of a session: an HMAC of the session id under the instance key.
// It is bound to the session, so a token from one session is useless in another.
func CSRFToken(instanceKey []byte, session uuid.UUID) string {
	mac := hmac.New(sha256.New, instanceKey)
	mac.Write([]byte("sentinel/csrf/v1|"))
	mac.Write(session[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyCSRF reports whether token is the valid anti-CSRF token of the session, in constant time.
func VerifyCSRF(instanceKey []byte, session uuid.UUID, token string) bool {
	return hmac.Equal([]byte(CSRFToken(instanceKey, session)), []byte(token))
}
