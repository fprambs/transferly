#!/bin/sh
# Replaces the database and instance state of an explicitly selected, persistent-volume installation.
set -eu
backup_dir=${1:?Usage: restore-backup.sh BACKUP COMPOSE_FILE APP_SERVICE}
compose_file=${2:?Select the installation to replace explicitly}
app_service=${3:?Select the application service explicitly}
(cd "$backup_dir" && sha256sum -c checksums.sha256)
docker compose -f "$compose_file" stop "$app_service"
docker compose -f "$compose_file" exec -T postgres pg_restore -U transferly --clean --if-exists --no-owner --exit-on-error -d transferly < "$backup_dir/database.dump"
# The production image runs as 65532. Host-side copies belong to the operator;
# restore the original runtime ownership without needing a shell in that image.
tar --owner=65532 --group=65532 -C "$backup_dir/data" -cf - . | docker compose -f "$compose_file" cp -a - "$app_service:/data"
docker compose -f "$compose_file" up -d --no-build --no-recreate --wait "$app_service"
echo "Restored the selected installation"
