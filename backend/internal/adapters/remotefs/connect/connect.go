// Package connect opens connections to the sites of a workspace: it reads the site, opens its credentials with
// the vault key of the user's session, checks the server key against the keys the workspace trusts, and refuses
// destinations the network guard forbids. Each connector has an opener that dials its sites (ADR-0022).
package connect

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/adapters/googleauth"
	"github.com/fprambs/transferly/internal/adapters/proxydial"
	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/credentials"
	"github.com/fprambs/transferly/internal/core/hostkey"
	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/prompts"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// QuickConnections finds the quick connections of a user.
type QuickConnections interface {
	// Lookup returns a quick connection of the user, with copies of its secrets that the caller wipes.
	Lookup(userID, workspaceID, id uuid.UUID) (quick.Connection, bool)
}

// Connector implements [connections.Connector] for the connectors the server is built with. The identifier of a site may
// also be that of a quick connection of the user.
type Connector struct {
	sites    *site.Service
	quick    QuickConnections
	hostKeys *hostkey.Service
	policy   *netguard.Policy
	timeout  time.Duration
	asked    *credentials.Store
	log      *connlog.Recorder
	prompts  *prompts.Broker
	strict   *sftpfs.StrictServers
	offering site.Offering
	roots    *x509.CertPool
	google   *googleauth.Service
	commands func() bool
}

// Option adjusts a [Connector].
type Option func(*Connector)

// AskedPasswords sets where the passwords typed for sites that ask for one when connecting are held (ADR-0010). Without
// it, those sites always fail with [credentials.ErrRequired].
func AskedPasswords(store *credentials.Store) Option {
	return func(c *Connector) { c.asked = store }
}

// Prompts sets who relays what servers ask to the person connecting to an interactive site (ADR-0016). Without it, those
// sites always fail as refused credentials.
func Prompts(broker *prompts.Broker) Option {
	return func(c *Connector) { c.prompts = broker }
}

// Offering sets who tells whether the instance offers the connector of a site (ADR-0019): sites of a connector it does not offer fail with
// [connectors.ErrDisabled] before anything is dialed. Without it every connector connects.
func Offering(o site.Offering) Option {
	return func(c *Connector) { c.offering = o }
}

// CertificateAuthorities sets the authorities that HTTPS connections, such as those of object stores, are verified against
// instead of the system's, for services with certificates of a private authority. Without it the system's are used.
func CertificateAuthorities(roots *x509.CertPool) Option {
	return func(c *Connector) { c.roots = roots }
}

// Google sets the sign-in with Google whose OAuth client authorizes Google Drive sites with their refresh tokens (ADR-0024). Without
// it, or without a client configured, those sites cannot connect.
func Google(service *googleauth.Service) Option {
	return func(c *Connector) { c.google = service }
}

// ServerCommands sets who tells whether sites may run commands on their servers (ADR-0027); when it says no, sites in sudo mode
// fail with [remotefs.ErrUnsupported]. Without it they may.
func ServerCommands(allowed func() bool) Option {
	return func(c *Connector) { c.commands = allowed }
}

// errNoPrompts fails an interactive site when nobody can relay the server's questions.
var errNoPrompts = fmt.Errorf("%w: nobody can answer what the server asks", remotefs.ErrAuth)

// promptWait is how long a question to the person connecting may wait.
func (c *Connector) promptWait() time.Duration {
	if c.prompts == nil {
		return 0
	}
	return c.prompts.Wait()
}

// anonymousPassword is what anonymous sites answer when a server asks for a password, the address FileZilla sends too.
const anonymousPassword = "anonymous@example.com"

var (
	_ connections.Connector = (*Connector)(nil)
	_ connections.Admitter  = (*Connector)(nil)
)

