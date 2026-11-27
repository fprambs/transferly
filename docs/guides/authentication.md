# Authentication

Sources: [Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html), [CSRF Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html), [x/crypto/argon2](https://pkg.go.dev/golang.org/x/crypto/argon2), [RFC 9106](https://www.rfc-editor.org/rfc/rfc9106), [RFC 7636](https://www.rfc-editor.org/rfc/rfc7636) (PKCE), [RFC 8252](https://www.rfc-editor.org/rfc/rfc8252) (OAuth for native apps).

## Passwords

- Argon2id with at least 19 MiB, 2 passes and 1 lane (OWASP minimum); Transferly uses the RFC 9106 low-memory profile: 64 MiB, 3 passes, 4 lanes.
- A random 16-byte salt per hash, stored in the PHC string. Bound the cost read from stored hashes.
- Compare in constant time; rehash after login when parameters change.
- Length policy: at least 12 characters, at most 1024 bytes.

## Sessions

- At least 64 bits of entropy are required; Transferly uses 256 bits, base64url encoded, with no meaning.
- Store only a hash of the identifier; log ids only as hashes.
- Regenerate the identifier on login. Enforce idle and absolute timeouts on the server.
- Cookie attributes: `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` over https.
- Every session has a kind. Browser sessions travel only in the cookie; sessions of the terminal client only as
  `Authorization: Bearer`. A token sent the other way is anonymous, and a request carrying both credentials is refused with
  `ambiguous_credentials`, so neither can stand in for the other.

## CSRF

For a same-origin SPA with a JSON API: require a custom header carrying a session-bound HMAC token, check `Sec-Fetch-Site` and `Origin`, keep `SameSite`, and protect every POST, PUT, PATCH and DELETE. Never mutate on GET. Bearer requests need no anti-CSRF token, since a browser never
sends one on its own; the origin guard still applies to them.

## Rate limiting and addresses

Limit login per address and per account; count failures for accounts that do not exist. Exchanges of terminal codes are limited per
address, and approvals share the per-account limit of the other password confirmations. Take the client address from the connection, or from `X-Forwarded-For` only behind configured trusted proxies.

## Sessions of the terminal client

[ADR-0029](../adr/0029-terminal-client-and-browser-login.md). `transferly-tui` signs in like a native OAuth app, without Transferly
being an OAuth server:

1. The client makes a PKCE verifier, its S256 challenge and a state, listens on `http://127.0.0.1:<free port>/callback` (or `[::1]`),
   and opens `/cli/authorize?code_challenge=…&code_challenge_method=S256&redirect_uri=…&state=…&client_name=…&client_host=…` in the
   browser. Without a browser or a listener it leaves `redirect_uri` out and shows the address to open elsewhere. The address shown
   and opened is a short link: `POST /api/v1/cli/links` registers the same parameters for 10 minutes under 8 characters without
   lookalikes, and `/cli/a/<code>` loads them (`GET /api/v1/cli/links/{code}`, with a session) into the same page. Servers
   without it answer 404 and the client opens the full address.
2. The page needs a browser session (it sends visitors to `/login?redirect=…` and back), shows what the client declares about itself,
   warns to approve only a sign-in the person started, and asks for the password again: a stolen cookie cannot mint a long session.
3. `POST /api/v1/cli/authorizations` stores the SHA-256 of a 256-bit single-use code with the challenge, the redirect address and the
   account; it lives 5 minutes. The browser goes to the redirect address with `code` and `state`, or shows the code to paste.
   Denying sends `error=access_denied` and `state`, so the page accepts only loopback callbacks, never other sites.
4. `POST /api/v1/cli/token` with the code, the verifier and the same redirect address spends the code on the first attempt, checks
   expiry, address, verifier (in constant time) and that the account is still active, and answers a bearer token. Every refusal is
   `invalid_grant`.
5. Terminal sessions last `TRANSFERLY_CLI_SESSION_IDLE` unused (default `168h`) and `TRANSFERLY_CLI_SESSION_MAX` at most (default
   `720h`); Go durations have no days. Their vault keys keep the idle time of browser sessions. `TRANSFERLY_CLI_LOGIN=false` turns
   approving and exchanging off.
6. `GET /api/v1/me/sessions` lists them for Security and `DELETE /api/v1/me/sessions/{id}` ends one with its vault keys, typed passwords,
   event streams and terminals. Disabling the account ends them too; resetting the second factor ends no session, of either kind.

Codes, tokens, verifiers and passwords never reach the log: the request log records method, path, status, size, duration and request
id only.

## Tests

Use the real database for handler tests, inject store failures for 500 paths, verify locking deterministically, and validate every response against the OpenAPI contract. The terminal sign-in has its own regression net
(`cli_security_test.go`) and an end-to-end test that plays the terminal client with a listener on 127.0.0.1 (`cli-sign-in.spec.ts`).
