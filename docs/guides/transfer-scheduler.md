# Transfer scheduler

`core/scheduler` decides which transfers run and which wait. It has no dependencies outside the standard library and keeps its state in memory: a restart interrupts every transfer anyway, and the persistent record lives in `core/transfer`.

## Rules

- `Acquire(ctx, Request)` blocks until the request fits under every limit and returns an idempotent `release`. Limits are global, per workspace, per user and per site; zero means no limit. The site of a quick connection is its own identifier, so quick connections are limited apart from saved sites.
- Waiting requests are ordered by priority (high, normal, low) and then by arrival. Every waiter that fits is admitted, so a full site never holds back transfers to another one.
- `Pause`, `Resume` and `SetPriority` act only on waiting requests and answer `ErrNotQueued` otherwise. A running transfer can only be canceled, because uploads and downloads are streams driven by the browser: stopping one half way is a cancellation followed by a resume.
- A canceled context removes the waiter, or gives its slot back when it was admitted in the same instant.
- `core/transfer` calls `Tracker.Admit` after `Begin` and before touching the remote server; the HTTP request waits there and the transfer stays `queued`. `Tracker.Finish` releases the slot.

## API

`POST /transfers/{id}/pause`, `POST /transfers/{id}/resume` and `PATCH /transfers/{id}` with `{"priority": -1|0|1}` answer the updated transfer, `409 transfer_not_queued` when it is not waiting and `422 invalid_priority`. `paused` and `priority` appear in the transfer and in its state events.

## Testing notes

An upload that is rejected while its body is still being sent only answers once the server has read (and dropped) the rest of the body, as Go's HTTP server does; tests that cancel a waiting upload close its body before reading the reply.
