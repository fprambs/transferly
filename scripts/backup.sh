#!/bin/sh
# Captures a quiesced PostgreSQL database and immutable instance state for a single application instance.
set -eu
umask 077

backup_dir=${1:?Usage: backup.sh DESTINATION [COMPOSE_FILE] [APP_SERVICE]}
compose_file=${2:-compose.yaml}
app_service=${3:-app}
[ ! -e "$backup_dir" ] || { echo "Backup destination already exists" >&2; exit 1; }
mkdir -p "$backup_dir/data"
docker compose -f "$compose_file" cp "$app_service:/data/." "$backup_dir/data"
docker compose -f "$compose_file" stop "$app_service"
trap 'docker compose -f "$compose_file" start "$app_service"' EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
docker compose -f "$compose_file" exec -T postgres pg_dump -U transferly -Fc transferly > "$backup_dir/database.dump"
(cd "$backup_dir" && sha256sum database.dump data/instance.key > checksums.sha256)
echo "Backup captured in $backup_dir"
