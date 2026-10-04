# Changelog

Every release of Transferly, newest first. Versions follow [Semantic Versioning](https://semver.org/). While the major version is 0,
a minor version may change behavior or the API. Each release is an annotated tag `vX.Y.Z` on `main`, made with `scripts/release.sh`
(see [docs/guides/releases.md](docs/guides/releases.md)).

## [0.5.0] - 2026-10-04

The first release. It sums up what Transferly does at the end of the work on the connector catalog and the cloud connectors of phase 5.

### Sites and connections

- Sites of SFTP, FTP and FTPS (explicit, implicit and optional TLS), with collections, color labels and a default remote folder.
- Logon types: password, key file, asking for the password, interactive (one-time codes) and anonymous.
- Proxies (SOCKS5 and HTTP) and SSH jump hosts.
- Trusted server keys and FTPS certificates, confirmed on first use; a changed key is refused.
- Quick connections that keep nothing, and the import of the FileZilla site manager.
- The connector catalog (ADR-0019, ADR-0022): every site is a connector and a configuration described by its schema. Administrators
  choose which connectors the instance offers. A new site starts from a catalog of tiles with the icon of each service.
- S3 and compatible services (AWS, MinIO, Cloudflare R2, Wasabi and others), in beta (ADR-0023).
- Google Cloud Storage with HMAC keys, and Google Drive, signed in with Google in the browser, in beta (ADR-0024).
- One conformance suite every connector passes, against local reference servers.

### Files and transfers

- An explorer with a split view, tabs, filters, search, bookmarks, a connection log and keyboard shortcuts.
- Uploads and downloads of files and folders, resumable where the protocol allows it, with a queue, priorities, speed limits and a
  history.
- Comparing and synchronizing folders between sites, and copying between sites.
- A file viewer that opens text, code, Markdown, structured data, images, audio, video, PDF, Office documents, archives, databases,
  fonts, certificates and email without downloading them, in an isolated page.
- An editor for remote text files, which warns before overwriting a newer version.
- Checksums of remote files.

### Security

- A vault per user, with a master password, Argon2id and a recovery kit; every secret of a site is sealed in it.
- Workspace keys shared within teams, rotated when a member leaves.
- Second factors: TOTP and passkeys (WebAuthn).
- A network guard that only lets sites reach public addresses and the private networks an administrator allows, resistant to DNS
  rebinding.
- Strict security headers, CSRF protection, rate limits on authentication, and a security review at the close of every phase.

### Teams and administration

- Team workspaces with roles, invitations and an audit trail.
- Administration of accounts, allowed networks, limits and quotas, and connectors, with its own audit log.
- Backups of the sites, encrypted with a password.

### Interface

- A dense, dark-by-default interface in English and Spanish, with several themes, right-to-left readiness, accessibility checks with
  axe in every theme, and the Transferly brand.
- Settings in a dialog; the version of the instance at the foot of its navigation.

### Operations

- One distroless image with the interface embedded in the binary; `transferly version` prints the version.
- Migrations on start, health checks, and Docker Compose files for production, development and tests.
