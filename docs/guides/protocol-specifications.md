# Protocol specifications (SFTP, SSH, FTP, FTPS)

The references every change to the SFTP, FTP and FTPS adapters follows. The SFTP RFC was never finished, so its drafts are read through the index FileZilla keeps; FTP and FTPS have RFCs. Read the section that applies before implementing a protocol feature, and cite it in the ADR or the godoc when behavior depends on it. The library guides ([pkg/sftp](pkg-sftp.md), [FTP and FTPS](ftp.md)) say how the libraries implement these; this guide says what the protocols require.

Sources (read 2026-09-28):

- [FileZilla wiki: SFTP specifications](https://wiki.filezilla-project.org/SFTP_specifications), the index of the SSH RFCs and the SFTP drafts.
- [draft-ietf-secsh-filexfer-02](https://datatracker.ietf.org/doc/html/draft-ietf-secsh-filexfer-02), SFTP version 3.
- [RFC 4252](https://www.rfc-editor.org/rfc/rfc4252) SSH authentication, [RFC 4256](https://www.rfc-editor.org/rfc/rfc4256) keyboard-interactive, and the rest of the SSH RFCs the wiki lists (4250–4254, 4716 public key files, 8308 extension negotiation, 8332 RSA with SHA-2, 8709 Ed25519, 8731 Curve25519, 9142 key exchange recommendations, the ML-KEM hybrid draft).
- [RFC 959](https://www.rfc-editor.org/info/rfc959) FTP.
- [RFC 4217](https://www.rfc-editor.org/info/rfc4217) FTP over TLS.

## SFTP: version 3

- **Version 3 is the target.** Drafts go up to version 6 (filexfer-13), but OpenSSH has implemented version 3 (filexfer-00 to 02) since 2001 and FileZilla uses it too. Features of later versions are used only through extensions the server announces in `SSH_FXP_VERSION`. Unknown extensions are ignored.
- **Pipelining is allowed.** The draft says "There is no limit on the number of outstanding (non-acknowledged) requests that the client may send to the server". Requests on the same file are processed in order. Every request gets exactly one response, matched by its id. This is why reads and writes run concurrently (`UseConcurrentReads`, `UseConcurrentWrites`): with latency, sequential 32 KB requests cap the speed. The concurrency is only there when the stream keeps `WriteTo`/`ReadFrom`; any wrapper that hides them falls back to one request at a time (a lead for 4.13).
- **Sizes.** Clients may count on reads and writes of 32768 bytes and packets of 34000. Larger packets are negotiated (`MaxPacketChecked`). A read of a disk file returns the requested length or up to end of file, and the end is `SSH_FX_EOF`.
- **Times are 32-bit unsigned seconds, UTC.** Sub-second precision and dates after 2106 do not exist in version 3.
- **File names are opaque bytes** separated by `/`. Version 3 names no character set, which is why SFTP sites are UTF-8 only ([charset](charset.md)). `SSH_FXP_REALPATH` canonicalizes paths, including the home directory as `.`.
- **Rename fails when the target exists** ("It is an error if there already exists a file with the name specified by newpath"). The adapter never replaces a destination. Replacing needs the `posix-rename@openssh.com` extension or a delete first.
- **`SSH_FXP_SYMLINK` takes linkpath then targetpath** in the draft, but OpenSSH sends them the other way round. Check the library before using symlinks.
- **Status codes** are the version 3 set: OK, EOF, NO_SUCH_FILE, PERMISSION_DENIED, FAILURE, BAD_MESSAGE, NO_CONNECTION, CONNECTION_LOST, OP_UNSUPPORTED. `FAILURE` is generic and is refined by looking at the path ([pkg/sftp](pkg-sftp.md#server-statuses)).

## SSH authentication (RFC 4252, RFC 4256)

- **"none".** Clients use it to query the methods that can continue. "The server MUST always reject this request, unless the client is to be granted access without any authentication, in which case, the server MUST accept this request." `x/crypto/ssh` always tries it first, so an anonymous SFTP site (4.8d) needs no method of its own. It offers FileZilla's anonymous password next, for servers that let the anonymous user in by password.
- **Failure replies** list the methods that can continue and a partial-success flag. Clients may try them in any order, and servers should cap attempts (20 recommended) and time (10 minutes). Offering the same password again only uses up attempts: a real server then closes the connection, which must read as refused credentials, not a lost connection (`refusedCredentials`).
- **Password change** (`SSH_MSG_USERAUTH_PASSWD_CHANGEREQ`) is not supported. An expired password is a refusal.
- **Banners** (`SSH_MSG_USERAUTH_BANNER`) are UTF-8 text the server may send before authentication. They belong in the connection log (4.9).
- **Keyboard-interactive (RFC 4256), for 4.8f.** A request has a name, an instruction and any number of prompts, each with its own echo flag. Clients must handle several rounds, one at a time, and requests with no prompts (show the name and instruction anyway). A GUI shows every prompt of a round in one dialog, name as title, instruction as text, and never one window per prompt. Input is masked unless echo is set, and when in doubt it is masked. The number of rounds can leak information, such as an expired password.

## FTP (RFC 959)

- **Login** is `USER`, then `PASS` on 331, then `ACCT` on 332. 230 means logged in, 530 refused. `ACCT` may also be asked later, for a single command such as `STOR`: 332 keeps the command waiting for it, 532 drops it. An account site (4.8e) answers 332 at login with its stored account, and treats 332 or 532 on a later command the same way.
- **Anonymous.** RFC 959 does not define it. The convention (RFC 1635) is the user `anonymous` with an e-mail address as password. Transferly sends `anonymous@example.com`, as FileZilla does.
- **Reply codes.** The first digit is 1 preliminary, 2 done, 3 needs more, 4 temporary failure, 5 permanent failure. The second is 0 syntax, 1 information, 2 connections, 3 authentication, 5 file system. Classification follows these digits before looking at the text ([FTP guide](ftp.md#rules-of-the-adapter)).
- **Transfers** are `TYPE I` (binary) in stream mode. `REST` before `RETR`/`STOR` resumes, and `ABOR` ends with 426 or 226.

## FTPS (RFC 4217)

- **Explicit TLS.** `AUTH TLS` is answered with 234 to go ahead. 334 is wrong for TLS, and 5xx is a refusal. After the handshake come `PBSZ 0` and then `PROT P`: PBSZ must be sent even though TLS needs no buffer size. Data connections are clear until `PROT P`.
- **Order.** Clients should not insist that `AUTH` comes first or that `PBSZ`/`PROT` come right after it. Transferly sends `AUTH` before `USER`, so credentials never travel in clear text over a site that requires TLS.
- **Refused AUTH.** "A client that receives an AUTH rejection MAY choose to continue with the session unprotected", provided the user can require protection instead. That is exactly the `optional` versus `explicit` choice of ADR-0012. `optional` accepts the downgrade and says so.
- **Certificates** should identify the server as in HTTPS. Transferly trusts by fingerprint, like an SSH host key ([FTP guide](ftp.md#rules-of-the-adapter)), and data connections reuse the control connection's TLS session.
- **CCC** (clear the control channel after login) weakens the session and is not implemented.
- **Implicit TLS on 990 is not in RFC 4217.** It predates it and is a de facto practice, kept for servers that use it (ADR-0012).