// New returns a connector. timeout bounds establishing each TCP connection.
func New(sites *site.Service, quick QuickConnections, hostKeys *hostkey.Service, policy *netguard.Policy, timeout time.Duration, options ...Option) *Connector {
	c := &Connector{sites: sites, quick: quick, hostKeys: hostKeys, policy: policy, timeout: timeout, strict: &sftpfs.StrictServers{}}
	for _, option := range options {
		option(c)
	}
	return c
}

// target is what a connection opens: a site, or a quick connection, with its settled configuration and its decrypted secrets.
type target struct {
	site.Site
	secrets map[site.SecretKind][]byte
}

// secret returns a decrypted secret of the target, or nil when it has none.
func (t target) secret(kind site.SecretKind) []byte { return t.secrets[kind] }

// number returns a numeric setting of the target, which its schema already checked; a setting that is missing reads as zero.
func (t target) number(name string) int {
	n, _ := strconv.Atoi(t.Setting(name))
	return n
}

// address returns host:port from a host setting and its port setting.
func (t target) address(host, port string) string {
	return net.JoinHostPort(t.Setting(host), t.Setting(port))
}

// wipe overwrites the secrets in memory.
func (t target) wipe() {
	for _, value := range t.secrets {
		clear(value)
	}
}

// opener dials the sites of one connector.
type opener func(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error)

// openers are the openers of the connectors Transferly is built with, by type.
var openers = map[string]opener{"sftp": openSFTP, "ftp": openFTP, "s3": openS3, "gcs": openS3, "gdrive": openGDrive, "azblob": openAzureBlob, "azfiles": openAzureFiles, "webdav": openWebDAV}

// Connect implements [connections.Connector].
//
// A server key that is not trusted yet fails with a [remotefs.HostKeyError] that carries its fingerprint, for the
// user to confirm; a changed key fails the same way and is never accepted. For FTPS the "key" is the certificate of the
// server, under the key type [ftpfs.CertificateKeyType]. Secrets are wiped after use.
func (c *Connector) Connect(ctx context.Context, key connections.Key, session string) (remotefs.RemoteFS, error) {
	t, err := c.resolve(ctx, key, session)
	if err != nil {
		return nil, err
	}
	defer t.wipe()
	if c.offering != nil {
		if err := c.offering.CheckConnector(ctx, t.Connector); err != nil {
			return nil, err
		}
	}
	open, ok := openers[t.Connector]
	if !ok {
		return nil, fmt.Errorf("%w: %s sites cannot be connected", remotefs.ErrInvalid, t.Connector)
	}
	switch t.Setting(connectors.FieldAuth) {
	case connectors.AuthAnonymous:
		t.secrets[site.SecretPassword] = []byte(anonymousPassword)
	case connectors.AuthAsk:
		password, err := c.askedPassword(key, session, t.Site)
		if err != nil {
			return nil, err
		}
		t.secrets[site.SecretPassword] = password
	}
	fsys, err := open(ctx, c, key, t)
	if t.Setting(connectors.FieldAuth) == connectors.AuthAsk && errors.Is(err, remotefs.ErrAuth) {
		c.asked.Forget(askedKey(key, session))
	}
	return fsys, err
}

// Admit implements [connections.Admitter]: a session may use an open connection to a site that asks for its password
// only when it gave the password itself, so another session of the same user cannot ride on it.
func (c *Connector) Admit(_ context.Context, key connections.Key, session string) error {
	if c.asked != nil && !c.asked.Admits(askedKey(key, session)) {
		return credentials.ErrRequired
	}
	return nil
}

// MaxConnections implements [connections.Limiter]: the connections setting of the site, for servers that allow few
// sessions or that sit behind a balancer whose servers each have their own key; 0, the default of the pool, when the site
// has none or cannot be read.
func (c *Connector) MaxConnections(ctx context.Context, key connections.Key) int {
	if q, ok := c.quick.Lookup(key.UserID, key.WorkspaceID, key.SiteID); ok {
		return settingCount(q.Site)
	}
	found, err := c.sites.Get(ctx, key.UserID, key.WorkspaceID, key.SiteID)
	if err != nil {
		return 0
	}
	return settingCount(found)
}

