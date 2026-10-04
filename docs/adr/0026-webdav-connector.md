# ADR-0026: WebDAV connector

- Status: accepted
- Date: 2026-10-04

## Context

WebDAV (RFC 4918) extends HTTP with methods to list (`PROPFIND`), create folders (`MKCOL`), move and copy (`MOVE`, `COPY`)
and lock resources. Many self-hosted storages speak it: Nextcloud and ownCloud, NAS systems such as Synology and QNAP, Apache
`mod_dav`, nginx and IIS, and services such as Box and pCloud. It is the most requested connector after the cloud stores.

Client libraries for Go exist, but they are small and wrap the same few requests. A client of our own keeps control of what
matters here:

- requests go through the network guard and the proxies;
- ranges on reads;
- conditional creates;
- the mapping of HTTP statuses to the errors of the port.

The Drive (ADR-0024) and Azure (ADR-0025) adapters took the same path.

## Decision

**A small client of our own over `net/http`** (`davfs`), with the subset of RFC 4918 a file manager needs.

- **Listings** are `PROPFIND` with `Depth: 1`, asking only for `resourcetype`, `getcontentlength` and `getlastmodified`. `Depth:
  infinity` is never sent, since servers often refuse it and it can be costly.
- **Addresses.** Hrefs in a multistatus may be absolute URLs or paths, and are percent-encoded. They are decoded and made
  relative to the root of the site. Entries outside the root are ignored.
- **Reads** are `GET` with `Range: bytes=n-`. A server that ignores the range and answers the whole file is still read
  correctly, by skipping the first bytes.
- **Writes** stream one `PUT` with the body sent as it comes. Creating a file that must not exist sends `If-None-Match: *`.
  There is no standard way to resume an upload, so writes do not resume: the transfer queue writes straight to the target (as for
  S3, ADR-0023). A failed upload is aborted.
- **Folders** are real: `MKCOL` creates one, and `DELETE` of a collection removes it with all its contents. Removing a folder
  that is not empty is refused before anything is sent.
- **Renames** are `MOVE` with `Overwrite: F`, so an existing target is never replaced.
- **Modification times** are read from `getlastmodified` but cannot be set: RFC 4918 makes that property protected.
- **Authentication** is HTTP Basic, sent with the first request, or none for public shares. The password can be stored in the
  vault or asked for when connecting. Digest authentication is left for later.

### Endpoint and TLS

A site has a host, a port, HTTPS on or off, and the path of its root, such as `/remote.php/dav/files/alice/` on Nextcloud.

HTTPS is first verified against the system authorities, or those Transferly was given. If that fails, as with self-signed
certificates on NAS systems and home servers, the fingerprint of the certificate is offered to the user, exactly like an FTPS
certificate (ADR-0012). The fingerprint is recorded under the key type `tls-certificate`, and a changed certificate is refused.

A certificate that the authorities accept is never pinned. Certificates from Let's Encrypt rotate every 90 days, and pinning
them would raise a false alarm at each renewal.

Plain HTTP is allowed, since WebDAV also serves local networks. It is marked in the connection log, and the site icon shows the
red badge.

### Capabilities

Real folders and renames. No permissions, no times that can be set, and no resumable writes. The maturity starts at beta, and
the connector is off until an administrator offers it.

### Tests

- `davtest` wraps `golang.org/x/net/webdav`, a full server in memory, with Basic authentication and faults per request. It runs
  the shared suite and the catalog conformance suite.
- Apache httpd with `mod_dav`, an independent implementation, runs in the isolated test stack and in the development test
  environment. The adapter passes the shared suite against it, and the end-to-end tests use it.

## Consequences

- Positive:
  - Nextcloud, ownCloud, NAS systems and any WebDAV server can be browsed and transferred like other sites;
  - self-signed servers work after a confirmation, without disabling verification;
  - no dependency is added.
- Negative:
  - uploads do not resume; a large upload that fails starts again;
  - times cannot be set, so synchronization compares sizes and the times the server writes;
  - Digest authentication, OAuth (for Box and others), locks and server-side copies are left for later.
