package ftpfs

import (
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"

	"context"
	"errors"
	"net"
	"net/textproto"
	"regexp"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// TestClassify verifies how replies and failures of the client become the sentinels of the port.
//
// Scenarios:
//   - each reply code maps to its sentinel, and unexpected codes are invalid arguments
//   - a failure that is not a reply is a lost connection, and the errors of a context pass through unchanged
//   - no error stays no error
func TestClassify(t *testing.T) {
	reply := func(code int, msg string) error { return &textproto.Error{Code: code, Msg: msg} }
	for name, tt := range map[string]struct {
		err  error
		want error
	}{
		"service closing":  {reply(421, "bye"), remotefs.ErrConnectionLost},
		"no data channel":  {reply(425, "no"), remotefs.ErrConnectionLost},
		"aborted":          {reply(426, "cut"), remotefs.ErrConnectionLost},
		"not logged in":    {reply(530, "no"), remotefs.ErrAuth},
		"need account":     {reply(532, "acct"), remotefs.ErrAuth},
		"no space":         {reply(452, "full"), remotefs.ErrQuota},
		"quota":            {reply(552, "over"), remotefs.ErrQuota},
		"page type":        {reply(551, "type"), remotefs.ErrInvalid},
		"unavailable":      {reply(550, "No such file"), remotefs.ErrNotFound},
		"name not allowed": {reply(553, "Permission denied"), remotefs.ErrPermission},
		"busy":             {reply(450, "File exists"), remotefs.ErrExists},
		"local error":      {reply(451, "no hint"), remotefs.ErrPermission},
		"syntax":           {reply(500, "what"), remotefs.ErrInvalid},
		"closed":           {net.ErrClosed, remotefs.ErrConnectionLost},
		"eof":              {errors.New("EOF"), remotefs.ErrConnectionLost},
	} {
		if got := classify(tt.err); !errors.Is(got, tt.want) {
			t.Errorf("%s: classify() = %v; want %v", name, got, tt.want)
		}
	}
	if classify(nil) != nil {
		t.Error("classify(nil) != nil")
	}
	if got := classify(context.Canceled); !errors.Is(got, context.Canceled) || errors.Is(got, remotefs.ErrConnectionLost) {
		t.Errorf("classify(canceled) = %v; want it unchanged", got)
	}
	if got := classify(context.DeadlineExceeded); !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("classify(deadline) = %v", got)
	}
}

// TestRefine verifies how the text of a "file unavailable" reply decides what went wrong.
//
// Scenarios:
//   - hints for a directory that is not one, one that is not empty, a missing path and an existing path
//   - without a hint the failure is a permission problem
func TestRefine(t *testing.T) {
	for text, want := range map[string]error{
		"Not a directory":              remotefs.ErrNotDirectory,
		"Directory not empty":          remotefs.ErrNotEmpty,
		"open /x: file does not exist": remotefs.ErrNotFound,
		"No such file or directory":    remotefs.ErrNotFound,
		"Can't find it":                remotefs.ErrNotFound,
		"File already exists":          remotefs.ErrExists,
		"550 exists":                   remotefs.ErrExists,
		"Access denied":                remotefs.ErrPermission,
		"":                             remotefs.ErrPermission,
	} {
		if got := refine(text); !errors.Is(got, want) {
			t.Errorf("refine(%q) = %v; want %v", text, got, want)
		}
	}
}

// TestToEntry verifies how listed entries are described.
//
// Scenarios:
//   - files, directories and links get their type bits and nominal permissions
//   - the precision follows the listing: seconds for MLSD, minutes for LIST, days when LIST shows only a date
//   - the times are UTC and the path joins the directory
func TestToEntry(t *testing.T) {
	when := time.Date(2026, 3, 8, 7, 30, 15, 0, time.FixedZone("x", 3600))
	file := toEntry("/d", &ftp.Entry{Name: "f", Type: ftp.EntryTypeFile, Size: 7, Time: when}, true)
	if file.IsDir() || file.Path != "/d/f" || file.Size != 7 || file.ModTimePrecision != remotefs.PrecisionSecond || file.ModTime.Location() != time.UTC {
		t.Errorf("file = %+v", file)
	}
	if dir := toEntry("/", &ftp.Entry{Name: "d", Type: ftp.EntryTypeFolder, Time: when}, false); !dir.IsDir() || dir.Path != "/d" || dir.ModTimePrecision != remotefs.PrecisionMinute {
		t.Errorf("directory = %+v", dir)
	}
	if link := toEntry("/", &ftp.Entry{Name: "l", Type: ftp.EntryTypeLink, Time: when}, false); link.Mode&0o777 != 0o777 || link.IsDir() {
		t.Errorf("link = %+v", link)
	}
	if day := toEntry("/", &ftp.Entry{Name: "o", Type: ftp.EntryTypeFile, Time: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)}, false); day.ModTimePrecision != remotefs.PrecisionDay {
		t.Errorf("date-only entry = %+v; want day precision", day)
	}
}

// TestConvert verifies that a listing leaves out the entries for the directory itself and its parent.
func TestConvert(t *testing.T) {
	raw := []*ftp.Entry{{Name: "."}, {Name: ".."}, {Name: ""}, {Name: "kept", Type: ftp.EntryTypeFile}}

	entries := convert("/", raw, true, codec{})

	if len(entries) != 1 || entries[0].Name != "kept" {
		t.Errorf("convert() = %+v; want only kept", entries)
	}
}

