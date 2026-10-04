# ADR-0022: Sites described by their connector's schema

- Status: accepted
- Date: 2026-10-03

## Context

ADR-0019 introduced the connector catalog. Each connector has a descriptor with a configuration schema, but sites still had a fixed shape made for SFTP and FTP:

- the protocol, the FTP encryption, the host, port and username, and the logon type;
- the listing time zone and the character set;
- a proxy and a jump host.

The rules that kept a site valid were written by hand in the site service. Examples are the port of implicit FTPS, the logon types each protocol allows, the anonymous user and jump hosts only for SFTP.

The site form, the quick connect dialog, the store, the API, the FileZilla import and the backups all repeated that shape. A connector such as S3 (endpoint, bucket, region, access keys) does not fit it. Adding it would mean touching every one of those places, which is what the catalog is meant to avoid.

## Decision

**A site is its connector plus a configuration.** A site keeps what every site has:

- its identity, workspace and collection;
- its name, label color, speed limit and default remote folder.

On top of that it has:

- the type of its connector (`sftp`, `ftp`, later `s3`…);
- a configuration, a map from setting names to text values;
- the kinds of the secrets stored for it in the vault.

The API, the store, quick connections, backups and the FileZilla import all use that shape. The protocol-specific fields and the columns that held them are removed.

**The schema says everything the form and the validation need.** A field has:

- a name, a kind and a group (the tab of the form it belongs to);
- whether it is required, its default and the defaults that depend on other fields (port 990 when the encryption is implicit);
- its choices and bounds;
- a condition that says when it applies, from the value of another field (the jump host's username only when a jump host is set, the password only for the logon types that use one).

A field that does not apply is dropped, and so is its stored secret.

The kinds are:

- **text, number, boolean and choice** for plain settings;
- **host**, a server name or address that the network policy of the instance must allow, with the port field that goes with it;
- **time zone**, an IANA zone;
- **secret**, sealed in the vault and never returned;
- **private key**, a secret that must be a usable private key, decrypted with the passphrase field it names.

Rules that a schema cannot say are kept in a normalizer of the descriptor. An example is forcing the anonymous user on anonymous sites. Each normalizer is small and tested with its connector.

**Field names are stable identifiers, in snake case.** They double as the kinds of the secrets in the vault, so stored secrets keep their kinds (`password`, `private_key`, `jump_password`…). Some names carry the same meaning in every connector that has them, and features that are not connector-specific read them:

- `host` and `port`, for the trusted server keys and the connection log;
- `username`;
- `auth`, the logon type, for the password asked when connecting (ADR-0010) and the interactive logon (ADR-0016).

Features that depended on the protocol depend on the connector's capabilities instead, such as the permissions column needing permissions.

**Connections are opened by the connector.** The adapter layer registers an opener for each connector type, and the connector of sites (`connect`) looks it up. The opener receives the site with its configuration and secrets, plus the services every connection needs: trusted server keys, the network guard, the connection log and the prompts. SFTP and FTP become two openers. A new connector adds its descriptor and its opener and touches nothing else.

**The interface draws the form from the schema.** It shows one tab per group, then the fields that apply, the default as a placeholder and secrets as password inputs. Labels come from the translation files by connector and field name, with shared fallbacks for common fields such as the host. The same component draws the site dialog and quick connect.

**Failures name the field.** A configuration that does not follow the schema is `invalid_config` (422), with the field and the reason, so the form marks the field. A host the network policy refuses stays `destination_blocked`, and a private key that cannot be used gets a reason of its own.

**No compatibility with the previous shape.** Nobody else uses Transferly yet, so this follows ADR-0020:

- a migration moves the settings of existing sites into the configuration and drops the old columns;
- the API takes and returns only the new shape;
- backups get a new format version and refuse the old one.

## Consequences

- Positive:
  - one shape for every site, whatever its connector, from the database to the form;
  - S3, WebDAV and the rest only add a descriptor, an opener and translations;
  - the validation rules live next to the connector they belong to, and the form cannot drift from the API.
- Negative:
  - a large change to sites, quick connections, the import, backups, the API and the interface at once;
  - the schema must be expressive enough for the SFTP and FTP forms, so it carries conditions and dependent defaults instead of a flat list of fields;
  - configuration values are text, so each opener parses its own settings, and the descriptor tests check that every setting parses.
