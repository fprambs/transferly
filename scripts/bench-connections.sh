#!/bin/sh
# Run the API benchmark with synthetic data in a disposable Compose project.
set -eu
cd "$(dirname "$0")/.."
project="transferly-benchmark-$$"
override=$(mktemp)
cat > "$override" <<'YAML'
services:
  transferly:
    environment:
      TRANSFERLY_ALLOWED_NETWORKS: ${TRANSFERLY_BENCHMARK_SUBNET:-}
YAML
compose() { docker compose -f compose.test.yaml -f "$override" -p "$project" "$@"; }
trap 'compose down --volumes >&2; rm -f "$override"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

compose up -d --build --wait postgres sftp >&2
TRANSFERLY_BENCHMARK_SUBNET=$(docker network inspect "${project}_default" --format '{{(index .IPAM.Config 0).Subnet}}')
export TRANSFERLY_BENCHMARK_SUBNET
compose up -d --build --wait transferly >&2
checksum=$(compose exec -T sftp sh -ec '
    file=/home/transferly/upload/connection-bench.bin
    dd if=/dev/urandom of="$file" bs=1M count=16 2>/dev/null
    chown 1001 "$file"
    sha256sum "$file" | cut -d " " -f 1
')
compose run --rm --no-deps -T unit python3 scripts/bench-connections.py "$checksum" "$@"
