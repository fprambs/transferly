# Second factor (RFC 4226, RFC 6238, WebAuthn)

Sources: [RFC 4226](https://www.rfc-editor.org/rfc/rfc4226) (HOTP), [RFC 6238](https://www.rfc-editor.org/rfc/rfc6238) (TOTP), the [Key URI Format](https://github.com/google/google-authenticator/wiki/Key-Uri-Format), [OWASP Multifactor Authentication cheat sheet](https://cheatsheetseries.owasp.org/cheatsheets/Multifactor_Authentication_Cheat_Sheet.html), `go doc crypto/hmac`, `go doc crypto/subtle`, [W3C WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/). Decisions: [ADR-0007](../adr/0007-second-factor.md).

## TOTP

- The code is `HOTP(K, T)` with `T = floor(unix time / 30)`: HMAC-SHA-1 of the 8 byte big-endian counter, dynamic truncation (offset is the low nibble of the last byte, take 31 bits) and the value modulo 10^6, zero padded to 6 digits. Authenticator apps implement exactly this, so SHA-1 is kept for compatibility: the security of HOTP rests on the HMAC, not on SHA-1 collision resistance.
- The shared secret has at least 128 bits; 160 bits (20 random bytes) is the recommended size. It is shown as base32 without padding.
- The provisioning URI is `otpauth://totp/Issuer:account?secret=…&issuer=Issuer&algorithm=SHA1&digits=6&period=30`; the label and the issuer are percent-encoded.
- Accept the current step and one step either side (network and clock skew), compare in constant time with `subtle.ConstantTimeCompare`, and remember the last accepted step so a code works once (RFC 6238 §5.2).
- Codes are digits only, so trim spaces and dashes typed by the user before comparing.
- Throttle failures per account; six digits are guessable without a limit.

## Recovery codes

- Random and single use; at least 40 bits each. Store only a hash. Show them once. Compare hashes in constant time and mark the code used in the same step as the sign-in.

## WebAuthn

- The relying party ID is the registrable host of the site; the origin must match one of the allowed origins exactly. Browsers expose `navigator.credentials` only in secure contexts (https, or `localhost`).
- Every ceremony starts with a fresh random challenge kept on the server; it is single use and expires. The response is verified for challenge, origin, RP ID hash, user presence, and the signature under the stored public key; the signature counter, when the authenticator supports it, must grow.
- Registration excludes the credentials the user already has, so the same authenticator cannot be enrolled twice.

## Testing WebAuthn

`internal/core/passkey/passkeytest` is a software authenticator (ES256, no attestation) that answers the options a server sends the way a browser does, so the ceremonies are tested with real signatures and without a browser. The browser tests use the virtual authenticator of Chromium (`WebAuthn.addVirtualAuthenticator` over the DevTools protocol) and treat the test origin as secure with `--unsafely-treat-insecure-origin-as-secure`.