// TestVerifier verifies the certificate pinning callback.
//
// Scenarios:
//   - a server that presents no certificate is refused
//   - the check receives the fingerprint of the first certificate and its verdict is remembered
func TestVerifier(t *testing.T) {
	v := &verifier{check: func(string, string) error { return nil }}
	if err := v.verify(nil, nil); err == nil {
		t.Error("verify() accepted a server without a certificate")
	}

	refused := errors.New("no")
	var seen string
	v = &verifier{check: func(keyType, fingerprint string) error {
		seen = keyType + " " + fingerprint
		return refused
	}}
	if err := v.verify([][]byte{[]byte("der")}, nil); !errors.Is(err, refused) || !errors.Is(v.reason(), refused) {
		t.Errorf("verify() = %v, reason %v; want the check's error", err, v.reason())
	}
	if want := CertificateKeyType + " " + fingerprintOf([]byte("der")); seen != want {
		t.Errorf("check saw %q; want %q", seen, want)
	}
}

// TestFingerprintOf verifies the notation of certificate fingerprints, which the host key store also validates.
func TestFingerprintOf(t *testing.T) {
	if got := fingerprintOf([]byte("certificate")); !regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`).MatchString(got) {
		t.Errorf("fingerprintOf() = %q; want SHA256: and 43 base64 characters", got)
	}
}

// TestLink_OpenAfterShutdown verifies that a session that was shut down refuses new connections and closes the one it got.
func TestLink_OpenAfterShutdown(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()
	ctx := context.Background()
	l := &link{conns: map[net.Conn]struct{}{}, dial: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	l.ctx.Store(&ctx)
	l.shutdown()

	if _, err := l.open("tcp", "127.0.0.1:21"); !errors.Is(err, net.ErrClosed) {
		t.Errorf("open() after shutdown = %v; want net.ErrClosed", err)
	}
	if _, err := client.Write([]byte("x")); err == nil {
		t.Error("the connection dialed after shutdown was left open")
	}
}

// TestDialError verifies how failures while connecting are explained.
//
// Scenarios:
//   - a refused certificate reports the reason the check gave
//   - a destination the guard blocked keeps its own error
//   - a server that answers 534 to AUTH TLS is an invalid configuration, not a lost connection
//   - anything else is classified as any other failure
func TestDialError(t *testing.T) {
	rejected := &remotefs.HostKeyError{Type: CertificateKeyType, Fingerprint: "SHA256:x"}
	v := &verifier{rejected: rejected}
	if err := dialError("h:21", v, errors.New("tls: bad certificate")); !errors.Is(err, rejected) {
		t.Errorf("refused certificate = %v; want the check's error", err)
	}
	if err := dialError("h:21", &verifier{}, netguard.ErrBlocked); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("blocked destination = %v", err)
	}
	if err := dialError("h:21", nil, &textproto.Error{Code: 534, Msg: "no tls"}); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("AUTH TLS refused = %v; want ErrInvalid", err)
	}
	if err := dialError("h:21", nil, errors.New("boom")); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("other failure = %v; want ErrConnectionLost", err)
	}
}

// isErr reports whether err matches target, for the tests of this package.
func isErr(err, target error) bool { return errors.Is(err, target) }

// TestCodec verifies the conversion of names.
//
// Scenarios:
//   - without a character set names pass unchanged in both directions
//   - with one, UTF-8 names become the bytes of the character set and back, names it cannot hold are ErrInvalid and bytes it does not define become the replacement character
//   - what is written for a multi-byte character set holds no control character
func TestCodec(t *testing.T) {
	var none codec
	if got, err := none.server("/café"); err != nil || got != "/café" || none.client("café") != "café" {
		t.Errorf("identity codec = %q, %v", got, err)
	}
	latin := codec{encoding: charmap.ISO8859_1}
	if got, err := latin.server("/café"); err != nil || got != "/caf\xe9" || latin.client("caf\xe9") != "café" {
		t.Errorf("ISO-8859-1 codec = %q, %v", got, err)
	}
	if _, err := latin.server("/日本"); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("unrepresentable name = %v; want ErrInvalid", err)
	}
	if got := (codec{encoding: charmap.Windows1252}).client("\x81"); got == "" {
		t.Error("an undefined byte was dropped")
	}
	sjis := codec{encoding: japanese.ShiftJIS}
	converted, err := sjis.server("/日本語/ファイル")
	if err != nil || validPath(converted) != nil || sjis.client(converted) != "/日本語/ファイル" {
		t.Errorf("Shift_JIS round trip = %q, %v", converted, err)
	}
}

// FuzzRepresentable checks that no path can smuggle a second command into the control connection through a character set.
//
// Scenarios:
//   - for every supported character set and any text, a path that is accepted is written without carriage returns, line feeds or NUL bytes
func FuzzRepresentable(f *testing.F) {
	sets := []encoding.Encoding{
		nil, charmap.ISO8859_1, charmap.Windows1252, charmap.KOI8R, japanese.ShiftJIS, japanese.EUCJP, simplifiedchinese.GBK, traditionalchinese.Big5, korean.EUCKR,
	}
	for _, seed := range []string{"/a", "/café", "/日本語", "/x\r\nDELE y", "/\x00", "/ｱｲｳ", "/　ÿ"} {
		f.Add(seed, uint8(1))
	}
	f.Fuzz(func(t *testing.T, name string, which uint8) {
		fsys := &FS{codec: codec{encoding: sets[int(which)%len(sets)]}}
		if validPath(name) != nil || fsys.representable(name) != nil {
			return
		}
		if strings.ContainsAny(fsys.sp(name), "\r\n\x00") {
			t.Errorf("%q is accepted and written with a control character: %q", name, fsys.sp(name))
		}
	})
}
