#!/bin/sh
# Regenerates code from the API contract and the SQL queries, and the themes of the terminal client from those of the web.
set -eu

cd "$(dirname "$0")/.."

(cd backend && sqlc generate && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml)
(cd frontend && mkdir -p src/lib/api && pnpm exec openapi-typescript ../api/openapi.yaml -o src/lib/api/schema.d.ts)
node scripts/export-themes.mjs
