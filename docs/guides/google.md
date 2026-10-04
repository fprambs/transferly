# Google (Drive, Cloud Storage and the sign-in with Google)

Sources: [Drive API v3 reference](https://developers.google.com/workspace/drive/api/reference/rest/v3), [resumable uploads](https://developers.google.com/workspace/drive/api/guides/manage-uploads#resumable), [export formats](https://developers.google.com/workspace/drive/api/guides/ref-export-formats), [OAuth 2.0 for web server applications](https://developers.google.com/identity/protocols/oauth2/web-server), [golang.org/x/oauth2](https://pkg.go.dev/golang.org/x/oauth2), [Cloud Storage interoperability](https://cloud.google.com/storage/docs/interoperability). Decision: [ADR-0024](../adr/0024-google-connectors.md).

## Versions

- `golang.org/x/oauth2` v0.37.0: the authorization code flow with PKCE (`GenerateVerifier`, `S256ChallengeOption`, `VerifierOption`) and the clients that refresh access tokens.
- No Google client library: `gdrivefs` speaks the Drive v3 REST API with `net/http`.

## Registering the OAuth client (administrators)

1. In the [Google Cloud console](https://console.cloud.google.com/), create or choose a project and enable the **Google Drive API**.
2. Configure the **OAuth consent screen**:
   - **Internal**, when every account belongs to your Google Workspace. Nothing else is needed.
   - **External** otherwise. While it is in testing mode, only the test users you add can sign in, and their refresh tokens expire after seven days. To publish it, Google reviews it, because the scope `https://www.googleapis.com/auth/drive` is restricted.
3. Add the scopes `.../auth/drive`, `openid` and `email`.
4. Create an **OAuth client ID** of the type **Web application**. Its authorized redirect URI is `TRANSFERLY_BASE_URL` followed by `/api/v1/oauth/google/callback`, such as `https://files.example.com/api/v1/oauth/google/callback`. Add one per origin of `TRANSFERLY_ALLOWED_ORIGINS` people use.
5. Set `TRANSFERLY_GOOGLE_CLIENT_ID` and `TRANSFERLY_GOOGLE_CLIENT_SECRET` and restart Transferly.
6. Offer **Google Drive** in Settings → Administration → Connectors.

Cloud Storage needs none of this. Each site uses an HMAC key, which is created in Cloud Storage → Settings → Interoperability.

## The sign-in

1. `POST /api/v1/oauth/google/authorizations` keeps the state and the PKCE verifier for ten minutes and answers the consent page and the state.
2. The form opens a popup first, before calling the API, so the browser does not block it. It then sends the popup to the consent page.
3. `GET /api/v1/oauth/google/callback` exchanges the code. Its page publishes `{source, provider, state, refreshToken, email}`, or `{…, error}`, on the `BroadcastChannel` `transferly-oauth`, and closes. The page's CSP only runs its own script, through a nonce.
4. The form keeps the outcome of its own state only. The token goes into the `oauth` secret field and the email into the account field. Saving the site seals the token in the vault.

Transferly answers every page with `Cross-Origin-Opener-Policy: same-origin`. Once the popup visits Google, it loses its link to the page that opened it, so `window.opener` and `postMessage` cannot carry the outcome; the broadcast channel, shared by the pages of one origin, does.

## Drive patterns

- Ask only for `fields` the adapter reads, and send `supportsAllDrives=true` everywhere. Listings also send `includeItemsFromAllDrives=true`, and `corpora=drive&driveId=` inside a shared drive.
- Escape quotes and backslashes in query values (`name = 'it\'s'`).
- Resumable uploads: a `POST`, or a `PATCH` for a new version, to `/upload/drive/v3/files?uploadType=resumable` answers a session URL in `Location`. Chunks are `PUT` with `Content-Range: bytes a-b/*`, in multiples of 256 KiB, and Drive answers 308 until the last chunk gives the total. An empty file is `bytes */0`.
- Google documents have no size. They are read with `/export?mimeType=`, which Google limits to about 10 MB.
- Removing sets `trashed: true`; files stay in the trash for 30 days.
- Errors are `{"error": {"code", "errors": [{"reason"}]}}`:
  - `storageQuotaExceeded` is a quota error;
  - `userRateLimitExceeded` and `rateLimitExceeded` are answered with 403 but are retryable;
  - a 401, and an `invalid_grant` from the token endpoint, mean the account must sign in again.

## Tests

- `gdrivefs/googletest` is a fake of the OAuth endpoints and of the Drive subset, in memory, with faults per request and a cap on page sizes.
- `cmd/fakegoogle` serves it as the `fakegoogle` service of the test stack (profile `google`), which the end-to-end tests sign in with. It is not named `google`, because browsers force HTTPS on that name: `.google` is a top-level domain in their HSTS preload list.
