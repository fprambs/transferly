# Transferly patches to jlaffaye/ftp

This is jlaffaye/ftp v0.2.4 (ISC license, see LICENSE), used through a `replace` directive in `backend/go.mod`. Only the library sources are kept; its tests stay upstream.

## ACCT (4.8e)

`ServerConn.LoginWithAccount(user, password, account)` answers servers that reply 332 ("need account for login", RFC 959 section 4.1.1) to USER or PASS with `ACCT <account>`. `Login` calls it with an empty account, so its behavior is kept, except that every refusal of the login is now a `*textproto.Error` carrying the server's code (upstream returned a plain error for an unexpected reply to USER).

`ServerConn.LoginInteractive(user, password, account)` is the same login, but asks for the password through a callback, only when the server replies 331 to USER, with the message of that reply (4.8f, interactive logon). `LoginWithAccount` calls it with the stored password.

The upstream client has no way to send a command of its own, and wrapping the control connection does not work with explicit FTPS, where the client wraps it in TLS itself. Drop this copy when upstream supports ACCT.
