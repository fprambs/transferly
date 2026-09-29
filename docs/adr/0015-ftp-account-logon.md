# ADR-0015: FTP account logon and a patched FTP client

- Status: accepted
- Date: 2026-09-29

## Context

FileZilla offers an "Account" logon type for FTP: some older servers reply 332 ("need account for login", RFC 959 section 4.1.1) to USER or PASS, and expect the account with ACCT before they let the user in. Sentinel's FTP client, jlaffaye/ftp v0.2.4, expects 230 after PASS and fails on 332. It has no way to send a command of its own. Wrapping the control connection to answer 332 underneath the client does not work with explicit FTPS, where the client itself wraps the connection in TLS after AUTH TLS, so the wrapper would only see ciphertext.

## Decision

**A patched copy of the client lives in `backend/third_party/ftp`,** used through a `replace` directive. It keeps upstream's sources and license and adds `LoginWithAccount`, which answers a 332 after USER or PASS with ACCT; `Login` calls it with no account. The patch and the reason for it are written in `third_party/ftp/PATCHES.md`. The copy is dropped when upstream supports ACCT. Nested modules are left out of Sentinel's documentation guard, and the image copies the patched module before it downloads the dependencies.

**The account is a secret of the site,** sealed in the vault like the password (`account` kind), only for FTP sites of the `account` logon type; any other logon type drops it, and SFTP refuses the type. A server that asks for an account the site has not got is an authentication failure, as a wrong password is. The FileZilla import reads logon type 4 with its password and account.

## Consequences

- Positive: the account logon works over plain FTP, explicit FTPS and implicit FTPS alike, and a USER refused by the server now reports its code (an authentication failure instead of a lost connection).
- Negative: Sentinel maintains a small fork of a dependency and must port the patch when it upgrades the client. No FTP server of the test stack asks for an account, so the ACCT exchange is covered by a scripted server in the adapter's tests, and the end-to-end test only covers saving and connecting such a site.
