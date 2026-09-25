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

	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
)

// Connector implements [connections.Connector] for the protocols the server supports.
type Connector struct {
	sites    *site.Service
	hostKeys *hostkey.Service
	policy   *netguard.Policy
	timeout  time.Duration
}

var _ connections.Connector = (*Connector)(nil)

// New returns a connector. timeout bounds establishing each TCP connection.
func New(sites *site.Service, hostKeys *hostkey.Service, policy *netguard.Policy, timeout time.Duration) *Connector {
	return &Connector{sites: sites, hostKeys: hostKeys, policy: policy, timeout: timeout}
}

// Connect implements [connections.Connector].
//
// A server key that is not trusted yet fails with a [remotefs.HostKeyError] that carries its fingerprint, for the
// user to confirm; a changed key fails the same way and is never accepted. Secrets are wiped after use.
func (c *Connector) Connect(ctx context.Context, key connections.Key, session string) (remotefs.RemoteFS, error) {
	target, err := c.sites.Get(ctx, key.UserID, key.WorkspaceID, key.SiteID)
	if err != nil {
		return nil, err
	}
	if target.Protocol != site.SFTP {
		return nil, fmt.Errorf("%w: %s sites cannot be connected yet", remotefs.ErrInvalid, target.Protocol)
	}

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

	secrets, err := c.open(ctx, key, session, target)
	if err != nil {
		return nil, err
	}
	defer secrets.wipe()
	cfg.Password, cfg.PrivateKey, cfg.Passphrase = string(secrets.password), secrets.privateKey, secrets.passphrase

	return sftpfs.Dial(ctx, cfg)
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
