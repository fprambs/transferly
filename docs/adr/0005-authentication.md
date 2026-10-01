# ADR-0005: Authentication, sessions and CSRF

- Status: accepted
- Date: 2026-09-24

## Context

Transferly guards credentials for remote servers, so account takeover is the main risk. Guidance comes from the OWASP Password Storage, Session Management and CSRF Prevention cheat sheets.

## Decision

- Login passwords use Argon2id in the PHC string format with the RFC 9106 low-memory profile (64 MiB, 3 passes, 4 lanes). Hashes are rehashed after a successful login when the parameters change. Concurrent derivations are capped, since each allocates its full memory cost.
- Unknown accounts cost the same as wrong passwords: a placeholder hash is verified, and the error is identical.
- Sessions are opaque 256-bit random tokens in an `HttpOnly`, `SameSite=Lax` cookie (`Secure` when the base URL is https). Only the SHA-256 of the token is stored. Each login creates a new token; sessions expire after 30 minutes idle and 8 hours absolute by default.
- CSRF: state-changing requests must not be marked cross-site by Fetch Metadata, must come from an allowed origin when `Origin` is present, and must carry `X-CSRF-Token`, an HMAC of the session id under the instance key.
- Rate limits use in-memory token buckets with bounded memory: per client address and per account for login, per address for setup.
- The client address is the connection address unless the connection comes from a configured trusted proxy, then the forwarded address; `RealIP` is never used.
- The first administrator is created through a wizard that works only while no account exists, serialized by a database advisory lock.
- Which operations require a session comes from the OpenAPI contract, not from a route list.

## Consequences

- Positive: database theft yields neither passwords nor usable session tokens; stateless anti-CSRF tokens need no storage; the contract drives protection.
- Negative: limits are per process, so a multi-node deployment needs a shared limiter; sessions need a database read per request.
