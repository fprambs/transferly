# Spike: Streaming Upload Memory

Program: `backend/spikes/streamupload`. A handler streams an HTTP request body into a new SFTP file with `File.ReadFrom` and concurrent writes; the client streams zeros. Server: `atmoz/sftp`.

## Question

Can 5 GiB pass browser, Sentinel, SFTP server without touching the disk, keeping the server under 100 MB of memory?

## Results

| Metric | Value |
|---|---|
| Size | 5 GiB (5 368 709 120 bytes stored, exact) |
| Time | 2 min 35 s |
| Throughput | 33 MB/s (one stream, client and handler in one process) |
| Peak resident memory | **14 MiB** (7 MiB before the upload) |
| Leftover files | none (the handler removed the file) |

## Findings

- Memory stays flat: `ReadFrom` keeps at most `requests x packet size` (2 MiB) in flight, independent of the file size.
- No temporary file is needed; the body is piped straight to the SFTP channel.
- Throughput is bounded by a single SSH channel, see the SFTP throughput spike.

## Decisions

- Uploads use `ReadFrom` on the request body with a context-bound request.
- Resumable uploads (phase 1) send chunks with an offset and reuse the same streaming path with `WriteResume`.
- Add a memory assertion to the upload integration test: peak resident memory must stay well under 100 MB.
