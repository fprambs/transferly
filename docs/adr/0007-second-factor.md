# ADR-0007: Second factor: TOTP, recovery codes and passkeys

- Status: accepted
- Date: 2026-09-26

## Context

A password is the only thing between an attacker and the credentials of every workspace a user belongs to. Phase 3 adds teams, so one stolen account now reaches shared servers. [ADR-0005](0005-authentication.md) already promised TOTP and passkeys.

## Decision

- A user may enable a **second factor**: an authenticator app (TOTP, RFC 6238: HMAC-SHA-1, 30 seconds, 6 digits, one step of clock skew either way) and any number of **passkeys** (WebAuthn credentials, used as a second factor and never as the only one). Ten single-use **recovery codes** are created with the first factor.
- Signing in has two steps for users with a second factor. The password step answers `202` with a short-lived challenge instead of a session; the second step answers with the session. The challenge is an HMAC under the instance key of the user and an expiry of five minutes; it proves the password step and nothing else, and has no server state.
- The TOTP secret must be readable by the server at sign-in, when no vault is unlocked, so it is sealed with AES-256-GCM under a key derived from the instance key (HKDF-SHA-256, bound to the user). A stolen database alone reveals no secret. The last accepted time step is stored, so a code cannot be used twice, and a wrong code counts against a per-account limit.
- Recovery codes are random (50 bits each), shown once, stored only as SHA-256 hashes and consumed on use. Regenerating them replaces the whole set.
- Enabling needs a valid code from the app, which proves it was scanned. Disabling, and creating new recovery codes, need the account password again.
- Passkeys: registered and removed from the signed-in account; the relying party is the host of the base URL and the ceremony data is kept in memory for five minutes (single node, as the rest of the in-memory state). Registration and use require user verification when the authenticator offers it.
- Enrolling never weakens anything: a user who loses every factor and every recovery code needs an administrator to disable their second factor (phase 3, administration panel), and that action is audited.

## Consequences

- Positive: a leaked password is no longer enough; nothing new lets an attacker enumerate accounts (the second step is reachable only with a valid challenge); the recovery path is explicit.
- Negative: the challenge is a bearer of "the password was right" for five minutes, so it is bound to nothing but the user; a stolen challenge still needs a code. TOTP secrets are as safe as the instance key: rotating that key invalidates every enrolment.
