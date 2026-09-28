# ADR-0010: "Ask for password" logon type

- Status: accepted
- Date: 2026-09-28

## Context

4.8 brings the site form closer to FileZilla's, whose logon types include **Ask for password**: the site is saved with its host and user but no password, and the password is asked for when connecting and kept only until FileZilla closes. People use it for servers whose password must not be written anywhere, and in teams it lets a shared site exist without sharing its password: each member types their own.

Today every password Sentinel keeps is sealed in the vault (ADR-0004) and read by `connect.Connector` when a connection opens, on the server, without the browser being involved. A password that is never stored has to reach the server some other way, live, and stay somewhere between the moment it is typed and the moment the connection pool uses it — including every later connection the pool opens for transfers, listings and reconnects. The only precedent is the quick connection (`core/quick`), whose credentials live in server memory for the session and are never written to the database.

## Decision

**A new authentication method, `ask`.** `site.AuthMethod` gains `ask`, valid for SFTP, FTP and FTPS. A site saved with it has no password secret: saving clears any stored one, the same way the anonymous logon type of 4.8 does. Keys are not covered: a key file is always stored, as in FileZilla, and asking for its passphrase is left for later.

**The password is typed when a connection needs it.** When `connect.Connector` opens a connection to an `ask` site and no password is held for it, it fails with a new stable problem code, `credentials_required` (409, like `vault_locked`). The explorer, the connection test and anything else that connects answers it the way 4.7 answers a locked vault: a dialog opens at once, naming the site, host and user, with a password field and a "Remember until I sign out" box, checked by default like FileZilla's "remember until closed". The dialog sends the password to a new endpoint, `PUT /workspaces/{id}/sites/{id}/credential`, and the caller retries.

**Server memory only, per session.** The password is held in a new in-memory store keyed by the user's **session** and the site, beside the one quick connections use. It is never written to the database, the audit log, the logs or a response, and is overwritten in memory when dropped. It is dropped when the session ends (sign-out, expiry, an administrator disabling the account), when the site is edited or deleted, when the server restarts, and, if "Remember until I sign out" was left unchecked, as soon as the site's connection pool closes (5 minutes idle, or Disconnect from 4.11). Keying by session rather than by user means another browser, or another device, asks again, and a stolen session cookie is the only way to use a password someone else typed.

**Consequences for the rest of the app.**

- Work the server carries on after the request, such as a copy between sites or a sync, runs with the session that started it, so it keeps working while that session holds the password; if the password goes (sign-out, vault lock), the next connection it needs fails with `credentials_required`. Sentinel has no transfers scheduled without a session; if they are added, `ask` sites cannot take part.
- A wrong password is an ordinary `remote_auth_failed`; the held password is dropped so the next attempt asks again. Whether repeated wrong passwords need their own rate limit is checked when this is built.
- The audit trail records that a credential was provided for the site (who, when, from where), never its value.
- Multi-node deployments (phase 5) hold the password on the node that received it only, the same limit quick connections and passkey ceremonies already have; it is recorded with them.

## Alternatives considered

- **Keep the password in the browser** (`sessionStorage`) and send it with each request: rejected. It puts the password within reach of any script that runs on the page and sends it over the wire far more often, while the server still has to hold it for the pool's background connections.
- **Seal it in the vault for the session only**: rejected. It is the Normal logon type with an expiry, still writes the password to the database, and needs an unlocked vault, which is exactly what people choosing this mode want to avoid.
- **Ask on every connection**: rejected. The pool opens and reopens connections in the background for transfers and reconnects; asking each time would interrupt work constantly.

## Review (2026-09-28)

1. No fixed expiry: a password lives until the session ends. Since an idle session ends on its own, the password goes with it; the store keeps no clock of its own beyond dropping entries of sessions that can no longer exist (the absolute session lifetime).
2. Locking the vault drops these passwords too, so "lock" means "nothing of mine can connect", even though they are not stored in the vault.

**Open connections belong to the user, not the session.** The connection pool is keyed by user and site (`connections.Key`), so a second session of the same user could reuse a connection a first session opened with its typed password. The pool therefore asks, before handing out a connection of an `ask` site, whether the session asking holds a password for it; a session that does not gets `credentials_required` like a new connection would.
