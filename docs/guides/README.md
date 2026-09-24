# Library Guides

Before using a library for the first time, read its official documentation and record the essentials here: pinned version, default setup, recommended patterns, anti-patterns, how to test. Code follows the guide; deviations need an ADR. Revisit a guide when its library is upgraded.

| Guide | Status |
|---|---|
| [Documentation](documentation.md) | done |
| [Internationalization](i18n.md) | done |
| [Dates and time zones](datetime.md) | done |
| [React](react.md) | done |
| [Zustand](zustand.md) | done |
| [TanStack Query](tanstack-query.md) | done |
| [shadcn/ui and Tailwind v4](shadcn-ui.md) | done; confirm Base UI or Radix at `init` |
| [Playwright](playwright.md) | done |
| [Frontend tooling](frontend-tooling.md) | done |
| [Dev environment (Compose, air, Toxiproxy)](dev-environment.md) | done |
| [Go module layout](go-layout.md) | done |
| [pkg/sftp](pkg-sftp.md) | done |
| [sqlc and pgx](sqlc-pgx.md) | done |
| TanStack Router, Table, Virtual; react-hook-form and zod; dnd-kit; react-arborist; Vitest; MSW | before use |
| chi, goose, oapi-codegen, jlaffaye/ftp, coder/websocket, `x/text` | before use |
| Docker: multi-stage, distroless, Compose, Dev Containers | before use |

## Pinned Versions (checked 2026-09-24)

| Component | Version |
|---|---|
| Go | 1.27 (`golang:1.27-bookworm`) |
| Node | 24 LTS |
| pnpm | 12.6.0 |
| sqlc | v1.31.1 |
| goose | v3.28.0 |
| oapi-codegen | v2.8.0 |
| govulncheck | v1.8.0 |
| air | v1.67.4 |
| golangci-lint | v2.14.0 |
| chi | v5.3.2 |
| pgx | v5.11.0 |
| pkg/sftp | v1.13.11 |
| jlaffaye/ftp | v0.2.4 |
| coder/websocket | v1.8.15 |
| golang.org/x/crypto | v0.57.0 |
