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
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/sentinel/internal/adapters/proxydial"
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

// baseDialer returns the DialContext that reaches a site: through its configured proxy, when it has one, otherwise
// straight from the network guard's own dialer. Either way the proxy's own address, or the site's when there is
// none, is checked by the guard before anything connects to it.
func (c *Connector) baseDialer(target site.Site, s secrets) func(ctx context.Context, network, address string) (net.Conn, error) {
	guarded := c.policy.Dialer(c.timeout).DialContext
	if !target.Proxy.Set() {
		return guarded
	}
	return proxydial.New(proxydial.Config{
		Kind: proxydial.Kind(target.Proxy.Kind), Address: net.JoinHostPort(target.Proxy.Host, strconv.Itoa(target.Proxy.Port)),
		Username: target.Proxy.Username, Password: string(s.proxyPassword),
	}, guarded).DialContext
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
		DialContext: c.baseDialer(target, s),
		Timeout:     c.timeout,
		VerifyHostKey: func(keyType, fingerprint string) error {
			return c.verifyHost(ctx, key.WorkspaceID, target.Host, target.Port, keyType, fingerprint)
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

// dialSFTP connects to an SFTP server with the password or the key of the site, through its jump host first when it
// has one.
func (c *Connector) dialSFTP(ctx context.Context, key connections.Key, target site.Site, s secrets) (remotefs.RemoteFS, error) {
	address := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	types, err := c.hostKeys.Algorithms(ctx, key.WorkspaceID, target.Host, target.Port)
	if err != nil {
		return nil, err
	}
	cfg := sftpfs.Config{
		Address: address, Username: target.Username, Timeout: c.timeout, HostKeyAlgorithms: algorithmsFor(types),
		Password: string(s.password), PrivateKey: s.privateKey, Passphrase: s.passphrase,
		VerifyHostKey: func(keyType, fingerprint string) error {
			return c.verifyHost(ctx, key.WorkspaceID, target.Host, target.Port, keyType, fingerprint)
		},
	}
	if !target.Jump.Set() {
		cfg.DialContext = c.baseDialer(target, s)
		return sftpfs.Dial(ctx, cfg)
	}
	jump, err := c.dialJump(ctx, key, target, s)
	if err != nil {
		return nil, err
	}
	tunnel, err := jump.DialContext(ctx, "tcp", address)
	if err != nil {
		_ = jump.Close()
		return nil, fmt.Errorf("opening a channel to %s through the jump host: %w: %w", address, remotefs.ErrConnectionLost, err)
	}
	cfg.Conn = tunnel
	fsys, err := sftpfs.Dial(ctx, cfg)
	if err != nil {
		_ = jump.Close()
		return nil, err
	}
	return &jumpedFS{FS: fsys, jump: jump}, nil
}

// dialJump connects to and authenticates against a site's jump host, through its proxy first when it also has one.
func (c *Connector) dialJump(ctx context.Context, key connections.Key, target site.Site, s secrets) (*ssh.Client, error) {
	address := net.JoinHostPort(target.Jump.Host, strconv.Itoa(target.Jump.Port))
	types, err := c.hostKeys.Algorithms(ctx, key.WorkspaceID, target.Jump.Host, target.Jump.Port)
	if err != nil {
		return nil, err
	}
	return sftpfs.DialSSH(ctx, sftpfs.Config{
		Address: address, Username: target.Jump.Username, Timeout: c.timeout, HostKeyAlgorithms: algorithmsFor(types),
		DialContext: c.baseDialer(target, s),
		Password:    string(s.jumpPassword), PrivateKey: s.jumpPrivateKey, Passphrase: s.jumpPassphrase,
		VerifyHostKey: func(keyType, fingerprint string) error {
			return c.verifyHost(ctx, key.WorkspaceID, target.Jump.Host, target.Jump.Port, keyType, fingerprint)
		},
	})
}

// jumpedFS is the file system of a site reached through a jump host: closing it also closes the SSH client that
// carries the tunnel, which otherwise nothing would ever close.
type jumpedFS struct {
	*sftpfs.FS
	jump *ssh.Client
}

// Close closes the site's own connection and then the jump host's.
func (f *jumpedFS) Close() error {
	err := f.FS.Close()
	if jumpErr := f.jump.Close(); err == nil {
		err = jumpErr
	}
	return err
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

// require checks that the credentials the site needs are there: a private key, or a password unless the account is
// an FTP one; and, when it has a jump host, the credentials that needs too.
func (s secrets) require(target site.Site) error {
	switch {
	case target.AuthMethod == site.AuthKey && len(s.privateKey) == 0:
		return fmt.Errorf("%w: no private key was given", remotefs.ErrAuth)
	case target.AuthMethod != site.AuthKey && target.Protocol == site.SFTP && len(s.password) == 0:
		return fmt.Errorf("%w: no password was given", remotefs.ErrAuth)
	}
	switch {
	case !target.Jump.Set():
	case target.Jump.AuthMethod == site.AuthKey && len(s.jumpPrivateKey) == 0:
		return fmt.Errorf("%w: no jump host private key was given", remotefs.ErrAuth)
	case target.Jump.AuthMethod != site.AuthKey && len(s.jumpPassword) == 0:
		return fmt.Errorf("%w: no jump host password was given", remotefs.ErrAuth)
	}
	return nil
}

// verifyHost compares the key a server presents with the ones the workspace trusts for host:port, whether that is a
// site, its proxy or its jump host.
func (c *Connector) verifyHost(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) error {
	verdict, err := c.hostKeys.Check(ctx, workspaceID, host, port, keyType, fingerprint)
	switch {
	case err != nil:
		return err
	case verdict == hostkey.Trusted:
		return nil
	}
	return &remotefs.HostKeyError{Type: keyType, Fingerprint: fingerprint, Changed: verdict == hostkey.Changed}
}

// secrets are the decrypted credentials of a site, its jump host and its proxy.
type secrets struct {
	password, privateKey, passphrase             []byte
	jumpPassword, jumpPrivateKey, jumpPassphrase []byte
	proxyPassword                                []byte
}

// wipe overwrites the secrets in memory.
func (s secrets) wipe() {
	for _, b := range [][]byte{s.password, s.privateKey, s.passphrase, s.jumpPassword, s.jumpPrivateKey, s.jumpPassphrase, s.proxyPassword} {
		clear(b)
	}
}

// open decrypts the credentials the site's authentication method needs, and its jump host's and proxy's when it has
// them. A missing one the site needs is an authentication error; a proxy needs one only when it has a username.
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
	} else {
		s.password, err = read(site.SecretPassword)
		if errors.Is(err, site.ErrNoSecret) && target.Protocol != site.SFTP {
			err = nil
		}
		if err != nil {
			return secrets{}, missing(err, "password")
		}
	}
	if err := openJump(read, target, &s); err != nil {
		s.wipe()
		return secrets{}, err
	}
	if target.Proxy.Set() && target.Proxy.Username != "" {
		if s.proxyPassword, err = read(site.SecretProxyPassword); err != nil && !errors.Is(err, site.ErrNoSecret) {
			s.wipe()
			return secrets{}, err
		}
	}
	return s, nil
}

// openJump decrypts the credentials a site's jump host needs, when it has one.
func openJump(read func(site.SecretKind) ([]byte, error), target site.Site, s *secrets) error {
	if !target.Jump.Set() {
		return nil
	}
	var err error
	if target.Jump.AuthMethod == site.AuthKey {
		if s.jumpPrivateKey, err = read(site.SecretJumpPrivateKey); err != nil {
			return missing(err, "jump host private key")
		}
		if s.jumpPassphrase, err = read(site.SecretJumpPassphrase); err != nil && !errors.Is(err, site.ErrNoSecret) {
			return err
		}
		return nil
	}
	if s.jumpPassword, err = read(site.SecretJumpPassword); err != nil {
		return missing(err, "jump host password")
	}
	return nil
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
