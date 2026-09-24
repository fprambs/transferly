# Dates and Time Zones

Sources: [MDN Intl.DateTimeFormat](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Intl/DateTimeFormat/DateTimeFormat), [Go time/tzdata](https://pkg.go.dev/time/tzdata), [IANA time zones](https://data.iana.org/time-zones/tz-link.html).

## Rules

- Store instants as `timestamptz`; use UTC `time.Time` in Go; send RFC 3339 with `Z`.
- Never store formatted strings or fixed offsets; store IANA names (`America/Santiago`).
- Convert only at the edge: the frontend formats through `lib/datetime` with an explicit `timeZone`.
- Temporal is ES2026 (Chrome 144+, Firefox 139+, Edge 144+; Safari partial): keep `Intl` behind `lib/datetime` and migrate when Safari ships.
- Go: import `time/tzdata` in `main`; containers run with `TZ=UTC`; read time from `ports.Clock`.

## Remote Timestamps

| Source | Zone |
|---|---|
| SFTP | epoch seconds, UTC |
| FTP `MLSD` | UTC by specification |
| FTP `MDTM` | UTC by specification, but vsftpd with `use_localtime=YES` answers local time: do not rely on it |
| FTP `LIST` | unspecified: use `sites.list_timezone`, detected by comparing the listing of a just-uploaded probe file with the client clock (round to 15 minutes) |

`LIST` can omit seconds or the year, so entries carry a `modified_precision` field.

## Tests

- Fixed clock (`testutil.FakeClock`); explicit zones including DST changes and half-hour offsets.
- UI suites run under several `TZ` values.
