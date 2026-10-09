#!/bin/sh
# Starts the complete isolated protocol stack and runs the browser suite, shared by Make and CI.
set -eu

cd "$(dirname "$0")/.."
docker compose -f compose.test.yaml --profile ftp --profile s3 --profile google --profile azure --profile webdav --profile ssh up -d --build --force-recreate --wait postgres transferly sftp ftp ftps ftps-implicit s3 fakegoogle azurite fakeazure webdav ssh strict-sftp
docker compose -f compose.test.yaml run --rm -T e2e node_modules/.bin/playwright test "$@"
