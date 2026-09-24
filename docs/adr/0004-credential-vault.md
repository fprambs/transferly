# ADR-0004: Credential vault design

- Status: accepted
- Date: 2026-09-24

## Context

Stored remote credentials must not be recoverable from a stolen database or backup, while the server needs them in clear text to connect.

## Decision

- Master password to a key-encryption key with Argon2id (64 MiB, 3 passes, 1 lane), per-user salt and stored parameters.
- The key wraps a random data-encryption key (DEK); secrets are sealed with AES-256-GCM, random 96-bit nonces and the site, kind and DEK version as associated data.
- Team workspaces share a DEK sealed to each member's X25519 public key; removing a member rotates it.
- DEKs live only in an in-memory key ring with a TTL and inactivity lock.
- A recovery kit is generated at setup; without it a lost master password loses the secrets.

## Consequences

- Positive: database theft alone reveals nothing; changing the master password re-wraps one key.
- Negative: not zero-knowledge, since the running server holds decrypted keys while unlocked.
