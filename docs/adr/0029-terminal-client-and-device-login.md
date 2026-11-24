# ADR-0029: Terminal client and device login

- Status: proposed
- Date: 2026-11-24

## Context

People who live in a terminal want to use their self-hosted Transferly without a browser, in the spirit of termscp and termirs:
browse the sites of a workspace, move files between the local disk and a site, and follow the transfer queue. The server already
does every protocol (SFTP, FTP, S3, WebDAV, Google, Azure), the queue, the vault and the host keys; a terminal client only needs
the API.

Today the API only accepts a browser session: an `HttpOnly` cookie that lives 30 minutes idle and 8 hours at most, plus
`X-CSRF-Token` (ADR-0005). A script can imitate a browser, but it has to handle the password and the second factor itself, and the
session ends too soon for a tool that stays open. D3 in the roadmap plans scoped API tokens for later; a terminal client needs
less than that and needs it first.

## Decision

1. **A terminal client, `transferly-tui`, written in Rust with ratatui**, in the `tui/` folder of this repository. It is only an API
   client: no protocol code, no secrets of sites on the user's machine. Its first version has no embedded SSH terminal (ADR-0027
   stays web only), no preview, no archives and no administration.
2. **Device login** following the shape of OAuth 2.0 Device Authorization Grant (RFC 8628), without being an OAuth server:
   - `POST /api/v1/device/authorizations` (anonymous, rate limited) answers a device code, a short user code, the verification URL
     and the polling interval. Codes expire after 10 minutes; only hashes are stored.
   - The person opens `/device` in the browser, already signed in with password and second factor, sees the code, the client name,
     its address and its user agent, and approves or denies it (`POST /api/v1/device/authorizations/{userCode}/approve|deny`,
     session and CSRF).
   - The client polls `POST /api/v1/device/token` and receives `authorization_pending`, `slow_down`, `access_denied`,
     `expired_token`, or a token.
3. **The token is a session of kind `device`** in `user_sessions` (new column `kind`, `browser` by default, and a name). It reuses
   everything a session has: per-session vault key ring (ADR-0004, ADR-0028), revocation, `LogoutAll` when an admin disables a
   user or resets their second factor, and the events stream.
   - It travels only as `Authorization: Bearer`, never as a cookie; a browser session is never accepted as a bearer and the other
     way around.
   - Bearer requests are exempt from `X-CSRF-Token` since nothing sends them on their own; the origin guard stays as it is.
   - Its own lifetimes: `TRANSFERLY_DEVICE_SESSION_IDLE` (default 7 days) and `TRANSFERLY_DEVICE_SESSION_MAX` (default 30 days).
     The vault key keeps the idle time of today; the client asks for the master password again when the server answers
     `vault_locked`.
   - Security in the account lists device sessions with name, last use and address, and revokes them one by one.
   - `TRANSFERLY_DEVICE_LOGIN=false` turns the flow off.
4. **The client keeps the token in the keychain of the system** (`keyring` crate), with a `0600` file as fallback where there is
   none, one profile per server.
5. **One command to install.** Releases build the client for Linux (x86_64, aarch64, static musl), macOS (x86_64, arm64) and
   Windows (x86_64) with cargo-dist, which publishes archives, checksums and the installers
   `curl -LsSf …/transferly-tui-installer.sh | sh` and `irm …/transferly-tui-installer.ps1 | iex`, installing to `$XDG_BIN_HOME`
   or `~/.local/bin` ([spike](../spikes/cargo-dist.md)). The client has the same version as the server and is released with the
   same tag; it reads the API version after login and warns when they differ.

## Consequences

- Positive:
  - the terminal client reuses the server for protocols, queue, vault and permissions; it stays small;
  - the second factor and passkeys keep protecting the account, because approval happens in the browser;
  - device sessions are the base that D3 (scoped API tokens) can build on later.
- Negative:
  - a second language and toolchain (Rust) with its own lint, coverage, audit and library guides;
  - a stolen device token gives the account, without the vault, for up to 30 days unless revoked; revocation and the list of device
    sessions are therefore part of the first version;
  - the user-code flow can be phished like any device flow: the approval page names the client and its address and warns to
    approve only a code the person started;
  - macOS builds need a macOS runner in CI.
