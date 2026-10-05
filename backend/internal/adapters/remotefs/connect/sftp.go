package connect

import (
	"context"
	"fmt"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs"
	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/connlog"
	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/site"
)

// requireSFTP checks that an SFTP target has the credentials its logon types need: a password for a password logon, a private key for a
// key logon, and the jump host's when it has one. Anonymous, interactive and asked passwords come from elsewhere.
func (t target) requireSFTP() error {
	var err error
	switch t.Setting(connectors.FieldAuth) {
	case connectors.AuthPassword:
		err = t.require(site.SecretPassword, "password stored")
	case connectors.AuthKey:
		err = t.require(site.SecretPrivateKey, "private key stored")
	}
	if err != nil || t.Setting("jump_host") == "" {
		return err
	}
	if t.Setting("jump_auth") == connectors.AuthKey {
		return t.require(site.SecretJumpPrivateKey, "jump host private key stored")
	}
	return t.require(site.SecretJumpPassword, "jump host password stored")
}

// openSFTP connects to an SFTP server with the password or the key of the site, through its jump host first when it has one.
func openSFTP(ctx context.Context, c *Connector, key connections.Key, t target) (remotefs.RemoteFS, error) {
	if err := t.requireSFTP(); err != nil {
		return nil, err
	}
	host, port := t.Setting(connectors.FieldHost), t.number(connectors.FieldPort)
	address := t.address(connectors.FieldHost, connectors.FieldPort)
	types, err := c.hostKeys.Algorithms(ctx, key.WorkspaceID, host, port)
	if err != nil {
		return nil, err
	}
	auth, username := t.Setting(connectors.FieldAuth), t.Setting(connectors.FieldUsername)
	leg := hop{c: c, key: key}
	cfg := sftpfs.Config{
		Address: address, Username: username, Timeout: c.timeout, HostKeyAlgorithms: algorithmsFor(types),
		Password: string(t.secret(site.SecretPassword)), PrivateKey: t.secret(site.SecretPrivateKey), Passphrase: t.secret(site.SecretPassphrase),
		VerifyHostKey: leg.verify(ctx, host, port),
		Prompt:        leg.ask(ctx, auth),
		Strict:        c.strict,
	}
	sudo, err := c.sudoOf(t)
	if err != nil {
		return nil, err
	}
	cfg.Sudo = sudo
	if t.Setting("jump_host") == "" {
		leg.connecting(t, host, port, viaOf(t))
		cfg.DialContext = leg.dialer(c.baseDialer(t))
		fsys, err := sftpfs.Dial(ctx, cfg)
		if err != nil {
			return nil, err
		}
		leg.authenticated(auth, username)
		leg.elevated(sudo)
		return fsys, nil
	}
	jump, err := dialJump(ctx, c, key, t)
	if err != nil {
		return nil, err
	}
	leg.connecting(t, host, port, "jump")
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
	leg.authenticated(auth, username)
	leg.elevated(sudo)
	return &jumpedFS{FS: fsys, jump: jump}, nil
}

// sudoOf returns the sudo mode of a site (ADR-0027), nil when it is off: the user sudo runs the SFTP server as, and the password it
// answers with, the site's own or, without one, the password the account signs in with. Sites cannot use it when the instance
// does not allow commands on servers.
func (c *Connector) sudoOf(t target) (*sftpfs.Sudo, error) {
	if t.Setting("sudo") != "true" {
		return nil, nil
	}
	if c.commands != nil && !c.commands() {
		return nil, fmt.Errorf("%w: an administrator turned commands on servers off, which sudo needs", remotefs.ErrUnsupported)
	}
	password := t.secret(site.SecretSudoPassword)
	if len(password) == 0 {
		password = t.secret(site.SecretPassword)
	}
	return &sftpfs.Sudo{User: t.Setting("sudo_user"), Password: string(password)}, nil
}

// elevated records that a session acts as another user through sudo, when it does.
func (h hop) elevated(sudo *sftpfs.Sudo) {
	if sudo != nil {
		h.record(connlog.Info, connlog.Elevated, map[string]string{"user": sudo.User})
	}
}

// dialJump connects to and authenticates against a site's jump host, through its proxy first when it also has one.
func dialJump(ctx context.Context, c *Connector, key connections.Key, t target) (*ssh.Client, error) {
	host, port := t.Setting("jump_host"), t.number("jump_port")
	types, err := c.hostKeys.Algorithms(ctx, key.WorkspaceID, host, port)
	if err != nil {
		return nil, err
	}
	leg := hop{c: c, key: key, jump: true}
	leg.connecting(t, host, port, viaOf(t))
	client, err := sftpfs.DialSSH(ctx, sftpfs.Config{
		Address: t.address("jump_host", "jump_port"), Username: t.Setting("jump_username"), Timeout: c.timeout, HostKeyAlgorithms: algorithmsFor(types),
		DialContext: leg.dialer(c.baseDialer(t)),
		Password:    string(t.secret(site.SecretJumpPassword)), PrivateKey: t.secret(site.SecretJumpPrivateKey), Passphrase: t.secret(site.SecretJumpPassphrase),
		VerifyHostKey: leg.verify(ctx, host, port),
	})
	if err != nil {
		return nil, err
	}
	leg.authenticated(t.Setting("jump_auth"), t.Setting("jump_username"))
	return client, nil
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
