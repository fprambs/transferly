# WebDAV

Sources: [RFC 4918, HTTP Extensions for WebDAV](https://www.rfc-editor.org/rfc/rfc4918), [RFC 9110, HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110) (ranges, conditional requests), [RFC 7617, Basic authentication](https://www.rfc-editor.org/rfc/rfc7617), [Apache mod_dav](https://httpd.apache.org/docs/2.4/mod/mod_dav.html), [golang.org/x/net/webdav](https://pkg.go.dev/golang.org/x/net/webdav). Decision: [ADR-0026](../adr/0026-webdav-connector.md).

## Versions

- No WebDAV client library: `davfs` speaks RFC 4918 with `net/http`.
- `golang.org/x/net/webdav` (v0.58.0, through `golang.org/x/net`) is the reference server in memory of `davtest`.
- Apache httpd 2.4 with `mod_dav` and `mod_dav_fs` runs in the test environments (`testenv/webdav`).

## Creating a site

1. Offer **WebDAV** in Settings → Administration → Connectors.
2. Give the server, HTTPS on or off, and the path of the root of the site:

   | Server | Path |
   |---|---|
   | Nextcloud | `/remote.php/dav/files/{user}/` |
   | ownCloud | `/remote.php/webdav/` |
   | Synology | `/` on port 5006 (HTTPS) or 5005 |
   | Apache `mod_dav` | the `Alias` of the share |

3. Sign in with a username and password (HTTP Basic), have the password asked for when connecting, or open a public share anonymously. On Nextcloud, an app password is safer than the account password.
4. A self-signed certificate is shown by its fingerprint for you to trust, as for FTPS. A certificate a public authority signed is trusted without asking.

## Patterns

- **PROPFIND** with `Depth: 0` describes one resource and `Depth: 1` lists a folder. The body asks only for `resourcetype`, `getcontentlength` and `getlastmodified`. Only `propstat` entries with status 200 count.
- **Hrefs** are percent-encoded paths or absolute URLs. They are decoded, made relative to the root of the site, and entries outside it are ignored.
- **Folders end in a slash.** Some servers answer a redirect for a folder named without one. The client never follows redirects, and retries a PROPFIND with the slash once.
- **Missing paths.** Servers answer `404`, `409` or `410`, and some `405` for a path below a file.
- **PUT** streams the body (chunked). `If-None-Match: *` makes a create fail with `412` when the file exists. A failed create removes what the server kept, because some servers write in place.
- **MKCOL** answers `405` when the folder exists and `409` when its parent does not.
- **DELETE** of a collection is recursive, so a removal of a folder that is not empty is refused before it is sent.
- **MOVE** sends `Destination` as an absolute URL and `Overwrite: F`; `412` means the target exists.
- **Statuses:**
  - `401` is a failed sign-in;
  - `403` and `423` (locked) are refusals;
  - `507` is a full quota;
  - `5xx` and `429` are retryable.

## Tests

- `davtest` runs `x/net/webdav` on a file system in memory under `/dav/files/transferly/`, with Basic authentication, anonymous shares, servers that ignore ranges, and faults per request. The shared suite and the catalog conformance suite (over HTTPS, with the certificate pinned) run against it.
- With `TRANSFERLY_TEST_WEBDAV_ADDR` set to an Apache of the test environments, such as `webdav:80`, `davfs` runs the shared suite against it, each test in a folder of its own.
- `e2e/specs/webdav.spec.ts` creates a site on the `webdav` service of the test stack, works with its files, and opens the public share anonymously.
