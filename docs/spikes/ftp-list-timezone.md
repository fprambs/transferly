# Spike: FTP LIST Time Zone Detection

Program: `backend/spikes/ftplisttz`. Server: vsftpd 3.0.3 with `TZ=America/Santiago` and `use_localtime=YES` (`ftp` in the test environment).

## Question

FTP `LIST` timestamps carry no zone. Can the zone be detected reliably, and can the listing then be interpreted correctly?

## Results

| Measurement | Value |
|---|---|
| Client clock at upload (UTC) | 21:55:37 |
| `MDTM` answer | 18:55:37 (local time, not UTC) |
| `LIST` parsed as UTC | 18:55:00 |
| Offset detected through `MDTM` | 0 s (wrong) |
| Offset detected through the client clock | -3 h (correct) |
| `LIST` parsed in `America/Santiago` | 21:55:00, 37 s from the client clock |
| `IsTimePreciseInList` | false (minute precision) |

## Findings

- vsftpd with `use_localtime=YES` answers `MDTM` in local time, against the specification. Detection through `MDTM`, the method FileZilla describes, fails on such servers.
- Comparing the listing time of a file just uploaded with the client clock recovers the offset. Round it to 15 minutes to absorb minute precision and small clock skew; it tolerates skew below about 7 minutes.
- `jlaffaye/ftp` parses listings in the location given by `DialWithLocation`, which is the hook for `sites.list_timezone`.
- `MLSD` is UTC by specification and needs no detection, but vsftpd 3.0.3 does not implement it; a server with `MLSD` is still missing from the test environment.

## Decisions

- Store an IANA zone per site (`sites.list_timezone`), defaulting to UTC.
- Auto-detect on first connection by uploading a probe file and comparing with the client clock; fall back to asking the user. Never trust `MDTM` alone.
- Prefer `MLSD` whenever the server advertises it and skip detection.
- Entries parsed from `LIST` carry `ModTimePrecision` minute (or day for old files).
