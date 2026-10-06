# Changelog

Every release of Transferly, newest first. Versions follow [Semantic Versioning](https://semver.org/). While the major version is 0,
a minor version may change behavior or the API. Each release is an annotated tag `vX.Y.Z` on `main`, made with `scripts/release.sh`
(see [docs/guides/releases.md](docs/guides/releases.md)).

## [0.9.0] - 2026-10-06

A round of fixes from daily use against enterprise SFTP gateways, FileZilla imports and large files, with faster and steadier
tests.

### Sites and connections

- Strict servers, such as gateways that report sizes they do not deliver: large files are checked at their end before their size
  is announced, the servers found strict are remembered across restarts, and a site can say its sizes are inexact. Reads from them
  go by large requests on a second channel, so downloads are no longer slow.
- A pool opens one connection per site at a time, with a short grace before a second one, and SFTP and FTP sites can cap their
  connections (for servers that change their key per connection, set it to 1).
- A connection keeps opening when the request that started it goes away and another comes at once, as when the explorer moves to
  another folder while connecting: the question the server asked while signing in is no longer withdrawn and reported as refused
  credentials.
- A wrong SFTP password is sent once per connection, so it counts as a single failed attempt towards the lockout of the server.
- A server that closes the sign-in counts as refused credentials, not as a lost connection to retry.
- FTPS keeps checking the certificate of data connections after the request that opened the session ends.
- FTP: a canceled download fails at once instead of returning bytes already received.
- The SFTP client no longer crashes on servers that send false lengths.

### Sites

- Duplicate a site, with its stored secrets copied on the server, from its menu.
- Right click on a site or collection opens the same actions as its ⋯ menu, now with icons and Delete set apart in red.
- Site names are unique per collection instead of per workspace, so the same name can live in different folders, as in FileZilla.
- Test connection tries what the form holds, before saving and after editing, not the saved site.
- FileZilla import: passwords stored by FileZilla with padding are read right.

### Vault

- The vault stays unlocked while the session is in use (ADR-0028), also for previews that need it. Keeping it across restarts is
  only for development, with `TRANSFERLY_VAULT_REMEMBER_SESSIONS`.

### Interface

- The explorer lists folders first and the newest files first, and sorts by the Name, Size, Type or Modified column clicked; the
  choice is remembered.
- Previews of tables and spreadsheets show the first 1,000 rows and say how to get the rest; spreadsheets over 10 MB are offered as
  a download, and a "Preparing the view…" notice shows while a viewer builds. Archives open whatever their size.
- Quick connect picks the protocol from a list with its icon instead of a row of tabs.
- The explorer toolbar folds into two rows in narrow panes.
- A server key met while previewing a file is asked about, as when listing.
- The count of an open connection log is readable in every theme.

### Operations

- The isolated test stack runs an SFTP server whose sizes do not hold (`strict-sftp`).
- Go integration tests copy a migrated template database and run in parallel; browser tests no longer depend on what earlier runs
  left behind, and `make e2e SPECS="..."` runs some specs on a fresh stack.

## [0.8.0] - 2026-10-05

SFTP sites gain SSH tools: the server does the work itself, sudo opens what only root may read, and a terminal opens in the folder
in view (ADR-0027).

### Sites and connections

- Operations on the server: on SFTP servers that run commands, copies within a site, checksums, compressing into ZIP or tar.gz,
  extracting and measuring folders run on the server, without moving the data through Transferly. Servers that refuse commands,
  such as accounts forced to `internal-sftp`, keep working as before. Paths are always quoted, and tested with hostile names
  against OpenSSH.
- Sudo mode: an SFTP site can run its session as root, or another user, through `sudo` and the SFTP server of OpenSSH, answering
  sudo with its own password or the password of the account. Only admins and the owner turn it on, and a refusal of sudo fails
  the connection before any SFTP is sent.
- Integrated terminal: an SFTP site can allow a terminal, which opens a shell with a PTY in the folder in view, over the same SSH
  connection. Only admins turn it on and open it, and the activity records each terminal with its folder and duration, never what
  was typed.
- SFTP and FTP sites without a default remote folder open where the server starts the session, instead of assuming `/`.

### Interface

- The explorer offers Compress… and Extract here in the context menu and the details panel, and Measure for folders, when the
  server can.
- Terminal in the explorer toolbar opens a panel below the files, with one tab per shell.

### Fixed

- Tabs of workspaces that no longer exist, kept by the browser, are closed for good instead of coming back as "Site not found".
- WebDAV: a canceled upload is removed again while the server still shows it, and the reason of a refused upload is kept.

### Administration

- An administrator can turn commands on servers off for the whole instance in Administration → Limits; sites then behave as
  plain SFTP, with no sudo mode or terminal.

### Operations

- The isolated test stack and `make testenv` run an OpenSSH server with a shell and sudo (`ssh`), next to the SFTP-only one.

## [0.7.0] - 2026-10-04

WebDAV joins the connectors, and the development environment gets a test server for every connector.

### Sites and connections

- WebDAV, in beta (ADR-0026): Nextcloud, ownCloud, NAS systems, Apache and any WebDAV server, with the path of the root of each
  site. Folders are real, renames never replace what is there, and reads go by ranges. Sign-in uses a password, kept in the
  vault or asked for when connecting, or no sign-in for public shares.
- HTTPS is trusted when a public authority signed the certificate. A self-signed certificate is offered by its fingerprint to
  trust, as for FTPS, and plain HTTP is marked in the connection log.
- Uploads to WebDAV do not resume, and times cannot be set: the protocol has no standard way to do either.

### Interface

- WebDAV has a tile of its own in the catalog, the store and the sites sidebar.

### Operations

- `make testenv` runs a test server for every connector, each holding the preview samples: SFTP, FTP and FTPS, S3, WebDAV
  (Apache with `mod_dav`, with a private and a public share), Azurite and a fake of Azure Files, and a fake of Google that the
  development backend signs in with. `testenv/README.md` lists the settings of a site for each.
- The isolated test stack runs Apache with `mod_dav`, and the end-to-end tests create WebDAV sites and work with their files.
- The roadmap plans SSH tools on the SFTP connector: operations on the server, a sudo mode and a terminal.

## [0.6.0] - 2026-10-04

Azure Storage joins the cloud connectors, and every connector now wears the official icon of its service.

### Sites and connections

- Azure Blob Storage, in beta (ADR-0025): containers reached with the account key, a SAS token or public access, also in sovereign
  clouds and emulators. Folders are emulated with prefixes, uploads are committed whole in blocks of 8 MiB and renames copy on the
  server.
- Azure Files, in beta (ADR-0025): shares with real folders, native renames, modification times that can be set and resumable
  uploads in ranges of 4 MiB.
- Both connectors speak the REST APIs of Azure Storage with a client of their own, signed with Shared Key, without the Azure SDK.

### Interface

- The catalog of a new site, its form and the connector store show the official icons of Amazon S3, Google Cloud Storage, Google
  Drive, Azure Blob Storage and Azure Files, and tiles of their own for SFTP and FTP.
- The sites sidebar shows the icon of each site's connector, with an amber badge when FTP only uses TLS if the server offers it and a
  red one when the connection is not encrypted.

### Operations

- The isolated test stack runs Azurite and a fake of Azure Files, and the end-to-end tests create Azure sites and work with their
  files.
- The README, the architecture and the guides describe the connector catalog and the S3, Google and Azure connectors.

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
