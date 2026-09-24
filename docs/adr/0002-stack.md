# ADR-0002: Technology stack

- Status: accepted
- Date: 2026-09-24

## Context

The product needs fast, resilient file transfers, many concurrent sessions, a small deployment footprint and a modern accessible interface. Benchmarks found for the SFTP libraries of Go, Node.js and Rust are equivalent when requests are pipelined, so raw language speed does not decide.

## Decision

- Backend in Go: goroutine concurrency, static binaries and minimal images; `pkg/sftp`, `x/crypto/ssh`, `jlaffaye/ftp`, `chi`, `pgx`, `sqlc`, `goose`.
- Frontend in React with TypeScript, Vite, Tailwind CSS v4 and shadcn/ui on Base UI.
- OpenAPI 3.1 as the single API contract, with generated Go and TypeScript code.
- Everything runs in Docker; no tooling is installed on the host.

## Consequences

- Positive: one small binary embeds the frontend; typed contract on both sides; large React ecosystem.
- Negative: two languages; the FTP client library has weaker FTPS coverage and is validated by a spike.
