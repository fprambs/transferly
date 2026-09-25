// Package connect opens connections to the sites of a workspace: it reads the site, opens its credentials with
// the vault key of the user's session, checks the server key against the keys the workspace trusts, and refuses
// destinations the network guard forbids.
package connect

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"

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
}

var _ connections.Connector = (*Connector)(nil)

// New returns a connector. timeout bounds establishing each TCP connection.
func New(sites *site.Service, quick QuickConnections, hostKeys *hostkey.Service, policy *netguard.Policy, timeout time.Duration) *Connector {
	return &Connector{sites: sites, quick: quick, hostKeys: hostKeys, policy: policy, timeout: timeout}
}

// Connect implements [connections.Connector].
//
// A server key that is not trusted yet fails with a [remotefs.HostKeyError] that carries its fingerprint, for the
// user to confirm; a changed key fails the same way and is never accepted. Secrets are wiped after use.
func (c *Connector) Connect(ctx context.Context, key connections.Key, session string) (remotefs.RemoteFS, error) {
	target, secrets, err := c.resolve(ctx, key, session)
	if err != nil {
		return nil, err
	}
	defer secrets.wipe()

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
		if err := supported(q.Site); err != nil {
			wipeAll(q.Secrets)
			return site.Site{}, secrets{}, err
		}
		s := secrets{password: q.Secrets.Password, privateKey: q.Secrets.PrivateKey, passphrase: q.Secrets.Passphrase}
		if err := s.require(q.Site.AuthMethod); err != nil {
			s.wipe()
			return site.Site{}, secrets{}, err
		}
		return q.Site, s, nil
	}
	target, err := c.sites.Get(ctx, key.UserID, key.WorkspaceID, key.SiteID)
	if err != nil {
		return site.Site{}, secrets{}, err
	}
	if err := supported(target); err != nil {
		return site.Site{}, secrets{}, err
	}
	s, err := c.open(ctx, key, session, target)
	return target, s, err
}

// supported refuses the protocols that cannot be connected yet.
func supported(target site.Site) error {
	if target.Protocol != site.SFTP {
		return fmt.Errorf("%w: %s sites cannot be connected yet", remotefs.ErrInvalid, target.Protocol)
	}
	return nil
}

// wipeAll overwrites the credentials of a quick connection copy.
func wipeAll(q quick.Secrets) {
	secrets{password: q.Password, privateKey: q.PrivateKey, passphrase: q.Passphrase}.wipe()
}

// require checks that the credentials the authentication method needs are there.
func (s secrets) require(method site.AuthMethod) error {
	switch {
	case method == site.AuthKey && len(s.privateKey) == 0:
		return fmt.Errorf("%w: no private key was given", remotefs.ErrAuth)
	case method != site.AuthKey && len(s.password) == 0:
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
	if s.password, err = read(site.SecretPassword); err != nil {
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