// settingCount reads the connections setting of a site, or 0 when it has none.
func settingCount(s site.Site) int {
	n, err := strconv.Atoi(s.Setting(connectors.FieldConnections))
	if err != nil {
		return 0
	}
	return n
}

// askedKey is where the password a session typed for the site of key is held.
func askedKey(key connections.Key, session string) credentials.Key {
	return credentials.Key{Session: session, UserID: key.UserID, WorkspaceID: key.WorkspaceID, SiteID: key.SiteID}
}

// baseDialer returns the DialContext that reaches a target: through its configured proxy, when it has one, otherwise
// straight from the network guard's own dialer. Either way the proxy's own address, or the target's when there is
// none, is checked by the guard before anything connects to it.
func (c *Connector) baseDialer(t target) func(ctx context.Context, network, address string) (net.Conn, error) {
	guarded := c.policy.Dialer(c.timeout).DialContext
	if t.Setting("proxy_kind") == "" {
		return guarded
	}
	return proxydial.New(proxydial.Config{
		Kind: proxydial.Kind(t.Setting("proxy_kind")), Address: t.address("proxy_host", "proxy_port"),
		Username: t.Setting("proxy_username"), Password: string(t.secret(site.SecretProxyPassword)),
	}, guarded).DialContext
}

// resolve finds the settings and the credentials of what the key names: a quick connection of the user, or a saved
// site whose stored secrets are opened with the vault key of the session.
func (c *Connector) resolve(ctx context.Context, key connections.Key, session string) (target, error) {
	if q, ok := c.quick.Lookup(key.UserID, key.WorkspaceID, key.SiteID); ok {
		return target{Site: q.Site, secrets: q.Secrets}, nil
	}
	found, err := c.sites.Get(ctx, key.UserID, key.WorkspaceID, key.SiteID)
	if err != nil {
		return target{}, err
	}
	t := target{Site: found, secrets: map[site.SecretKind][]byte{}}
	for _, kind := range found.Secrets {
		value, err := c.sites.OpenSecret(ctx, key.UserID, key.WorkspaceID, key.SiteID, session, kind)
		if err != nil {
			t.wipe()
			return target{}, err
		}
		t.secrets[kind] = value
	}
	return t, nil
}

// verifyHost compares the key a server presents with the ones the workspace trusts for host:port, whether that is a
// site, its proxy or its jump host.
func (c *Connector) verifyHost(ctx context.Context, workspaceID uuid.UUID, host string, port int, keyType, fingerprint string) error {
	verdict, trusted, err := c.hostKeys.Check(ctx, workspaceID, host, port, keyType, fingerprint)
	switch {
	case err != nil:
		return err
	case verdict == hostkey.Trusted:
		return nil
	}
	return &remotefs.HostKeyError{Type: keyType, Fingerprint: fingerprint, Changed: verdict == hostkey.Changed, Trusted: trusted}
}

// askedPassword returns the password the session typed for a site that asks for one when connecting, and records that
// the site asks, so another session cannot use its open connections; without one it fails with [credentials.ErrRequired].
func (c *Connector) askedPassword(key connections.Key, session string, s site.Site) ([]byte, error) {
	if c.asked == nil {
		return nil, credentials.ErrRequired
	}
	c.asked.MarkAsk(s.ID)
	password, ok := c.asked.Password(askedKey(key, session))
	if !ok {
		return nil, credentials.ErrRequired
	}
	return password, nil
}

// require refuses a target that lacks a secret it needs, as an authentication error that names what is missing.
func (t target) require(kind site.SecretKind, what string) error {
	if len(t.secret(kind)) == 0 {
		return fmt.Errorf("%w: the site has no %s", remotefs.ErrAuth, what)
	}
	return nil
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
