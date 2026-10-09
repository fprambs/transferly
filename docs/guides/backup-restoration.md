# Backup and restoration

A recoverable installation needs both the PostgreSQL database and the instance state in `/data`. The instance key encrypts secrets in the database. A database dump alone does not recover those secrets. Keep backups private and store an encrypted copy outside the installation.

The scripts below support a single application instance using the production Compose service names and persistent volumes. They require Docker Compose, GNU tar and sha256sum on the operator's Linux machine. Stop any additional application replicas or writers before capturing a backup. An externally supplied `TRANSFERLY_INSTANCE_KEY` must also be preserved in the deployment's secret manager; these scripts capture the file-based instance key.

## Capture

```sh
sh scripts/backup.sh /private/backups/transferly-2026-10-08
```

The destination must be new. The script copies immutable instance state, stops the application, captures a PostgreSQL custom-format dump, records checksums, then starts the application again. It sets restrictive permissions. A failed or interrupted backup without valid checksums must not be restored.

## Restore

Restoration replaces the selected installation's database and instance state. Capture its current backup first. Select the target explicitly:

```sh
sh scripts/restore-backup.sh /private/backups/transferly-2026-10-08 compose.yaml app
```

The script verifies checksums before stopping the application, restores PostgreSQL, copies instance state with the runtime UID/GID 65532, and waits for the application health check. If database restoration fails, it leaves the application stopped rather than serving a partially restored database. Diagnose the failure and repeat with a verified backup. Remote server files are outside this backup's scope.

After restoration, sign in through a fresh session, unlock the vault and read a file through a saved credential. Check trusted server keys and the instance key checksum. Never print decrypted credentials or recovery kits in a report.

## Isolated recovery rehearsal

`compose.restore-test.yaml` uses separate persistent volumes, a synthetic account and an isolated SFTP server, with no published ports:

```sh
docker compose -f compose.restore-test.yaml up -d --wait postgres app sftp
docker compose -f compose.restore-test.yaml run --rm -T --no-deps verify python3 scripts/verify-backup.py seed
sh scripts/backup.sh /tmp/transferly-audit/recovery-backup compose.restore-test.yaml app
sh scripts/restore-backup.sh /tmp/transferly-audit/recovery-backup compose.restore-test.yaml app
docker compose -f compose.restore-test.yaml run --rm -T --no-deps verify python3 scripts/verify-backup.py check
```

Build the `transferly:test` and `transferly-dev-tools` images first. Create `/tmp/transferly-audit` writable by the operator. The `verify` service reads the repository and saves only synthetic resource identifiers in its report directory. The rehearsal performed on 2026-10-08 recovered the account, cold vault unlock, trusted host key, decrypted SFTP credential and exact file bytes.

## Interrupted uploads and upgrade compatibility

New HTTP uploads stage into a sibling `transferly-<transferId>.part`, retaining ownership through the persisted transfer record. A resumed transfer must match its user, workspace, site and destination. Previous `<destination>.part` files are not adopted automatically, because their ownership cannot be established. Keep those remote files until the operator chooses to recover or remove them; restart the upload with a new transfer identifier.

Destination serialization covers one application process and a normalized workspace/site/path. Distinct site configurations, symlinks, other application replicas and external remote clients can still identify the same physical file. Atomic replacement is used only when the adapter and server support it; an unsupported replacement refuses before overwriting the original.

A final transfer record update retries three times within a shared five-second timeout. Each failure is logged; progress writes are coalesced into later saves. If the store remains unavailable, restart reconciliation marks records left active as interrupted. Verify the actual remote file before retrying an ambiguous operation. These database retries never repeat a remote write or command.

## History retention and remote staging

Transfer listings already cap pages at 200 items; audit listings cap pages at 500 and support their existing filters/cursors. For storage retention, `scripts/prune-history.sql` supplies an operator-controlled, preview-first maintenance operation. Its defaults retain successful transfers for 90 days and audit events for 365 days. Minimum values are 30 and 90 days respectively; choose longer periods for your installation. Capture and verify a backup before applying retention.

Run it with `psql` against the explicitly selected installation. Omitting `apply` or setting it to `false` reports eligible counts without deleting rows. Only an explicit `-v apply=true` deletes a batch of up to 1000 records per table within a 30-second transaction timeout. Interrupted/failed/canceled uploads and unresolved copies remain available for recovery; downloads in terminal states can expire. Active transfers never qualify. No automatic schedule is installed.

This operation does not delete remote `.part` files. Review a remote staging file against the transfer ID, workspace/site, destination and terminal status before removing it. Preserve a file whose owner or outcome is uncertain, including all legacy `<destination>.part` files. Automatic recursive cleanup of remote servers is outside this candidate's guarantees.
