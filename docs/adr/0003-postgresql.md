# ADR-0003: PostgreSQL as the primary store

- Status: accepted
- Date: 2026-09-24

## Context

Multi-user workspaces, a persistent transfer queue with several workers and cross-node events call for a real database from the start.

## Decision

- PostgreSQL 17 through `pgx` and `sqlc`; migrations with `goose`, embedded in the binary.
- The queue uses `SELECT ... FOR UPDATE SKIP LOCKED`; cross-node events can use `LISTEN/NOTIFY`.
- SQL stays portable and Postgres specifics sit behind `JobQueue` and `EventBus` so a SQLite desktop store remains possible (ADR-0001).

## Consequences

- Positive: safe multi-worker queue, transactional integrity, no later migration from SQLite.
- Negative: a database container is required even for a single user.
