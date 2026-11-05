# Production Image and Stack

Sources: [Dockerfile best practices](https://docs.docker.com/build/building/best-practices/), [distroless](https://github.com/GoogleContainerTools/distroless), [OWASP HTTP Headers](https://cheatsheetseries.owasp.org/cheatsheets/HTTP_Headers_Cheat_Sheet.html), [go:embed](https://pkg.go.dev/embed).

## Image

`docker/app.Dockerfile` is multi-stage: Node builds the frontend, Go embeds `dist/` (`//go:embed all:dist`) and compiles a static binary, and the result runs on distroless `static-debian13` as user `65532`. The final image is about 6 MB.

## Rules

- Runtime base images are pinned by digest; build images by version tag.
- Non-root user, read-only root file system, `cap_drop: ALL`, `no-new-privileges`.
- The image has no shell or curl: health checks use `transferly healthcheck`.
- `dist/` is copied into the Go module before the build because `go:embed` cannot reach outside the module; a committed `.gitkeep` keeps the embed valid during development.
- The published port binds to `127.0.0.1` by default (`TRANSFERLY_BIND`); expose it through the `https` profile (Caddy) instead of opening it.
- Static assets under `/assets/` are immutable for a year; every other response is `no-store` or `no-cache`.
- Unknown extension-less GET paths fall back to `index.html`; unknown files, `/api/` paths and other methods return 404.
- Security headers follow the OWASP HTTP Headers cheat sheet; HSTS is set by Caddy because the application does not know whether TLS terminates in front of it.
- The instance key is generated on first start into `/data/instance.key` (mode 0600) and reused afterwards; `TRANSFERLY_INSTANCE_KEY` overrides it.

## Compose Notes

- Compose interpolates every service, even those in inactive profiles, so optional variables need defaults (`TRANSFERLY_DOMAIN` defaults to `localhost`).
- `TRANSFERLY_DB_PASSWORD` defaults to `transferly`; the database is only reachable on the internal network, and production deployments should set a stronger value.

## Verification

Build, start and check headers, probes, routing, container hardening and key persistence; see the phase 0 security report.
