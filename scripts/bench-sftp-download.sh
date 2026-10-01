#!/bin/sh
# Compares downloading a 64 MiB file from the SFTP server of the isolated test stack with OpenSSH's sftp client and with Transferly's API,
# optionally with latency added to the server (4.13). Run with the stack of compose.test.yaml up:
#
#   ./scripts/bench-sftp-download.sh        # local network
#   ./scripts/bench-sftp-download.sh 20ms   # 20 ms added to every packet the SFTP server sends
#
# It touches only the isolated test stack: its sftp container gets the file and, for a while, the latency.
set -eu
cd "$(dirname "$0")/.."
delay=${1:-}
compose="docker compose -f compose.test.yaml"
sftp=$($compose ps -q sftp)
$compose exec -T sftp sh -c 'dd if=/dev/urandom of=/home/transferly/upload/bench.bin bs=1M count=64 2>/dev/null; chown 1001 /home/transferly/upload/bench.bin'
netem() { docker run --rm --net "container:$sftp" --cap-add NET_ADMIN alpine:3.22 sh -c "apk add -q iproute2 >/dev/null && $1"; }
if [ -n "$delay" ]; then
  netem "tc qdisc replace dev eth0 root netem delay $delay"
  trap 'netem "tc qdisc del dev eth0 root"' EXIT
fi
docker run --rm -i --network "$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}' "$sftp")" alpine:3.22 sh <<'BENCH'
set -eu
apk add -q curl jq openssh-client sshpass coreutils >/dev/null
B=http://transferly:8080 J=/tmp/jar A='{"email":"e2e@example.com","password":"a long e2e password"}' M='{"masterPassword":"a long e2e master password"}'
code=$(curl -s -o /tmp/r -w '%{http_code}' -c $J -H 'Content-Type: application/json' -d "$A" $B/api/v1/setup)
[ "$code" = 409 ] && curl -s -o /tmp/r -c $J -H 'Content-Type: application/json' -d "$A" $B/api/v1/auth/login
H="X-CSRF-Token: $(jq -r .csrfToken /tmp/r)"
for step in setup unlock; do curl -s -o /dev/null -b $J -H "$H" -H 'Content-Type: application/json' -d "$M" $B/api/v1/vault/$step; done
WS=$(curl -s -b $J $B/api/v1/workspaces | jq -r '.[0].id')
Q=$(curl -s -b $J -H "$H" -H 'Content-Type: application/json' -d '{"protocol":"sftp","host":"sftp","port":22,"username":"transferly","authMethod":"password","password":"transferly"}' $B/api/v1/workspaces/$WS/quick-connections | jq -r .id)
F=$B/api/v1/workspaces/$WS/sites/$Q/files
if [ "$(curl -s -o /tmp/l -w '%{http_code}' -b $J "$F?path=/")" = 409 ]; then
  curl -s -o /dev/null -b $J -H "$H" -H 'Content-Type: application/json' -d "{\"host\":\"sftp\",\"port\":22,\"keyType\":\"$(jq -r .params.keyType /tmp/l)\",\"fingerprint\":\"$(jq -r .params.fingerprint /tmp/l)\"}" $B/api/v1/workspaces/$WS/known-hosts
fi
s=$(date +%s.%N); sshpass -p transferly sftp -q -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null transferly@sftp:/upload/bench.bin /tmp/b >/dev/null; e=$(date +%s.%N)
echo "openssh sftp: $(echo "$e $s" | awk '{printf "%.1f MB/s", 64/($1-$2)}')"
curl -s -o /dev/null -b $J "$F/content?path=/upload/bench.bin" -w 'transferly:     %{speed_download}\n' | awk '{printf "%s %.1f MB/s\n", $1, $2/1048576}'
curl -s -o /dev/null -b $J -H "$H" -X DELETE "$F?path=/upload/bench.bin"
BENCH
