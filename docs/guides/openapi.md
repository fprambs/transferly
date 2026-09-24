# OpenAPI Contract and Code Generation

Sources: [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen), [openapi-typescript](https://openapi-ts.dev/introduction), [kin-openapi](https://github.com/getkin/kin-openapi), [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457).

## Flow

`api/openapi.yaml` (OpenAPI 3.1) is the single contract. `make gen` runs `scripts/generate.sh`:

- Go: `oapi-codegen -config backend/oapi-codegen.yaml` writes `internal/transport/httpapi/api.gen.go` (models, chi server, strict server, embedded spec).
- TypeScript: `openapi-typescript` writes `frontend/src/lib/api/schema.d.ts` (types only, no runtime).
- SQL: `sqlc generate` writes `internal/adapters/store/postgres/db`.

Generated files are committed; CI regenerates and fails on any diff.

## Rules

- Handlers implement `StrictServerInterface`; they return typed response objects, not raw `http.ResponseWriter` calls.
- Keep `skip-prune: true` so schemas without an operation, such as `Problem`, are still generated.
- Errors are RFC 9457 `application/problem+json` with a stable `code`; the server never localizes text.
- Frontend types alias the generated schema; do not hand-write API types.
- Generated code carries the standard `Code generated ... DO NOT EDIT.` header, which excludes it from lint, documentation guards and coverage.

## Tests

`contract_test.go` validates real responses against the embedded spec with `openapi3filter.ValidateResponse`, and error bodies against the `Problem` schema. Every new operation adds a case.
