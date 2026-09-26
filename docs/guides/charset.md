# Character sets of file names (golang.org/x/text/encoding)

Sources: `go doc golang.org/x/text/encoding` and the `charmap`, `japanese`, `korean`, `simplifiedchinese` and `traditionalchinese` packages of `golang.org/x/text` v0.42. Adapter: `backend/internal/adapters/remotefs/ftpfs/charset.go`; mapping of site settings: `connect.charsetOf`.

## Why

FTP servers older than RFC 2640 send and expect file names in a national character set, not UTF-8. Names are bytes on the wire; the client works with UTF-8 strings, so it must convert at the boundary or names with accents show garbled and cannot be opened.

## Rules

- A site has a `charset` (`sites.charset`, default `utf-8`) chosen from `site.Charsets`: UTF-8, ISO-8859-1, -2 and -15, Windows-1250, -1251 and -1252, KOI8-R, Shift_JIS, EUC-JP, GBK, Big5 and EUC-KR. SFTP only uses UTF-8 and refuses another one with `invalid_charset`.
- `encoding.Encoding` is used through `NewEncoder().String` (UTF-8 to the server's bytes; it fails when a character cannot be represented) and `NewDecoder().String` (never fails: undefined bytes become the replacement character).
- Every path the adapter sends goes through the encoder (`FS.sp`), and every name read from a listing through the decoder (`convert`). Paths are checked before anything is sent: a name the set cannot hold is `remotefs.ErrInvalid`, and what would be written is validated again with `validPath`, so a multi-byte set cannot smuggle a control character into the control connection.
- With a legacy character set the client library is told not to send `OPTS UTF8 ON` (`DialWithDisabledUTF8`).
- Names travel as Go strings holding the server's raw bytes while they are inside the library; only the adapter converts them.
- Data connections carry file contents and are never converted.

## Tests

`TestCodec` (round trips, unrepresentable names, undefined bytes, Shift_JIS), `TestCharset_Names` against a scripted server that only answers to the bytes of ISO-8859-1, and `TestVsftpd_Charset` against a real vsftpd.
