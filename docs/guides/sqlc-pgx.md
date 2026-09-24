# sqlc and pgx v5

Source: [Using Go and pgx](https://docs.sqlc.dev/en/latest/guides/using-go-and-pgx.html).

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries/postgres"
    schema: "migrations/postgres"
    gen:
      go:
        package: "db"
        sql_package: "pgx/v5"
        out: "internal/adapters/store/postgres/db"
```

## Rules

- Annotate queries: `-- name: GetSite :one`, `:many`, `:exec`.
- Generated methods take `context.Context` first.
- Use `pgxpool` in production; `db.New(pool)`, and `db.New(tx)` inside a `WithTx` helper.
- Keep SQL portable; Postgres specifics stay in the adapter behind interfaces.
- The schema comes from the goose migrations.

## Tests

`storetest` suite against a real Postgres, one isolated database per test.
