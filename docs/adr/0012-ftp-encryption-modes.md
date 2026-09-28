# ADR-0012: FTP encryption modes

- Status: accepted
- Date: 2026-09-28

## Context

Sentinel had two FTP protocols, `ftp` (clear text) and `ftps` (TLS), and chose between explicit TLS (`AUTH TLS`) and implicit TLS by the port: 990 meant implicit, anything else explicit. FileZilla instead has one FTP protocol with an **Encryption** setting of four values, and its default is "use explicit FTP over TLS if available". People coming from FileZilla expect that setting; its site manager files carry it as the protocol number (0, 3, 4 and 6), which the importer read as clear text for 0 and could not represent for implicit TLS off port 990.

## Decision

**One FTP protocol with an encryption.** A site's `Protocol` is `sftp` or `ftp`; FTP sites carry an `FTPEncryption`:

- `explicit` requires `AUTH TLS` and refuses a server that does not offer it. The default of new sites in the interface.
- `implicit` expects TLS from the first byte, on any port (990 by default).
- `plain` sends everything, credentials included, in clear text, with the warning the interface already shows.
- `optional` sends `AUTH TLS` and, when the server refuses it with a 5xx reply before the greeting ends, connects again in clear text.

**Compatibility.** `ftps` stays accepted as input and is read as FTP that requires TLS: implicit on port 990, explicit elsewhere, exactly what it meant before; `ftp` without an encryption stays clear text. A migration rewrites the stored sites the same way, so none changes behavior. The importer maps FileZilla's numbers to the four modes, and implicit TLS is no longer tied to port 990.

**The downgrade risk of `optional` is accepted and shown.** Anyone who can tamper with the connection can answer `AUTH TLS` with a refusal and have the password sent in clear text; this is the price FileZilla's default pays too. Sentinel offers it because some servers genuinely have no TLS, but not as the default: the interface warns next to it, and the live connection log (4.9) is to say when a connection went on without TLS. Once TLS is established the certificate is checked as for `explicit`.

## Consequences

- Positive: the setting matches FileZilla's; imported sites keep their encryption; implicit TLS works on any port; `ftps` clients keep working.
- Negative: `optional` can be downgraded silently until the connection log reports it; API responses now answer `ftp` with an `ftpEncryption` where they used to answer `ftps`.
