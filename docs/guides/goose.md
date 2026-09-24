# goose

Sources: [annotations](https://pressly.github.io/goose/documentation/annotations/), [provider](https://pressly.github.io/goose/documentation/provider/) (v3.28.0).

## Files

- One SQL file per change, named with a zero-padded version: `00002_create_users.sql`.
- Exactly one `-- +goose Up`; `-- +goose Down` is optional. Annotations sit on their own line without leading spaces.
- Wrap functions and multi-statement bodies in `-- +goose StatementBegin` and `-- +goose StatementEnd`.
- Use `-- +goose NO TRANSACTION` for statements that cannot run in a transaction, such as `CREATE INDEX CONCURRENTLY`.
- Never edit an applied migration; fix forward with a new one.

## Provider

- Use `goose.NewProvider(goose.DialectPostgres, db, fsys, options)`; there is no global state.
- Migrations are embedded (`//go:embed *.sql` in `migrations/postgres`) so the binary carries its schema.
- Always pass `goose.WithSessionLocker` with the Postgres advisory locker so instances that start together migrate one at a time.
- `Up` is idempotent; `GetDBVersion` reports the current version.

## Tests

Migrate an empty isolated database, migrate it repeatedly, migrate concurrently, and check the resulting version.
