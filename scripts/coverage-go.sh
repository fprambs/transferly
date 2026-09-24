#!/bin/sh
# Runs the Go tests with the race detector and fails unless production code is fully covered.
# Excluded from the measurement: executables (cmd), test helpers, repository guards and generated code.
set -eu

cd "$(dirname "$0")/../backend"

packages=$(go list ./... | grep -v -e '/cmd/' -e '/internal/testutil' -e '/internal/docscheck' -e '/store/postgres/db' | paste -sd, -)
status=0
go test -race -count=1 -coverpkg="$packages" -coverprofile=coverage.out ./... >test.log 2>&1 || status=$?
sed -E 's/[[:space:]]+coverage: .*//' test.log
rm -f test.log
[ "$status" -eq 0 ] || exit "$status"

grep -v '\.gen\.go:' coverage.out > coverage.filtered.out
mv coverage.filtered.out coverage.out

go tool cover -func=coverage.out | tail -n 1
missing=$(go tool cover -func=coverage.out | awk '$3 != "100.0%" && $1 != "total:"')
if [ -n "$missing" ]; then
  echo "Functions below 100% coverage:"
  echo "$missing"
  exit 1
fi
