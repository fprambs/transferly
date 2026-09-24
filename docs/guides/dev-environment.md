# Dev Environment

Sources: [Compose services](https://docs.docker.com/reference/compose-file/services/), [air](https://github.com/air-verse/air), [Toxiproxy](https://github.com/Shopify/toxiproxy).

## Layout

- `compose.dev.yaml`: `tools`, `backend` (air), `frontend` (Vite), `postgres`; includes `compose.testenv.yaml`.
- `compose.testenv.yaml`: `sftp`, `ftp`, `ftps`, `ftps-implicit`, `toxiproxy`, behind the `testenv` profile.
- Published ports bind to `127.0.0.1` only: backend `8080`, frontend `5173`, Postgres `5432` (override with `SENTINEL_HTTP_PORT`, `SENTINEL_WEB_PORT`).

## Dev Container

Source: [devcontainer.json reference](https://containers.dev/implementors/json_reference/).

`.devcontainer/devcontainer.json` uses `dockerComposeFile` with the `tools` service, `runServices` limited to `tools` and `postgres`, `workspaceFolder` `/workspace` and `shutdownAction` `stopCompose`. Editor extensions and settings go under `customizations.vscode`.

## Rules

- Health checks plus `depends_on: condition: service_healthy` order startup.
- Bind host ports to `127.0.0.1`; test servers publish nothing.
- Use `init: true` for programs that misbehave as PID 1 (vsftpd).
- air rebuilds on `.go` and `.sql` changes (`send_interrupt`, `kill_delay`, `stop_on_error`); migrations are embedded, so SQL changes rebuild the binary.
- Vite proxies `/api`, `/healthz` and `/readyz` to `SENTINEL_API_URL`; `server.allowedHosts` lists `frontend` for container-to-container requests.
- Run a one-off command without waiting for other services: `docker compose run --rm --no-deps -T tools <command>`; `-T` avoids a TTY in scripts.
- Pin third-party images by version; pin by digest before closing a phase.
