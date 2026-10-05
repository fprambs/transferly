package connect

import (
	"context"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// tlsModes is how each FTP encryption of a site protects the connection.
var tlsModes = map[string]ftpfs.TLSMode{
	connectors.EncryptionPlain:    ftpfs.TLSNone,
	connectors.EncryptionOptional: ftpfs.TLSOptional,
	connectors.EncryptionExplicit: ftpfs.TLSExplicit,
	connectors.EncryptionImplicit: ftpfs.TLSImplicit,
}

// openFTP connects to an FTP server, protected as the site's encryption says (ADR-0012). An FTP account may have an empty
// password, so none is required. The certificate is checked again on every data connection, long after the request that opened
// the connection ended, so its check does not end with that request.
func openFTP(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	mode := tlsModes[t.Setting("encryption")]
	host, port := t.Setting(connectors.FieldHost), t.number(connectors.FieldPort)
	auth, username := t.Setting(connectors.FieldAuth), t.Setting(connectors.FieldUsername)
	leg := hop{c: c, key: key}
	leg.connecting(t, host, port, viaOf(t))
	fsys, err := ftpfs.Dial(ctx, ftpfs.Config{
		Address:       t.address(connectors.FieldHost, connectors.FieldPort),
		Username:      username,
		Password:      string(t.secret(site.SecretPassword)),
		Account:       string(t.secret(site.SecretAccount)),
		TLS:           mode,
		Location:      location(t.Setting("list_timezone")),
		Charset:       charsetOf(t.Setting("charset")),
		DialContext:   leg.dialer(c.baseDialer(t)),
		Timeout:       c.timeout,
		VerifyHostKey: leg.verify(context.WithoutCancel(ctx), host, port),
		Prompt:        leg.ask(ctx, auth),
		PromptWait:    c.promptWait(),
	})
	if err != nil {
		return nil, err
	}
	if !fsys.Secure() && mode == ftpfs.TLSOptional {
		leg.record(connlog.Warn, connlog.PlainText, map[string]string{})
	}
	leg.authenticated(auth, username)
	return fsys, nil
}

// charsetOf returns the character set named by a site for its file names; UTF-8, and any name that is not known, need no conversion.
func charsetOf(name string) encoding.Encoding {
	switch name {
	case "iso-8859-1":
		return charmap.ISO8859_1
	case "iso-8859-2":
		return charmap.ISO8859_2
	case "iso-8859-15":
		return charmap.ISO8859_15
	case "windows-1250":
		return charmap.Windows1250
	case "windows-1251":
		return charmap.Windows1251
	case "windows-1252":
		return charmap.Windows1252
	case "koi8-r":
		return charmap.KOI8R
	case "shift_jis":
		return japanese.ShiftJIS
	case "euc-jp":
		return japanese.EUCJP
	case "gbk":
		return simplifiedchinese.GBK
	case "big5":
		return traditionalchinese.Big5
	case "euc-kr":
		return korean.EUCKR
	}
	return nil
}

// location returns the zone named by a site for the times of its listings; a name that cannot be loaded means UTC.
func location(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
