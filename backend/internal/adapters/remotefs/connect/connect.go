// Package connect opens connections to the sites of a workspace: it reads the site, opens its credentials with
// the vault key of the user's session, checks the server key against the keys the workspace trusts, and refuses
// destinations the network guard forbids.
package connect

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
)

// QuickConnections finds the quick connections of a user.
type QuickConnections interface {
	// Lookup returns a quick connection of the user, with copies of its secrets that the caller wipes.
	Lookup(userID, workspaceID, id uuid.UUID) (quick.Connection, bool)
}

// Connector implements [connections.Connector] for the protocols the server supports. The identifier of a site may
// also be that of a quick connection of the user.
type Connector struct {
	sites    *site.Service
	quick    QuickConnections
	hostKeys *hostkey.Service
	policy   *netguard.Policy
	timeout  time.Duration
	implicit map[int]bool
}

// Option adjusts a [Connector].
type Option func(*Connector)

// ImplicitFTPSPorts sets the ports whose FTPS sites start with TLS instead of upgrading with AUTH TLS. The default is 990.
func ImplicitFTPSPorts(ports ...int) Option {
	return func(c *Connector) {
		c.implicit = make(map[int]bool, len(ports))
		for _, port := range ports {
			c.implicit[port] = true
		}
	}
}

var _ connections.Connector = (*Connector)(nil)

// New returns a connector. timeout bounds establishing each TCP connection.
func New(sites *site.Service, quick QuickConnections, hostKeys *hostkey.Service, policy *netguard.Policy, timeout time.Duration, options ...Option) *Connector {
	c := &Connector{sites: sites, quick: quick, hostKeys: hostKeys, policy: policy, timeout: timeout, implicit: map[int]bool{implicitFTPSPort: true}}
	for _, option := range options {
		option(c)
	}
	return c
}

// implicitFTPSPort is the port of implicit FTPS; FTPS on any other port starts in clear text and upgrades with AUTH TLS.
const implicitFTPSPort = 990

// Connect implements [connections.Connector].
//
// A server key that is not trusted yet fails with a [remotefs.HostKeyError] that carries its fingerprint, for the
// user to confirm; a changed key fails the same way and is never accepted. For FTPS the "key" is the certificate of the
// server, under the key type [ftpfs.CertificateKeyType]. Secrets are wiped after use.
func (c *Connector) Connect(ctx context.Context, key connections.Key, session string) (remotefs.RemoteFS, error) {
	target, secrets, err := c.resolve(ctx, key, session)
	if err != nil {
		return nil, err
	}
	defer secrets.wipe()
	return c.dial(ctx, key, target, secrets)
}

// dial connects with the protocol of the site.
func (c *Connector) dial(ctx context.Context, key connections.Key, target site.Site, secrets secrets) (remotefs.RemoteFS, error) {
	switch target.Protocol {
	case site.SFTP:
		return c.dialSFTP(ctx, key, target, secrets)
	case site.FTP, site.FTPS:
		return c.dialFTP(ctx, key, target, secrets)
	}
	return nil, fmt.Errorf("%w: %s sites cannot be connected", remotefs.ErrInvalid, target.Protocol)
}

