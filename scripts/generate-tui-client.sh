#!/bin/sh
# Generates the Rust client of the API contract into tui/client with openapi-generator, in Docker, and points it at the HTTP stack of
# the terminal client: reqwest 0.13 without a TLS backend of its own, since the binary brings rustls (docs/spikes/rust-api-client.md,
# docs/guides/reqwest.md). Run by `make gen`.
set -eu

cd "$(dirname "$0")/.."
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD:/repo" openapitools/openapi-generator-cli:v7.17.0 generate \
  -i /repo/api/openapi.yaml -g rust -o /repo/tui/client \
  --additional-properties=packageName=transferly-client,library=reqwest,supportAsync=true,avoidBoxedModels=true >/dev/null
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD/tui/client:/client" alpine:3.22 sh -c '
  sed -i \
    -e "s|^version = \"0.1.0\"|version = \"0.0.0\"|" \
    -e "s|^edition = \"2021\"|edition = \"2024\"\npublish = false|" \
    -e "s|^reqwest = .*|reqwest = { version = \"0.13.5\", default-features = false, features = [\"json\", \"multipart\", \"stream\", \"query\", \"form\"] }|" \
    -e "/^\[features\]/,\$d" \
    /client/Cargo.toml'
