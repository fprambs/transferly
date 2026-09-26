# Workspace keys (crypto/ecdh, crypto/hkdf, AES-GCM)

Sources: `go doc crypto/ecdh`, `go doc crypto/hkdf` (Go 1.24 and later), `go doc crypto/cipher.NewGCM`, RFC 7748 (X25519), RFC 5869 (HKDF) and RFC 9180 (HPKE, for the shape of a seal to a public key). Code: `backend/internal/core/vault/share.go`. Decisions: [ADR-0006](../adr/0006-teams-and-invitations.md).

## Rules that follow from the documentation

- X25519 is reached through `ecdh.X25519()`. `PrivateKey.ECDH` returns an error when the peer's public key has low order (all-zero shared secret), so a hostile key cannot force a predictable secret. Any 32 byte string is a valid X25519 private key; the library clamps it.
- `Curve.GenerateKey` ignores the random source it is given (Go 1.26 and later, `cryptocustomrand`). Sentinel reads the 32 bytes itself from its `io.Reader` and calls `NewPrivateKey`, so the source stays injectable and failures are testable.
- The shared secret is never used as a key. `hkdf.Key(sha256.New, secret, salt, info, 32)` derives it, with the two public keys as salt and the workspace, the member and the key version as `info`.
- The derived key seals with AES-256-GCM (`cipher.NewGCM`), a fresh random 96-bit nonce and the same context as associated data, so a grant cannot be moved to another workspace, member or version.
- A grant is `ephemeral public key (32) || nonce (12) || ciphertext || tag`. Each sealing uses a new ephemeral key: sealing the same workspace key twice gives different bytes, and only the recipient's private key opens it.
- The private key of a user is stored sealed with the user's DEK (`user-key/v1|<user id>`), so the master password protects it like every other secret and changing the password re-wraps one key only.
- Every key that is opened is wiped after use (`vault.Wipe`); shared secrets and derived keys are wiped inside the functions.

## Tests

`TestKeyPair` and `TestGrant`: round trips, another member, workspace, user or version, every byte of a grant changed, short and malformed keys, low-order public keys on both sides, and failing random sources.
