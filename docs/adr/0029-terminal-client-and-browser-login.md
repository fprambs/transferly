# ADR-0029: Terminal client and sign-in through the browser

- Status: accepted
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

Signing in should feel like Claude Code: start the client, give the address of the server, approve in the browser, and be in. The
OAuth 2.0 device flow (RFC 8628) was considered first; it needs polling and a short code the person types, which is the part of that
flow most exposed to phishing. Native apps are better served by the authorization code flow with PKCE and a loopback redirect
(RFC 7636, RFC 8252), which also covers machines without a browser by showing the code to paste.

## Decision

1. **A terminal client, `transferly-tui`, written in Rust with ratatui**, in the `tui/` folder of this repository. It is only an API
   client: no protocol code, no secrets of sites on the user's machine. Its first version has no embedded SSH terminal (ADR-0027
   stays web only), no preview, no archives and no administration.
2. **Sign-in through the browser, with the shape of the OAuth 2.0 authorization code flow with PKCE for native apps**, without
   making Transferly an OAuth server:
   - Started without a saved session, the client asks for the address of the server inside the interface and checks that it is a
     Transferly. `transferly-tui login <url>` does the same for scripts.
   - The client makes a `code_verifier`, its S256 challenge and a `state`, listens on `http://127.0.0.1:<free port>/callback`, and
     opens `<server>/cli/authorize` with the challenge, the state, that redirect address and its name and host name.
   - The page needs a browser session, so the person signs in first if needed, with password, second factor or passkey. It shows
     the client name and host name as the client declared them, warns to approve only a sign-in the person just started, and waits
     for an explicit Approve or Deny; nothing is approved without a click. **Approving asks for the account password again**, as
     turning off the second factor does: otherwise a stolen browser cookie, which lives 8 hours at most, could mint a terminal
     session of 30 days.
   - Approving calls `POST /api/v1/cli/authorizations` (session, CSRF and password), which stores the challenge, the redirect address and the
     user with a code that is single use, lives 5 minutes and is kept only as a hash. The page sends the browser to the redirect
     address with the code and the state.
   - **Without a browser on the machine**, such as over SSH, or when the client cannot listen, it shows the address instead, the
     person opens it on another device, and the page shows the code to paste into the terminal, with a warning never to give it to
     anyone. A code without the verifier that only the client holds is useless.
   - The client checks the state and calls `POST /api/v1/cli/token` (anonymous, rate limited) with the code, the verifier and the
     redirect address, and receives the token. Wrong, used or expired codes and verifiers that do not match are one error,
     `invalid_grant`.
   - Redirect addresses are only `http://127.0.0.1:<port>/callback` and `http://[::1]:<port>/callback`, any port, or none for the
     code to paste.
3. **The token is a session of kind `cli`** in `user_sessions` (new column `kind`, `browser` by default, and a name). It reuses
   everything a session has: per-session vault key ring (ADR-0004, ADR-0028), revocation, `LogoutAll` when an admin disables a
   user or resets their second factor, and the events stream.
   - It travels only as `Authorization: Bearer`, never as a cookie; a browser session is never accepted as a bearer and the other
     way around.
   - Bearer requests are exempt from `X-CSRF-Token` since nothing sends them on their own; the origin guard stays as it is.
   - Its own lifetimes: `TRANSFERLY_CLI_SESSION_IDLE` (default 7 days) and `TRANSFERLY_CLI_SESSION_MAX` (default 30 days).
     The vault key keeps the idle time of today; the client asks for the master password again when the server answers
     `vault_locked`.
   - Security in the account lists terminal sessions with name, last use and address, and revokes them one by one.
   - `TRANSFERLY_CLI_LOGIN=false` turns the flow off.
4. **The client keeps the token in the keychain of the system** (`keyring` crate), with a `0600` file as fallback where there is
   none, one profile per server.
5. **One command to install.** Releases build the client for Linux (x86_64, aarch64, static musl), macOS (x86_64, arm64) and
   Windows (x86_64) with cargo-dist, which publishes archives, checksums and the installers
   `curl -LsSf …/transferly-tui-installer.sh | sh` and `irm …/transferly-tui-installer.ps1 | iex`, installing to `$XDG_BIN_HOME`
   or `~/.local/bin` ([spike](../spikes/cargo-dist.md)).
6. **Same version as the server, compatible across versions.** The client is released with the server's tag `vX.Y.Z` and the same
   version; one `scripts/release.sh` run releases both. A client does not demand the exact version of its server:
   - after signing in it reads `GET /api/v1/version` and shows a notice when the minor versions differ;
   - it finds what the server offers by using it: an endpoint the server lacks answers `404`, and the client explains that this
     needs a newer server and disables that action instead of failing. Only a server without the sign-in of point 2 cannot be used
     at all, and the login screen says so.
7. **The client finds and installs its own updates**, with the `axoupdater` library and the install receipt the installer writes:
   - At start, in the background and at most once a day, it asks GitHub for the latest release, remembering the answer and when it
     asked in its configuration folder. The check never delays the interface, fails silently without network, and contacts only
     github.com, never the person's server.
   - A newer release shows a quiet line in the status bar ("v1.5.0 available, U to update"). When the person's server is older than
     that release, the line says so, since some features of the new client would need the newer server.
   - **U** asks for confirmation, then leaves the interface (restoring the terminal), runs the installer of the new version through
     `axoupdater`, prints the result and asks to start the client again. Nothing is downloaded or replaced without that
     confirmation.
   - `transferly-tui update` does the same from the command line, with `--version X.Y.Z` to install a given release and
     `--match-server` to install the version of the server the profile points to.
   - Without an install receipt (installed by `cargo install`, a package manager or by hand) the client only tells that a release
     exists and how it was installed, and never replaces the binary itself.
   - `check_updates = false` in the configuration, or `TRANSFERLY_TUI_NO_UPDATE_CHECK=1`, turns the check off, for machines without
     internet access and for packagers.

## Consequences

- Positive:
  - the terminal client reuses the server for protocols, queue, vault and permissions; it stays small;
  - signing in is one approval in the browser, and the second factor and passkeys keep protecting the account;
  - one approval page serves both the loopback redirect and the code to paste; the server needs no polling;
  - terminal sessions are the base that D3 (scoped API tokens) can build on later;
  - one version for server and client keeps compatibility simple, and the client still works with servers of nearby versions;
  - people learn about new releases inside the client and update with one key, but nothing changes without their confirmation.
- Negative:
  - a second language and toolchain (Rust) with its own lint, coverage, audit and library guides;
  - a stolen terminal token gives the account, without the vault, for up to 30 days unless revoked; revocation and the list of
    terminal sessions are therefore part of the first version;
  - someone can send a person a link to the approval page made with their own challenge and ask for the pasted code. The page names
    the client and its host, asks for a click, and warns never to share the code; with a loopback redirect the code only reaches
    the person's own machine;
  - macOS builds need a macOS runner in CI;
  - a fix only in the client still makes a release of the server, whose image may not change;
  - every feature of the client must handle a server that lacks it;
  - the update check depends on GitHub and its anonymous rate limit, which once a day per machine stays far from.
