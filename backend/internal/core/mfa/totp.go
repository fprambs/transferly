package mfa

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA-1; its security does not depend on collision resistance.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// SecretLength is the size in bytes of a TOTP secret: 160 bits, the size RFC 4226 recommends.
	SecretLength = 20
	// Period is how many seconds a code is valid for.
	Period = 30
	digits = 6
	// Skew is how many steps either side of the current one a code is still accepted.
	skew = 1
)

// secretEncoding renders secrets as base32 without padding, as authenticator apps expect.
var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// EncodeSecret renders a secret as base32.
func EncodeSecret(secret []byte) string { return secretEncoding.EncodeToString(secret) }

// Step returns the time step a moment falls in.
func Step(at time.Time) int64 { return at.Unix() / Period }

// code computes the HOTP value of a secret for a counter (RFC 4226).
func code(secret []byte, counter int64) string {
	mac := hmac.New(sha1.New, secret)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter)) //nolint:gosec // counters are non-negative times.
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", digits, value%1_000_000)
}

// Code returns the code an authenticator shows for a secret at a moment.
func Code(secret []byte, at time.Time) string { return code(secret, Step(at)) }

// Verify checks a code against the current step and one step either side. It returns the step the code belongs to, which the caller stores
// to refuse the same code twice, and whether it matched. The comparison takes the same time whatever the digits.
func Verify(secret []byte, given string, at time.Time) (int64, bool) {
	given = strings.NewReplacer(" ", "", "-", "").Replace(given)
	if len(given) != digits {
		return 0, false
	}
	matched := int64(-1)
	for step := Step(at) - skew; step <= Step(at)+skew; step++ {
		if subtle.ConstantTimeCompare([]byte(code(secret, step)), []byte(given)) == 1 {
			matched = step
		}
	}
	return matched, matched >= 0
}

// URI returns the otpauth address an authenticator app scans as a QR code to add the account.
func URI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	query := url.Values{"secret": {EncodeSecret(secret)}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + query.Encode()
}
