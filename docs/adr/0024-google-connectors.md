# ADR-0024: Google connectors (Cloud Storage and Drive)

- Status: accepted
- Date: 2026-10-04

## Context

The next connectors are Google's: Cloud Storage buckets and Google Drive. They differ a lot.

- **Cloud Storage** stores objects in buckets, as S3 does, and its XML API is compatible with S3. Google gives HMAC keys for it.
- **Drive** belongs to a person or a team. It is reached on their behalf, after they sign in with Google and consent, so it needs OAuth 2.0. Files are identified by ID, not by path. A folder may hold two files of the same name. Google Docs, Sheets and Slides are not files and must be exported to be downloaded.

The official Go client of Drive (`google.golang.org/api`) pulls in gRPC, OpenTelemetry and the Cloud authentication library. A program that only creates the Drive service weighs 22 MB. Transferly needs about a dozen documented REST calls.

## Decision

### Cloud Storage

**A connector of its own on the S3 adapter.** The `gcs` connector reaches `storage.googleapis.com` with an HMAC access ID and secret, through the `s3fs` adapter of ADR-0023. minio-go already knows Google's endpoint: it streams uploads without chunked signatures and deletes objects one by one, as Cloud Storage requires.

**It behaves as S3:**

- folders are emulated with prefixes and markers;
- renames copy on the server;
- there are no permissions, times that can be set or resumable writes;
- public buckets can be read anonymously.

Copies of objects larger than 5 GiB are not possible, because the XML API has no copy of parts. Service account keys and the JSON API are left for later. The endpoint is a setting on the Advanced tab, so tests and private gateways can change it.

### Google sign-in

**Each installation registers its own OAuth client.** Google ties a client to its redirect URL, and every installation has its own URL. `TRANSFERLY_GOOGLE_CLIENT_ID` and `TRANSFERLY_GOOGLE_CLIENT_SECRET` configure it. The redirect URL is `TRANSFERLY_BASE_URL` followed by `/api/v1/oauth/google/callback`. Without them, connecting with Google is refused with guidance for the administrator. `TRANSFERLY_GOOGLE_AUTH_URL`, `TRANSFERLY_GOOGLE_TOKEN_URL` and `TRANSFERLY_GOOGLE_API_URL` override Google's endpoints, for the fake server of the test stack.

**The authorization code flow with PKCE, in a popup.**

1. The site form opens `GET /api/v1/oauth/google/start` in a popup, with the session of the person.
2. The server keeps the state and the PKCE verifier for ten minutes, bound to that user, and sends the popup to Google.
3. Google sends it back to the callback. The server checks the state, exchanges the code and reads the email of the account.
4. The server answers with a page that hands the refresh token and the email to the window that opened it, with `postMessage` restricted to the origin of the instance, and closes.

The form puts the token in the secret field of the site, as a typed password. Saving the site seals it in the vault, so the token passes through the browser once, as passwords do.

**Scopes.** `https://www.googleapis.com/auth/drive`, since the narrower `drive.file` only sees files the application created. Also `openid email`, to show which account is connected. The full Drive scope is restricted by Google. An installation that serves accounts outside its own Workspace needs Google's verification, or its app stays in testing mode, where refresh tokens expire after seven days. An internal Workspace app has neither limit.

**A new field kind, `oauth`.** It is a secret the form fills by signing in with a provider (`Provider: google`) instead of typing it. The schema of the connector says so, and the form draws a "Connect with Google" button.

### Drive

**A small REST client of our own.** It uses `net/http` and `golang.org/x/oauth2`, which refreshes the access token from the stored refresh token. Every request goes through the dialer of the network guard and the proxies. A refresh token that Google revoked is `ErrAuth`, and the person connects again.

**Paths are resolved to IDs**, one folder at a time, with a cache per connection that changes invalidate. The root is My Drive, or a shared drive named in the settings.

**Duplicate names.** When a folder holds several files of the same name, the oldest one is the one its path names. The others are not listed, as a path cannot tell them apart.

**Google Docs are exported** when they are read: Docs as `.docx`, Sheets as `.xlsx`, Slides as `.pptx` and Drawings as `.pdf`, with that extension added to their name. Their size is unknown until exported, so they are listed with size 0, and downloads stream them unsized. They cannot be written or renamed to a non-Google format.

**Writes.** Uploads use resumable upload sessions, sent in chunks of 8 MiB, so memory stays bounded. Overwriting a file uploads a new version of the same file. Creating a folder makes a Drive folder. Renames and moves are native. Modification times can be set. There are no permissions in the port's sense. Removing a file or a folder moves it to the trash, where it can be restored for 30 days.

**Capabilities:** real folders, renames and times that can be set. No permissions, no resumable writes and no server-side copy.

### Tests

An in-process fake of Google, `googletest`, serves the OAuth endpoints and the subset of Drive v3 the client uses, from memory. The Drive adapter passes the shared suite against it, and the connector passes the catalog conformance suite. The same fake runs as a service of the test stack for the end-to-end tests. Its authorization page consents at once, so no test touches a real Google account.

## Consequences

- Positive:
  - buckets of Cloud Storage and the files of Drive and shared drives are browsed and transferred like any site;
  - the sign-in flow and the `oauth` field kind are ready for OneDrive and other OAuth connectors;
  - no heavy dependency is added for Drive.
- Negative:
  - each installation must register an OAuth client with Google, and verify it to serve personal accounts for long;
  - duplicate names in Drive hide all but the oldest file;
  - Google Docs are exported, so editing them happens in Google, not in Transferly;
  - our Drive client must follow changes of the REST API by hand.
