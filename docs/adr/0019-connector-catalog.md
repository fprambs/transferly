# ADR-0019: Connector catalog

- Status: accepted
- Date: 2026-09-30

## Context

Sentinel reaches SFTP and FTP servers through adapters of one port, `remotefs.RemoteFS` (list, stat, read by offset, write, rename, remove, capabilities), and every feature (explorer, transfers, sync, copy between sites, file preview, archives) is written against that port only. Phase 5 adds S3, WebDAV, Google Drive, SMB and Azure Blob, and more will come as they are needed. Adding each one by hand to the site form, the store and the pool would not scale, and an installation should offer only the connectors its admins want.

## Decision

**A catalog of connectors compiled into Sentinel.** Each connector is Go code inside the binary, an adapter of `remotefs.RemoteFS`, that registers itself in a catalog with a descriptor. Nothing is loaded at run time: no third-party code runs next to the vault, the build stays reproducible and tested, and the same catalog works in Sentinel Desktop.

**The descriptor** tells everything the rest of Sentinel needs, so a new connector touches no other code:

- identity: type (`sftp`, `ftp`, `s3`, `webdav`, `gdrive`…), display name, icon, version and maturity (stable or beta);
- capabilities, extending `remotefs.Capabilities`: real folders or prefixes, permissions, times that can be set, resumable writes, server-side copy, rename;
- a configuration schema (fields, types, defaults, validation), from which the site form is built and the API validates input;
- the authentication it uses: password, access keys, private key, or OAuth through the browser, with the connector refreshing its tokens;
- a constructor that opens a connection from a site and its secrets.

**The store.** An admin screen lists the catalog and enables the connectors the installation offers, optionally per workspace. A disabled connector does not appear when creating sites; sites of a connector that becomes disabled stay stored but cannot connect. SFTP and FTP are enabled by default.

**Secrets** of every connector (access keys, OAuth refresh tokens, passwords) are kept in the vault like the passwords of today, sealed with the workspace key; configuration that is not secret goes in the site.

**Tests.** Every connector passes the shared conformance suite (`remotefstest`) and has a local server in the isolated test stack (MinIO for S3, a WebDAV server, an SMB server, emulators for the clouds when they exist), so no test touches a real account.

**Out-of-process plugins are not built now.** The descriptor keeps the door open: a later loader could register connectors that run as separate processes, with signed binaries and a versioned protocol, without changing the rest of Sentinel.

## Consequences

- Positive: one interface for every storage; a connector is a self-contained package with its descriptor, adapter and tests; the site form, the API validation and the store follow from the descriptor; admins decide what the installation exposes.
- Negative: adding a connector needs a new release of Sentinel; capabilities must be modelled carefully, since object stores lack real folders, permissions and partial writes, and features such as resume or chmod must adapt to what each connector declares.
