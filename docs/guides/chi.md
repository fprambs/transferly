# chi

Source: [go-chi/chi](https://github.com/go-chi/chi) (v5.3.2).

## Rules

- Build routers with `chi.NewRouter()`; handlers are plain `http.Handler` values.
- Middleware order: request id, security headers, structured request logger (our own, `log/slog`), recoverer. chi's `middleware.Logger` uses the standard `log` package and is not used.
- Unknown routes and wrong methods get `r.NotFound` and `r.MethodNotAllowed` handlers that return RFC 9457 problem documents.
- Do not use `middleware.RealIP`: it is deprecated and spoofable through `X-Forwarded-For`. Sentinel uses `ClientIPFromRemoteAddr`, or `ClientIPFromXFF(trustedPrefixes...)` when `SENTINEL_TRUSTED_PROXIES` is set, and reads the result with `middleware.GetClientIP`.
- Do not apply `middleware.Timeout` globally: transfers are long-lived streams. Apply timeouts to groups of ordinary API routes.
- Group related routes with `r.Route` and per-group middleware with `r.Group`; mount sub-routers with `r.Mount`.
- Routes are generated from the OpenAPI contract; see the OpenAPI guide.

## Tests

Exercise the router through `httptest.NewServer` with contexts (`noctx`), and validate responses against the contract.
