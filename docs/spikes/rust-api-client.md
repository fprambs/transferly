# Spike: Rust Client from the API Contract

For the terminal client of [ADR-0029](../adr/0029-terminal-client-and-browser-login.md). Run on 2026-11-24 in Docker (`rust:1-bookworm`,
`openapitools/openapi-generator-cli:v7.17.0`); nothing was installed on the host.

## Question

Can the Rust client of `api/openapi.yaml` be generated, so the terminal client does not write 110 operations and their models by
hand? The contract says OpenAPI 3.1.

## Results

| Candidate | Result |
|---|---|
| `progenitor` 0.17 (`cargo progenitor`) | **Rejected.** Reads only 3.0; the contract uses no 3.1 feature (no `null` types, `const`, `examples`, `$defs`, `webhooks`), so changing the header to `3.0.3` is enough. It then fails on `importFilezilla` (`application/xml` body) and, with that removed, panics with `assertion failed: response_types.len() <= 1`: it supports one success body per operation, and `login` answers `200 SessionInfo` or `202` with a second-factor challenge. |
| `openapi-generator` 7.17, generator `rust`, `reqwest`, async, rustls | **Works with the contract as it is.** 94 models, every operation, compiles with no errors. The warnings are only renamed identifiers. |
| Hand-written client | Not needed for the models; still needed for the few operations below. |

## Findings on the generated client

- Models, path and query parameters, `X-CSRF-Token` and the `problem+json` error bodies (one error enum per operation) are right.
- `login` returns `SessionInfo` for every 2xx, so a `202` second-factor challenge fails to decode. Login and `/auth/mfa` need a
  hand-written call (the browser sign-in of T1 replaces the password login in the terminal anyway).
- `upload_file` streams a file from a `PathBuf` from its first byte: no resume from an offset and no progress. Uploads need a
  hand-written call that seeks to the offset and wraps the stream to count bytes.
- `download_file` returns the raw `reqwest::Response`, which streams and accepts `Range`: usable as it is.
- `/api/v1/ws` is not in the contract (an upgrade has no request and response shape); its client is hand-written with
  `tokio-tungstenite`, typed by the event table of `docs/guides/websocket.md`.

## Decision

1. Generate `tui/client` (`transferly-client`) with openapi-generator `rust`, `library=reqwest`, `supportAsync=true`,
   `useRustls=true`, from the contract as it is, through `make gen` in Docker. The generated crate is excluded from coverage, like
   `*.gen.go`.
2. The terminal client never calls the generated functions directly: a thin `api` module wraps them, adds the bearer token, maps
   error codes, and holds the hand-written calls: login and second factor, resumable uploads with progress, downloads to disk, and
   the events stream.
3. When T1 adds the `bearerAuth` scheme to the contract, the generated configuration carries the token (`bearer_access_token`).
