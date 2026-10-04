# ADR-0025: Azure connectors (Blob Storage and Files)

- Status: accepted
- Date: 2026-10-04

## Context

Azure Storage keeps files in two services that matter to Transferly:

- **Blob Storage** keeps blobs in containers, the object store of Azure. Like S3, it has prefixes instead of folders, blobs written whole and copies on the server.
- **Azure Files** keeps shares with real directories. Files are written by ranges at any offset, renamed natively and carry their times, as on a file server.

Both are reached with the account name and one of these:

- an account key, which signs each request with Shared Key;
- a SAS token, a signed query string that grants limited access;
- nothing, for public Blob containers.

OneDrive and SharePoint belong to Microsoft 365, not to Azure Storage, and are left for a later connector with the sign-in of Microsoft.

The official Go SDK pulls in Apache Arrow and other heavy dependencies. A program that only creates a Blob client and a Files client weighs 13 MB.

## Decision

**A small REST client of our own**, as for Google Drive (ADR-0024). It uses `net/http`, Shared Key signing following the documented string to sign, and SAS tokens appended to the query. Every request goes through the network guard and the proxies. Azurite, the official emulator, checks the signing independently for Blob, and the same algorithm signs Files.

**Endpoints.** By default the endpoints are `https://{account}.blob.core.windows.net` and `https://{account}.file.core.windows.net`. An endpoint on the Advanced tab replaces them:

- when its host starts with the account name, it is used as an account host, as in sovereign clouds such as `account.blob.core.chinacloudapi.cn`;
- otherwise it is an emulator that takes the account in the path, as Azurite does (`http://azurite:10000/{account}`).

Plain HTTP is only for local emulators, and the connection log marks it.

### Blob Storage

- **It behaves as S3 does (ADR-0023).** Folders are emulated with prefixes and empty marker blobs whose name ends in a slash, which the Azure portal also shows as folders.
- **Writes are atomic.** Uploads stage blocks of 8 MiB and commit the block list when they finish, so a blob appears whole or not at all. An abandoned upload leaves uncommitted blocks, which Azure discards. Creating a blob that must not exist sends `If-None-Match: *`, so the check and the write are one.
- **Renames copy on the server** and delete the source; a copy that Azure reports as pending is waited for.
- **Capabilities:** renames only. No real folders, permissions, times that can be set or resumable writes.

### Azure Files

- **Real directories and native renames** (`comp=rename`), the directories of a path included.
- **Uploads write ranges.** A file is created empty, then grown and written in ranges of 4 MiB, the largest Azure accepts, so writes resume at any offset.
- **Times.** The last write time is read and set.
- **Capabilities:** real folders, times that can be set, resumable writes that may rewrite earlier bytes, and renames. No permissions: Azure Files keeps NTFS ACLs, which the port does not model. Anonymous access does not exist for shares.

### Tests

- `azuretest`, a fake of both REST APIs in memory, drives the shared suite and the catalog conformance suite for both connectors, with faults per request. It checks that every request is signed with the shared key of its account or carries a SAS token.
- Azurite runs in the isolated test stack. The Blob adapter passes the shared suite against it, and the end-to-end tests use it.
- The fake runs as a service of the stack for Azure Files, which Azurite does not emulate.

## Consequences

- Positive:
  - containers and shares of Azure Storage are browsed and transferred like any site, with account keys, SAS tokens or anonymous public containers;
  - Azure Files keeps resumable uploads and native renames, unlike object stores;
  - no heavy dependency is added.
- Negative:
  - our client must follow changes of the REST APIs by hand;
  - Shared Key is the key of the whole account, so SAS tokens with narrow permissions are the safer choice, which the form says;
  - Microsoft Entra ID sign-in, OneDrive and SharePoint are left for later.
