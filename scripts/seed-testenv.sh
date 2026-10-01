#!/bin/sh
# Fills the test servers with data for listing and transfer tests.
#   scripts/seed-testenv.sh           50,000 small files and files with unusual names
#   scripts/seed-testenv.sh --large   also 1 GiB and 5 GiB files
# Requires: docker compose -f compose.dev.yaml --profile testenv up -d
set -eu

cd "$(dirname "$0")/.."

large=false
[ "${1:-}" = "--large" ] && large=true

seed() {
  service="$1"
  dir="$2"
  owner="$3"
  docker compose -f compose.dev.yaml --profile testenv exec -T "$service" sh -s -- "$dir" "$owner" "$large" <<'INNER'
set -eu
dir="$1"; owner="$2"; large="$3"
mkdir -p "$dir/seed/many" "$dir/seed/names"
cd "$dir/seed/many"
awk 'BEGIN { for (i = 1; i <= 50000; i++) printf "file-%05d.txt\n", i }' | xargs -n 2000 touch
cd ../names
touch "space in name.txt" "ñandú.txt" "日本語.txt" "emoji-😀.txt" "quote'apostrophe.txt" "comma,semicolon;name.txt" "UPPER.TXT" "tab	name.txt"
mkdir -p "carpeta con ñ" "空のフォルダ"
cd ..
if [ "$large" = "true" ]; then
  dd if=/dev/zero of=1g.bin bs=1M count=1024 2>/dev/null
  dd if=/dev/zero of=5g.bin bs=1M count=5120 2>/dev/null
fi
chown -R "$owner" "$dir/seed"
INNER
}

seed sftp /home/transferly/upload 1001:100
seed ftp /srv/ftp/upload transferly
echo "seeded: 50000 files in seed/many, unusual names in seed/names$($large && echo ', 1g.bin and 5g.bin in seed')"
