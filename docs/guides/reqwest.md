# reqwest and tokio-tungstenite

Sources: [reqwest docs](https://docs.rs/reqwest/latest/reqwest/) (crate root, `ClientBuilder`, `Body`, `Response`) and the streaming
spike ([report](../spikes/rust-streaming.md)). Versions: reqwest 0.13.5, tokio-tungstenite 0.30.0 (checked 2026-11-24). Used by
`transferly-tui` and its generated client ([report](../spikes/rust-api-client.md)).

## Rules

- **Features:** `default-features = false` with `rustls`, `http2`, `json` and `stream`. 0.13 renamed the TLS feature from
  `rustls-tls` to `rustls`. It uses `aws-lc-rs` and `rustls-platform-verifier`, which trusts the certificates of the system: a
  self-hosted server with its own CA works once that CA is installed on the machine. Checked on 2026-11-24: builds and runs as a
  static `x86_64-unknown-linux-musl` binary. The client never turns certificate checks off.
- **One `Client`** for the whole process, cloned where needed; it pools connections. Built once with the user agent
  `transferly-tui/<version>`.
- **Timeouts:** `connect_timeout` (10 s) and `read_timeout` (60 s), which resets after every read and catches stalled transfers.
  Never `timeout`: it is a deadline for the whole request including the body, and would cut long uploads and downloads. Ordinary API
  calls get a per-request `timeout` instead.
- **Redirects:** `redirect::Policy::none()`. The API never redirects, and following one could send the bearer token to another host.
- **Proxies:** system proxies (`HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`) apply by default, which is what people behind a proxy need.
  Calling `.proxy()` turns them off, so the client does not.
- **Uploads** stream with `Body::wrap_stream` over `ReaderStream`, after a `seek` to the offset when resuming; a stream wrapper counts
  bytes for the progress bar. A streaming body cannot be replayed, so uploads are never retried automatically; the resume logic of
  the spike is the retry.
- **Downloads** stream with `Response::bytes_stream()` into the file. `content_length()` is `None` when the body was decompressed, so
  progress uses the size from the file listing or `Content-Range`.
- **Errors:** read the `problem+json` body before checking the status, so the stable `code` reaches the interface; do not call
  `error_for_status`, which drops the body.
- The bearer token goes in `Authorization` on every request and in the WebSocket upgrade; it is never logged, and the debug log
  redacts the header.
- **WebSocket:** `tokio-tungstenite` with `connect_async(request)`, the token added through `IntoClientRequest` and `headers_mut`.
  Its TLS feature must use the same rustls as reqwest; `cargo tree -d` shows duplicated TLS stacks and `cargo deny` bans them
  ([Rust tooling](rust-tooling.md)). After a drop, reconnect with backoff and reload the transfers after the first `snapshot`
  ([websocket guide](websocket.md)).

## Tests

- HTTP tests run against a local server in the test (`wiremock` or a small `axum` app) that checks headers and paths and answers
  problem bodies, slow bodies and dropped connections.
- Transfers and resume are integration tests against the isolated stack of `compose.test.yaml`, staying below the 1022 MiB cap of its
  SFTP folder.
