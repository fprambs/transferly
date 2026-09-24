# Spike: SFTP Throughput

Program: `backend/spikes/sftpthroughput`. Path: client, Toxiproxy (latency and bandwidth toxics), `atmoz/sftp`; everything on one Docker network. Client: `pkg/sftp` v1.13.11 over `x/crypto/ssh`, 32 KiB packets, files of zeros.

## Question

Does pipelining keep an SFTP link full at 100 ms round-trip time (target: at least 80 percent of the bandwidth)?

## Results

| Scenario | Upload MB/s | Download MB/s | Share of link |
|---|---|---|---|
| no limits, 0 ms, pipelined 64 | 78.8 | 64.9 | environment ceiling |
| no limits, 100 ms, sequential | 0.3 | 0.3 | |
| no limits, 100 ms, pipelined 64 | 18.3 | 17.7 | |
| no limits, 100 ms, pipelined 256 | 18.3 | 17.7 | |
| 100 Mbit, 100 ms, sequential | 0.3 | 0.3 | 2% |
| 100 Mbit, 100 ms, pipelined 64 | 11.1 | 10.8 | **86%** |
| 1 Gbit, 100 ms, pipelined 64 | 18.3 | 17.5 | 14% |
| 1 Gbit, 100 ms, pipelined 256 | 18.3 | 17.5 | 14% |
| 1 Gbit, 100 ms, 4 files, 1 SSH connection | 64.0 | 59.2 | 47% |
| 1 Gbit, 100 ms, 4 files, 4 SSH connections | 68.8 | 61.6 | 49% |
| 1 Gbit, 100 ms, 8 files, 1 SSH connection | 71.7 | 62.7 | 50% |

## Findings

- Without pipelining, one 32 KiB request per round trip caps the speed at 0.3 MB/s: unusable.
- With pipelining the target holds up to about 100 to 150 Mbit: 86 percent of a 100 Mbit link at 100 ms.
- Raising `MaxConcurrentRequestsPerFile` from 64 to 256 changes nothing. The limit is the SSH channel window of `x/crypto/ssh` (2 MiB): `2 MiB / 0.1 s` is about 20 MB/s per channel.
- The window is per channel, so parallel transfers add up: four files reach about 64 to 69 MB/s. One shared SSH connection performs about as well as one connection per file.
- The environment ceiling (Toxiproxy plus SSH encryption on one machine) is about 70 to 80 MB/s, which is why 8 files stop scaling and 80 percent of 1 Gbit cannot be demonstrated here.

## Decisions

- Keep `UseConcurrentReads`, `UseConcurrentWrites` and 64 requests per file.
- Run several transfers at once per site as separate SFTP clients on the same SSH connection; default 4, configurable per site.
- Splitting one very large file into ranges over several channels is a later optimization for fast, high-latency links.
- Document that a single stream cannot exceed roughly `2 MiB / RTT`.
- Keep this program as the throughput benchmark; run it before releases and compare with these numbers.
