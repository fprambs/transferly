#!/bin/sh
# Runs the Go tests with the race detector and fails unless production code is fully covered.
# Excluded from the measurement: executables (cmd), test helpers, repository guards and generated code.
set -eu

cd "$(dirname "$0")/../backend"

rm -f coverage.out coverage.filtered.out test.log

packages=$(
  go list ./... | grep -v -e '/cmd/' -e '/internal/testutil' -e '/internal/docscheck' -e '/store/postgres/db' \
    -e '/remotefstest' -e '/connectortest' -e '/sftptest' -e '/ftptest' -e '/s3test' -e '/googletest' -e '/azuretest' -e '/davtest' -e '/storetest' -e '/passkeytest' -e '/pgtest' -e '/spikes' | paste -sd, -
)
test_pkgs=$(
  go list ./... | grep -v -e '/cmd/' -e '/spikes'
)

status=0
go test -race -count=1 -covermode=atomic -coverpkg="$packages" -coverprofile=coverage.out $test_pkgs >test.log 2>&1 || status=$?

if [ ! -f test.log ]; then
  echo "coverage-go: test.log was not created (push or tests interrupted?). Run again and wait for the full suite (~8–12 min in Docker)." >&2
  exit 1
fi
sed -E 's/[[:space:]]+coverage: .*//' test.log
rm -f test.log

if [ "$status" -ne 0 ]; then
  echo "coverage-go: go test failed (exit $status)." >&2
  exit "$status"
fi

if [ ! -s coverage.out ] || ! head -n 1 coverage.out | grep -q '^mode:'; then
  echo "coverage-go: coverage.out is missing or incomplete (often after Ctrl+C during pre-push). Remove backend/coverage.out and retry." >&2
  exit 1
fi

if ! go tool cover -func=coverage.out >/tmp/cover-func.txt 2>/tmp/cover-func.err; then
  echo "coverage-go: invalid coverage profile (merge interrupted?). Delete backend/coverage.out and run pre-push again without interrupting." >&2
  cat /tmp/cover-func.err >&2
  rm -f coverage.out
  exit 1
fi

grep -v '\.gen\.go:' coverage.out > coverage.filtered.out
mv coverage.filtered.out coverage.out

if ! go tool cover -func=coverage.out >/tmp/cover-func.txt 2>/tmp/cover-func.err; then
  echo "coverage-go: coverage profile invalid after filtering .gen.go lines." >&2
  cat /tmp/cover-func.err >&2
  exit 1
fi

tail -n 1 /tmp/cover-func.txt
missing=$(awk '$3 != "100.0%" && $1 != "total:"' /tmp/cover-func.txt)
rm -f /tmp/cover-func.txt /tmp/cover-func.err
if [ -n "$missing" ]; then
  echo "Functions below 100% coverage:"
  echo "$missing"
  exit 1
fi
