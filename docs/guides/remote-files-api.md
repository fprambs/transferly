# Remote files API

Endpoints live under `/api/v1/workspaces/{workspaceId}/sites/{siteId}/files`. Every one needs a session; writes also need the anti-CSRF token. The first call connects through the pool, so the vault must be unlocked in the session and the server key must be trusted (`409 host_key_unknown` carries the fingerprint to confirm with `POST .../known-hosts`; `409 host_key_changed` also lists the trusted fingerprints in `trusted`, and trusting that key takes `mode` `add` or `replace`, see ADR-0014).

| Call | Permission | Notes |
|---|---|---|
| `GET /files?path=` | browse | list a directory |
| `GET /files/entry?path=` | browse | describe one path; its size is the offset to resume an upload |
| `GET /files/content?path=` | browse | stream a file; one `Range` header is honored (206), invalid or multiple ranges are ignored, unsatisfiable ones are 416 with `Content-Range: bytes */size` |
| `PUT /files/content?path=&mode=&offset=` | upload | stream the body; `mode` is `create` (default, refuses to replace), `overwrite` or `resume`; `offset` only with `resume` |
| `POST /files/directories` | modify | create a directory |
| `POST /files/rename` | modify | never replaces an existing destination |
| `DELETE /files?path=&recursive=` | modify | a non-empty directory needs `recursive=true` |

## Copies between sites

`POST /workspaces/{id}/copies` with `{sourceSiteId, sourcePath, targetSiteId, targetPath, policy?, transferId?}` copies a file, or a folder with everything below it, from a site to another (or to another folder of the same one) through the server. It answers `202` with a transfer of direction `copy` and keeps working after the request ends, so closing the browser does not stop it; the user cancels it like any transfer. The two authorizations (download on the source, upload on the target), the paths and copying a file onto itself are checked before the answer; what fails later (missing source, an existing file under the `fail` policy) ends the transfer as `failed` with its code. `policy` says what to do with files that exist at the destination: `fail` (default), `overwrite`, `skip`, `newer` (replace only when the source is newer) or `rename` (keep both as `name (1).ext`); folders that exist are merged into. Copying a folder into itself is refused, links and special files are left out, and the depth and entry limits of archives apply. A copy that fails leaves what it copied so far. The scheduler limits count the target site.

## Folders as ZIP

`GET /files/archive?path=&transferId=` streams a folder and its subfolders as a ZIP attachment named after the folder, written as the files are read, so memory does not grow with the tree. It is recorded as a download named `<folder>.zip` with an unknown size; the transfer is done once the client read the end of the stream. Links, devices and names with a separator, `..` or a control character are left out, so unpacking the ZIP cannot write outside its folder. A tree deeper than 64 levels or with more than 100,000 entries fails with `tree_too_large`, which after the stream started leaves the ZIP incomplete and the transfer failed. The explorer downloads folders this way by default and also offers to download the files one by one (up to 500), each as its own transfer.

## Checksums

`GET /files/checksum?path=&algorithm=` reads a file through the server and answers `{algorithm, checksum, size}` with the digest in lower case hexadecimal; the algorithms are `sha256` (default), `sha512`, `sha1` and `md5`, the last two only to compare with published sums. Nothing is stored, and the cost is reading the whole file. A copy with `verify: true` hashes the bytes it sends, reads the destination back and fails with `checksum_mismatch` when they differ; a folder verifies every file.

## Rules

- Downloads are always `Content-Disposition: attachment` with `Content-Type: application/octet-stream` and `nosniff`; what a remote server holds is never rendered as a page of this origin. File names are encoded as RFC 2231 parameters, so they cannot inject headers.
- Uploads are streamed end to end with no size limit; memory use does not grow with the file. An interrupted upload leaves the partial file: read its size with the entry call and send the rest with `mode=resume&offset=size`.
- Paths are validated (not empty, at most 4096 bytes, no control characters) before any connection is used.
- A connection is borrowed only while a call or a stream is open. A stream that hits a lost connection discards it instead of pooling it.
- A user keeps up to four connections per site, or the `connections` setting of an SFTP or FTP site (1 to 4). Only one connection of
  a site is opened at a time: calls that come meanwhile wait for it and share its failure, so a server sees one sign-in and the
  connection log one connection, and a changed host key is asked about once. A call that finds every connection busy waits 250 ms
  for one before opening another, so short calls share a connection. Servers behind a balancer whose servers each have their own
  key are reached through few connections; with `connections` at 1, browsing, previews and transfers share one.
- A read of the viewer that opens a new connection and meets a key that is not trusted shows the same question as the explorer, and
  reads again once the key is trusted.
- Errors have stable codes: `path_not_found`, `path_exists`, `directory_not_empty`, `not_a_directory`, `invalid_path`, `invalid_mode`, `remote_permission_denied`, `remote_quota_exceeded` (507), `remote_auth_failed` and `remote_unavailable` (502), plus the host key and vault codes. Internal details never reach the client.

## Tests

`core/files` runs on an in-memory file system behind the real pool, including connections that drop mid-stream. The HTTP tests cover the authorization matrix (anonymous, CSRF, stranger, viewer, editor, other workspace) and validate real responses against the OpenAPI document.
