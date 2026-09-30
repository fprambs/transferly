# ADR-0017: Encrypted export of the sites and their secrets

- Status: accepted
- Date: 2026-09-29

## Context

A workspace's sites are only in Sentinel's database, and their secrets are sealed with the workspace key, which only exists unlocked in the sessions of its members (ADR-0002, ADR-0007). A person who wants a backup, or to move the sites to another Sentinel instance, has no way to take them out with their secrets. The FileZilla import (4.8) brings sites in, but its format keeps passwords in base64, which is no protection.

## Decision

**The export is a JSON file sealed with a password of its own,** chosen for the export and at least 12 characters, as the master password. The password derives a key with Argon2id (the vault's default cost, recorded in the file with a random salt), and the key seals the content with AES-256-GCM. The header (format, version, cost and salt) is the additional data of the seal, so a file whose cost or salt was changed does not open.

**The content holds the settings of every site, the path of names of its collection, the empty collections, and the secrets in clear text inside the seal.** Secrets are opened with the key of the session and sealed again under the export key; the workspace key, the vault's DEK and the sealed blobs of the database never leave. Paths are names, not identifiers, so the file imports into any workspace of any instance. Bookmarks, trusted server keys and transfer history stay out.

**Exporting needs the admin or owner role and the vault unlocked,** is audited with the number of sites, and is answered as a download. Nothing is kept on the server. Editors manage and connect with the sites, but the API never shows them a secret; an export shows every one, so it stays with the roles that manage the members (security review of phase 4).

**Importing a Sentinel file reuses the FileZilla import:** the same dry run, conflict policy (skip or rename), target collection and one result per site. Storing its secrets needs the vault unlocked. A wrong password and a file that was tampered with are the same refusal, `wrong_export_password`; a file that is not an export is `invalid_export`. A cost above 256 MiB, 10 passes or 8 lanes is refused before deriving anything, so a crafted file cannot exhaust the server, and derivations share the vault's bounded pool.

## Consequences

- Positive: backups and moves between instances keep every secret, protected by a password the person chooses and that Sentinel never stores.
- Negative: whoever has the file and its password has every secret in it; the password cannot be recovered, and a lost one loses the file. Sites of the ask and interactive logon types export without a password, since they never stored one.
