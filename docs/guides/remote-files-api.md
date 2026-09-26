# Remote files API

Endpoints live under `/api/v1/workspaces/{workspaceId}/sites/{siteId}/files`. Every one needs a session; writes also need the anti-CSRF token. The first call connects through the pool, so the vault must be unlocked in the session and the server key must be trusted (`409 host_key_unknown` carries the fingerprint to confirm with `POST .../known-hosts`).

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

`POST /workspaces/{id}/copies` with `{sourceSiteId, sourcePath, targetSiteId, targetPath, overwrite?, transferId?}` copies one file from a site to another (or to another folder of the same one) through the server. It answers `202` with a transfer of direction `copy` and keeps working after the request ends, so closing the browser does not stop it; the user cancels it like any transfer. The two authorizations (download on the source, upload on the target), the paths and copying a file onto itself are checked before the answer; what fails later (missing source, existing target without `overwrite`) ends the transfer as `failed` with its code. The scheduler limits count the target site. Only files are copied.

## Rules

- Downloads are always `Content-Disposition: attachment` with `Content-Type: application/octet-stream` and `nosniff`; what a remote server holds is never rendered as a page of this origin. File names are encoded as RFC 2231 parameters, so they cannot inject headers.
- Uploads are streamed end to end with no size limit; memory use does not grow with the file. An interrupted upload leaves the partial file: read its size with the entry call and send the rest with `mode=resume&offset=size`.
- Paths are validated (not empty, at most 4096 bytes, no control characters) before any connection is used.
- A connection is borrowed only while a call or a stream is open. A stream that hits a lost connection discards it instead of pooling it.
- Errors have stable codes: `path_not_found`, `path_exists`, `directory_not_empty`, `not_a_directory`, `invalid_path`, `invalid_mode`, `remote_permission_denied`, `remote_quota_exceeded` (507), `remote_auth_failed` and `remote_unavailable` (502), plus the host key and vault codes. Internal details never reach the client.

## Tests

`core/files` runs on an in-memory file system behind the real pool, including connections that drop mid-stream. The HTTP tests cover the authorization matrix (anonymous, CSRF, stranger, viewer, editor, other workspace) and validate real responses against the OpenAPI document.
