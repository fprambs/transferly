# Go Module Layout

Source: [Organizing a Go module](https://go.dev/doc/modules/layout).

- Servers keep logic in `internal/` and executables in `cmd/<name>/` (`package main`).
- Sentinel: `backend/` is the module; see ARCHITECTURE section 5.
- `internal/core` never imports adapters, transports, `app`, HTTP, database or Wails packages (depguard).

## Conventions

- `context.Context` first on I/O functions.
- Wrap errors with `%w`; inspect with `errors.Is` and `errors.As`.
- Tests next to code, always with `-race`; table tests where useful.
- Logging with `log/slog`; never log secrets.
- No `net/http` calls without a context (`noctx`); do not use `chi` `RealIP` (spoofable).
