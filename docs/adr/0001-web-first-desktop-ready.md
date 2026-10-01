# ADR-0001: Web first, desktop ready

- Status: accepted
- Date: 2026-09-24

## Context

Transferly needs multi-user workspaces, shared and personal environments, concurrent transfers and Docker deployment. Web and desktop (Electron, Tauri, Wails) were considered.

- Web adds a hop (browser, Transferly, remote server). Near the user or the servers the cost is negligible, and server-to-server transfers are faster.
- Multi-user, shared workspaces, queues that outlive the client and Docker deployment fit the web.
- Desktop offers full local disk access and active-mode FTP without NAT.

## Decision

1. Ship a web application in Docker first.
2. Keep a Wails desktop build feasible: reuse the Go core and the React UI.
   - Hexagonal backend: `core` free of HTTP, Postgres and Wails; separate transport and store adapters.
   - Frontend `Transport` interface and `usePlatform()`.
3. Reject Electron (duplicates the core in Node, heavy installers) and Tauri (Rust backend forces a core rewrite).

## Consequences

- Positive: desktop becomes packaging plus adapters, not a rewrite; the core is testable without HTTP or a database.
- Negative: dependency rules must be enforced in CI (depguard, ESLint `no-restricted-imports`); SQLite support needs portable SQL and a shared `storetest` suite; one extra indirection in the frontend.
