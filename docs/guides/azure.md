# Azure Storage (Blob Storage and Azure Files)

Sources: [Authorize with Shared Key](https://learn.microsoft.com/en-us/rest/api/storageservices/authorize-with-shared-key), [Create a service SAS](https://learn.microsoft.com/en-us/rest/api/storageservices/create-service-sas), [Blob service REST API](https://learn.microsoft.com/en-us/rest/api/storageservices/blob-service-rest-api), [File service REST API](https://learn.microsoft.com/en-us/rest/api/storageservices/file-service-rest-api), [error codes](https://learn.microsoft.com/en-us/rest/api/storageservices/common-rest-api-error-codes), [Azurite](https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite). Decision: [ADR-0025](../adr/0025-azure-connectors.md).

## Versions

- No Azure SDK. The package `remotefs/azure` speaks the REST APIs with `net/http`; `azblobfs` and `azfilefs` build on it.
- Every request sends `x-ms-version: 2023-11-03`.
- Azurite 3.37 runs in the test stack, for Blob only.

## Creating a site

1. Offer **Azure Blob Storage** or **Azure Files** in Settings → Administration → Connectors.
2. Fill in the storage account and the container or share.
3. Choose the logon type:
   - **Account key**: one of the two keys of the account, from Security + networking → Access keys in the Azure portal. It grants everything in the account.
   - **SAS token**: a token limited to the container or share, its permissions and an expiry. It is the safer choice. Blob sites need read, write, delete and list (`sp=rwdl`) and Files sites also need create (`sp=rcwdl`). The leading `?` is optional.
   - **Anonymous**: Blob only, for containers with public access.
4. The Advanced tab replaces the endpoint, for sovereign clouds (`account.blob.core.chinacloudapi.cn`) or emulators (`azurite`, port 10000, HTTPS off).

## Patterns

- **Shared Key.** The string to sign is the verb, eleven standard headers in a fixed order, the `x-ms-` headers sorted and lowercased, then `/{account}{path}` and the query parameters sorted with their values joined by commas. `Content-Length` is empty when it is zero. Emulators take the account in the path too, so their resource reads `/{account}/{account}/…`.
- **SAS tokens** are appended to every URL and never logged; the connection log shows only the logon type.
- **Errors** carry their code in the `x-ms-error-code` header, or in the XML body when the header is missing. `ConditionNotMet` after `If-None-Match: *` means the blob exists. `ServerBusy` and 5xx answers are retryable.
- **Blob listings** use `delimiter=/` and `prefix=`, and follow `NextMarker` until it is empty. A folder marker is a zero-length blob named with a trailing slash.
- **Blob uploads** stage blocks with `comp=block` and base64 block IDs of equal length, then commit them with `comp=blocklist`.
- **Blob copies** use `x-ms-copy-source` and poll `x-ms-copy-status` while it is `pending`.
- **Files uploads** create the file with `x-ms-content-length: 0`, grow it with `comp=properties` and write ranges of at most 4 MiB with `comp=range` and `x-ms-write: update`.
- **Files renames** use `comp=rename` with `x-ms-file-rename-source`; directories are created one by one and removed after their contents.

## Tests

- `azuretest` is a fake of both REST APIs in memory, with faults per request, public containers and pending copies. It rejects requests that are not signed with its key and carry no SAS token.
- `cmd/fakeazure` serves the Files fake as the `fakeazure` service of the test stack (profile `azure`), with `Reports/q3.csv` in its share.
- The `azurite` service runs the official emulator for Blob, with its well-known account `devstoreaccount1`. With `TRANSFERLY_TEST_AZURITE_ADDR` set, `azblobfs` runs the shared suite against it.
- `e2e/specs/azure.spec.ts` creates a container in Azurite with a signed request, then creates a site of each connector and works with their files.
