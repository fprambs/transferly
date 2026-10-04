# ADR-0023: S3 connector

- Status: accepted
- Date: 2026-10-04

## Context

Roadmap item 5.2 adds the first connector that is not a file server: S3 and the services that speak its API (MinIO, Cloudflare R2, Wasabi, Backblaze B2 and others). It is also the test of the catalog (ADR-0019, ADR-0022): a new connector should only add a descriptor, an opener, an adapter and translations.

S3 differs from SFTP and FTP in ways the rest of Transferly sees:

- a bucket holds objects under keys; folders do not exist, only prefixes shared by keys;
- an object is written whole: it cannot be appended to or written from an offset;
- objects have no modes, and their modification time is set by the service when they are written;
- a rename does not exist; an object can be copied on the server and the original deleted;
- requests are HTTPS calls signed with an access key, and the server is identified by its certificate and the public certificate authorities, not by a key the user trusts.

## Decision

**Library.** The adapter uses `minio-go` v7, the client of MinIO for S3 and every compatible service. It is one module, streams uploads of unknown size in parts and takes a custom HTTP transport, which is how the network guard and the proxies reach it. The in-process tests use `gofakes3`, an S3 server in memory.

**Folders are emulated, as the AWS console, rclone and Cyberduck do.** The path `/a/b.txt` is the key `a/b.txt`. Creating the folder `/a` writes an empty marker object `a/`. A folder exists when its marker exists or when some key starts with its prefix, so buckets filled by other tools browse normally. With this the adapter keeps the semantics of the port: a folder must exist before something is written in it, an empty folder survives, and removing a folder that is not empty is refused. The connector declares no real folders, so the interface can say that a folder is a prefix.

**Renames copy on the server.** Renaming a file copies the object on the server and deletes the original; renaming a folder does so for every key under it. Nothing goes through Transferly, but a folder rename is not atomic: if it fails halfway, part of the keys are under the new name. Objects larger than 5 GiB are copied in parts.

**What S3 does not do, it does not declare.** No permissions, no times that can be set, and no resumable writes. An interrupted upload starts again. Uploads stream in parts of 16 MiB, which bounds the memory each upload uses and allows objects up to about 156 GiB.

**Uploads go straight to their key.** Uploads to SFTP and FTP are staged in a `.part` file and renamed at the end, so an interrupted one can resume. On a file system that cannot resume, the file service writes the final path directly: an object appears whole when its upload completes, and a rename would copy it on the server. When the data stops arriving halfway, the write is canceled and the upload aborted, so the object keeps what it had.

**Settings.** The schema has:

- the endpoint (host and port, `s3.amazonaws.com` and 443 by default), checked by the network guard like every host;
- whether to use HTTPS (on by default; plain HTTP is only for local services and is marked in the connection log);
- the region (optional; AWS finds it when empty);
- the bucket;
- the logon type: an access key, or anonymous for public buckets;
- the access key ID, and the secret access key sealed in the vault;
- the proxy fields every connector shares.

**Certificates come from the public authorities.** HTTPS is verified against the system roots, which the production image carries. Self-signed certificates, as some private MinIO installations use, are not supported in this first version.

**Errors keep the port's meaning.** Missing keys and buckets are `ErrNotFound`, refused credentials `ErrAuth`, refused access `ErrPermission`, and network failures `ErrConnectionLost`.

**Tests.** The adapter passes the shared suite against `gofakes3` and against Versity Gateway, a real S3 server, in the isolated test stack; MinIO stopped publishing images of its community edition. The connector passes the catalog conformance suite, with `gofakes3` as its reference server.

## Consequences

- Positive:
  - buckets of AWS and every compatible service can be browsed, uploaded to and downloaded from like any site;
  - the catalog proved enough for a connector unlike SFTP and FTP;
  - the shared suite keeps folder semantics the same for every connector.
- Negative:
  - every write checks that its folder exists, an extra request per upload;
  - folder renames are not atomic, and big folders take one copy per object;
  - interrupted uploads restart from zero; resumable multipart uploads are left for later;
  - self-signed certificates and credentials other than static access keys (session tokens, roles) are left for later.
