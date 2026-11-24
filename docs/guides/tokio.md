# Tokio

Sources: [Tokio tutorial](https://tokio.rs/tokio/tutorial) (spawning, channels, select, streams, async in depth),
[graceful shutdown](https://tokio.rs/tokio/topics/shutdown), [tokio docs](https://docs.rs/tokio/latest/tokio/) and
[`CancellationToken`](https://docs.rs/tokio-util/latest/tokio_util/sync/struct.CancellationToken.html). Versions: tokio 1.54.1,
tokio-util 0.7.20 (checked 2026-11-24). Used by `transferly-tui`.

## Rules

- The binary enables `full`; library crates of the workspace enable only the features they use.
- **Never block a worker thread.** Code that runs long without an `.await` stalls every task on that thread. Blocking calls (the
  system keychain, opening the browser, walking large local folders, hashing) go through `tokio::task::spawn_blocking`.
- Do not hold `std::sync::Mutex` guards or `Rc` across an `.await`; the first stalls the runtime, the second makes the future not
  `Send` and `tokio::spawn` rejects it.
- **Bounded channels.** `mpsc::channel(n)` gives backpressure: `send().await` waits when the buffer is full. Unbounded channels and
  unbounded spawning can exhaust memory; the one exception is the channel of terminal events, which a person cannot flood. Use
  `oneshot` for request and reply.
- `select!`: all branches run on one task and the ones that lose are dropped, which cancels them. A future that must survive several
  iterations of a loop is created outside it and pinned (`tokio::pin!`, then `&mut fut`); a branch that may already be complete has
  a precondition (`, if !done`), since polling a completed future panics. Only cancel-safe futures go in loops, such as
  `mpsc::Receiver::recv`, `CancellationToken::cancelled` and `StreamExt::next`.
- Dropping a future cancels it but not the tasks it spawned; spawned tasks get a child `CancellationToken`.
- **Shutdown in three steps:** decide (quit key, `tokio::signal::ctrl_c()`), tell (cancel the root `CancellationToken`; every task
  selects on `cancelled()` and may finish what it must, such as closing the WebSocket), wait (`TaskTracker::close` then
  `wait().await`, with a short deadline). The terminal is restored after the wait ([ratatui](ratatui.md)).
- A `JoinHandle` returns `Err` when the task panicked or was cancelled; the app reports it instead of ignoring it.
- Local file I/O uses `tokio::fs` and `tokio_util::io::ReaderStream`, which run blocking reads on the blocking pool; that is what the
  streaming spike measured ([report](../spikes/rust-streaming.md)).

## Tests

- `#[tokio::test]` per async test; `start_paused = true` with `tokio::time::advance` for ticks, timeouts and retries, so tests do not
  sleep.
- Cancellation and shutdown have their own tests: cancel the token and assert that tasks end and resources close.
