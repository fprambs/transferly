# Spike: Streaming Transfers and Events from Rust

Program: `tui/spikes/apistream`. For the terminal client of [ADR-0029](../adr/0029-terminal-client-and-browser-login.md). Run on
2026-11-24 in Docker against the isolated stack of `compose.test.yaml` (`transferly:test` built from `main`, `atmoz/sftp`), with
`reqwest` 0.12 (rustls), `tokio`, `tokio-util` and `tokio-tungstenite` 0.24; nothing was installed on the host.

## Question

Can a Rust client do what the terminal client needs, with memory that does not grow with the file?

1. Stream a large upload from disk with its own byte count.
2. Interrupt it, find the staged size and resume it with the same `transferId`.
3. Download from an offset with `Range`, to disk.
4. Follow `/api/v1/ws` with credentials in the upgrade request.

## Results

| Metric | Value |
|---|---|
| File | 512 MiB from disk, cut by the client at 256 MiB |
| Cut upload | server records it `failed` 11 s after the start; staged `transferly-<id>.part` of 255 MiB |
| Resumed upload | `mode=resume&offset=<staged size>`, same `transferId`; final size exact (536 870 912 bytes) |
| Upload throughput | 23.6 MB/s over both requests, including the wait for the cut one to stop |
| Ranged download | last 128 MiB with `Range: bytes=384MiB-`, `206`, exact length, 64.8 MB/s |
| Events | `snapshot` 1, `transfer.state` 6 (`queued`, `running`, `failed`, `queued`, `running`, `done`), `transfer.progress` 89 |
| Peak resident memory of the client | **9 MiB** |
| Server container memory | 24 MiB idle, 165 MiB peak (includes page cache) |
| Release binary | 6 MB, not stripped |

## Findings

- `reqwest::Body::wrap_stream` over `tokio_util::io::ReaderStream` streams a file after a `seek` with fixed memory; wrapping the
  stream counts the bytes for a local progress bar, and an `Err` from it aborts the request, which the server sees as an interrupted
  upload.
- **A resume must wait until the server stopped the previous request.** The server allows one upload per path at a time and keeps
  writing the staged file until it notices the cut. The first attempt asked for the staged size right after the client cut, before
  the server had closed the upload, and failed. The client waits for the transfer to be `failed`, `interrupted` or `canceled`,
  through `transfer.state` or `GET /transfers/{id}`, and only then reads the size of the `.part` file, as the frontend does
  (`features/transfers/resume.ts`).
- The staged file is `<folder>/transferly-<transferId>.part`; its size from `GET /files/entry` is the offset.
- `tokio-tungstenite` takes any header in the upgrade request (`IntoClientRequest`, then `headers_mut`). Here it carried the session
  cookie; after T1 it carries `Authorization: Bearer` the same way. A request without `Origin` passes the origin check, as
  `docs/guides/websocket.md` says.
- The downloaded range streams with `bytes_stream()` into a file; the client never holds more than one chunk.
- **The SFTP server of the test stack caps its upload folder at about 1022 MiB in total.** A 1 GiB upload fails with
  `502 remote_unavailable` once the folder holds 1 071 677 440 bytes, from Rust and from `curl` alike, and 1000 MiB passes. It is a
  limit of that test server, not of Transferly. Transfer tests of the terminal client stay under it and clean the folder.

## Decision

1. The terminal client streams uploads and downloads with `reqwest` and `tokio-util`, counting bytes in a stream wrapper; transfers
   never load a file into memory.
2. Resuming an upload follows the frontend: wait for the stopped state, read the size of the staged file, send the rest with the
   same `transferId`. Downloads resume with `Range` from the size of the local file.
3. Progress comes from `transfer.progress` for what the server reports and from the local counter for what the client sends; the
   events client is `tokio-tungstenite` with the token in the upgrade request, reconnecting and reloading after a snapshot, as the
   guide asks.