// dialFTP connects to an FTP or FTPS server. FTP sends everything in clear text; FTPS on an implicit port (990 by default)
// starts with TLS, on any other port it upgrades with AUTH TLS. An FTP account may have an empty password.
func (c *Connector) dialFTP(ctx context.Context, key connections.Key, target site.Site, s secrets) (remotefs.RemoteFS, error) {
	mode := ftpfs.TLSNone
	if target.Protocol == site.FTPS {
		mode = ftpfs.TLSExplicit
		if c.implicit[target.Port] {
			mode = ftpfs.TLSImplicit
		}
	}
	return ftpfs.Dial(ctx, ftpfs.Config{
		Address:     net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
		Username:    target.Username,
		Password:    string(s.password),
		TLS:         mode,
		Location:    location(target.ListTimezone),
		Charset:     charsetOf(target.Charset),
		DialContext: c.policy.Dialer(c.timeout).DialContext,
		Timeout:     c.timeout,
		VerifyHostKey: func(keyType, fingerprint string) error {
			return c.verify(ctx, key, target, keyType, fingerprint)
		},
	})
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

// dialSFTP connects to an SFTP server with the password or the key of the site.
func (c *Connector) dialSFTP(ctx context.Context, key connections.Key, target site.Site, secrets secrets) (remotefs.RemoteFS, error) {
	cfg := sftpfs.Config{
		Address:     net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
		Username:    target.Username,
		DialContext: c.policy.Dialer(c.timeout).DialContext,
		Timeout:     c.timeout,
		VerifyHostKey: func(keyType, fingerprint string) error {
			return c.verify(ctx, key, target, keyType, fingerprint)
		},
	}
	types, err := c.hostKeys.Algorithms(ctx, key.WorkspaceID, target.Host, target.Port)
	if err != nil {
		return nil, err
	}
	cfg.HostKeyAlgorithms = algorithmsFor(types)

	cfg.Password, cfg.PrivateKey, cfg.Passphrase = string(secrets.password), secrets.privateKey, secrets.passphrase

	return sftpfs.Dial(ctx, cfg)
}

// resolve finds the settings and the credentials of what the key names: a quick connection of the user, or a saved
// site whose secrets are opened with the vault key of the session.
func (c *Connector) resolve(ctx context.Context, key connections.Key, session string) (site.Site, secrets, error) {
	if q, ok := c.quick.Lookup(key.UserID, key.WorkspaceID, key.SiteID); ok {
		s := secrets{password: q.Secrets.Password, privateKey: q.Secrets.PrivateKey, passphrase: q.Secrets.Passphrase}
		if err := s.require(q.Site); err != nil {
			s.wipe()
			return site.Site{}, secrets{}, err
		}
		return q.Site, s, nil
	}
	target, err := c.sites.Get(ctx, key.UserID, key.WorkspaceID, key.SiteID)
	if err != nil {
		return site.Site{}, secrets{}, err
	}
	s, err := c.open(ctx, key, session, target)
	return target, s, err
}

// require checks that the credentials the site needs are there: a private key, or a password unless the account is an FTP one.
func (s secrets) require(target site.Site) error {
	switch {
	case target.AuthMethod == site.AuthKey && len(s.privateKey) == 0:
		return fmt.Errorf("%w: no private key was given", remotefs.ErrAuth)
	case target.AuthMethod != site.AuthKey && target.Protocol == site.SFTP && len(s.password) == 0:
		return fmt.Errorf("%w: no password was given", remotefs.ErrAuth)
	}
	return nil
}

// verify compares the key presented by the server with the trusted ones.
func (c *Connector) verify(ctx context.Context, key connections.Key, target site.Site, keyType, fingerprint string) error {
	verdict, err := c.hostKeys.Check(ctx, key.WorkspaceID, target.Host, target.Port, keyType, fingerprint)
	switch {
	case err != nil:
		return err
	case verdict == hostkey.Trusted:
		return nil
	}
	return &remotefs.HostKeyError{Type: keyType, Fingerprint: fingerprint, Changed: verdict == hostkey.Changed}
}

// secrets are the decrypted credentials of a site.
type secrets struct {
	password, privateKey, passphrase []byte
}

// wipe overwrites the secrets in memory.
func (s secrets) wipe() {
	for _, b := range [][]byte{s.password, s.privateKey, s.passphrase} {
		clear(b)
	}
}

// open decrypts the credentials the site's authentication method needs. A missing one is an authentication error.
func (c *Connector) open(ctx context.Context, key connections.Key, session string, target site.Site) (secrets, error) {
	var s secrets
	read := func(kind site.SecretKind) ([]byte, error) {
		return c.sites.OpenSecret(ctx, key.UserID, key.WorkspaceID, key.SiteID, session, kind)
	}
	var err error
	if target.AuthMethod == site.AuthKey {
		if s.privateKey, err = read(site.SecretPrivateKey); err != nil {
			return secrets{}, missing(err, "private key")
		}
		if s.passphrase, err = read(site.SecretPassphrase); err != nil && !errors.Is(err, site.ErrNoSecret) {
			s.wipe()
			return secrets{}, err
		}
		return s, nil
	}
	s.password, err = read(site.SecretPassword)
	if errors.Is(err, site.ErrNoSecret) && target.Protocol != site.SFTP {
		err = nil
	}
	if err != nil {
		return secrets{}, missing(err, "password")
	}
	return s, nil
}

// missing turns "no such secret" into an authentication error and keeps other errors.
func missing(err error, what string) error {
	if errors.Is(err, site.ErrNoSecret) {
		return fmt.Errorf("%w: the site has no %s stored", remotefs.ErrAuth, what)
	}
	return err
}

// algorithmsFor lists the SSH signature algorithms that a server may use to prove one of the trusted key types.
// RSA keys are proven with SHA-2 signatures; the SHA-1 based ssh-rsa signature is never accepted.
func algorithmsFor(keyTypes []string) []string {
	var out []string
	for _, t := range keyTypes {
		if t == "ssh-rsa" {
			out = append(out, "rsa-sha2-512", "rsa-sha2-256")
			continue
		}
		out = append(out, t)
	}
	return out
}
