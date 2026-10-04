# S3 (minio-go, gofakes3, Versity Gateway)

Sources: [minio-go API reference](https://github.com/minio/minio-go/blob/master/docs/API.md), [minio-go repository](https://github.com/minio/minio-go), [gofakes3](https://github.com/johannesboyne/gofakes3), [Versity Gateway](https://github.com/versity/versitygw). Decision: [ADR-0023](../adr/0023-s3-connector.md).

## Versions

- `github.com/minio/minio-go/v7` v7.3.0: the client, used by `internal/adapters/remotefs/s3fs`.
- `github.com/johannesboyne/gofakes3` v1.2.0: an S3 server in memory, for tests only (`s3fs/s3test`).
- `versity/versitygw`, pinned by digest: the real S3 server of the test stack and of the `testenv` profile. MinIO stopped publishing images of its community edition, so it is not used.

## Setup

- `minio.New(host:port, &minio.Options{...})`: the endpoint has no scheme nor path; `Secure` selects HTTPS.
- `Transport` is our own `*http.Transport`, so every connection goes through the dialer of the network guard and the proxies, with `TLSClientConfig.RootCAs` nil for the system authorities.
- `BucketLookup: BucketLookupAuto` uses virtual-host URLs where the service supports them (AWS) and path-style URLs elsewhere, such as an IP or a local host.
- `MaxRetries: 3`: the default of 10 makes an unreachable server take long to fail.
- Anonymous access uses `credentials.NewStatic("", "", "", credentials.SignatureAnonymous)`; an access key uses `SignatureV4`.

## Patterns

- `ListObjectsIter` with `Recursive: false` lists one level, folding deeper keys into common prefixes; breaking out of the loop stops the requests. It stops quietly when the context ends, so the adapter checks the context after the loop and reports it as an error.
- `PutObject(..., -1, PutObjectOptions{PartSize: 16 MiB})` streams an upload of unknown size in parts. Without `PartSize` the client buffers parts of hundreds of MiB.
- `GetObject` is lazy: it only validates names and every failure appears on the first `Read`. Ranges use `SetRange(offset, 0)` for "from offset to the end".
- `CopyObject` copies up to 5 GiB; `ComposeObject` with one source copies larger objects in parts (`UploadPartCopy`).
- Errors are `minio.ErrorResponse`; read them with `errors.As` and its `Code` (`NoSuchKey`, `NoSuchBucket`, `AccessDenied`, `InvalidAccessKeyId`, `SignatureDoesNotMatch`). HEAD answers have no body, so a refused HEAD reads as `AccessDenied`.
- Drain the channel of `RemoveObjects`; returning on the first error leaves its goroutine blocked.

## Anti-patterns

- Copying folder markers on the server: some services (Versity on POSIX) keep them as directories; write a new empty marker instead.
- Closing an upload pipe cleanly after the data failed: that completes a short object. Close it with the error, which aborts the upload.

## Tests

- `s3test.Start` serves gofakes3 over HTTPS with its own certificate (`RootCAs`) and injects S3 errors per request. Over plain HTTP the client signs uploads in chunks (`aws-chunked`), which gofakes3 does not decode, so plain servers only suit tests that do not write. gofakes3 also lacks `UploadPartCopy`; `s3test` performs it.
- `TestConformance_Gateway` and `TestGateway_MultipartUpload` run against Versity Gateway when `TRANSFERLY_TEST_S3_ADDR` names it (profile `s3` of `compose.test.yaml`), over plain HTTP, which exercises chunked uploads.
