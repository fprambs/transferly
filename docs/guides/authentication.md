# Authentication

Sources: [Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html), [Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html), [CSRF Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html), [x/crypto/argon2](https://pkg.go.dev/golang.org/x/crypto/argon2), [RFC 9106](https://www.rfc-editor.org/rfc/rfc9106).

## Passwords

- Argon2id with at least 19 MiB, 2 passes and 1 lane (OWASP minimum); Sentinel uses the RFC 9106 low-memory profile: 64 MiB, 3 passes, 4 lanes.
- A random 16-byte salt per hash, stored in the PHC string. Bound the cost read from stored hashes.
- Compare in constant time; rehash after login when parameters change.
- Length policy: at least 12 characters, at most 1024 bytes.

## Sessions

- At least 64 bits of entropy are required; Sentinel uses 256 bits, base64url encoded, with no meaning.
- Store only a hash of the identifier; log ids only as hashes.
- Regenerate the identifier on login. Enforce idle and absolute timeouts on the server.
- Cookie attributes: `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` over https.

## CSRF

For a same-origin SPA with a JSON API: require a custom header carrying a session-bound HMAC token, check `Sec-Fetch-Site` and `Origin`, keep `SameSite`, and protect every POST, PUT, PATCH and DELETE. Never mutate on GET.

## Rate limiting and addresses

Limit login per address and per account; count failures for accounts that do not exist. Take the client address from the connection, or from `X-Forwarded-For` only behind configured trusted proxies.

## Tests

Use the real database for handler tests, inject store failures for 500 paths, verify locking deterministically, and validate every response against the OpenAPI contract.
