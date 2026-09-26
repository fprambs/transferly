package connect_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/fprambs/sentinel/internal/adapters/remotefs/connect"
	"github.com/fprambs/sentinel/internal/adapters/remotefs/sftpfs/sftptest"
	"github.com/fprambs/sentinel/internal/adapters/store/postgres"
	"github.com/fprambs/sentinel/internal/core/auth"
	"github.com/fprambs/sentinel/internal/core/connections"
	"github.com/fprambs/sentinel/internal/core/hostkey"
	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/quick"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/core/workspace"
	"github.com/fprambs/sentinel/internal/testutil"
)

const (
	sshUser     = "deploy"
	sshPassword = "a long password"
	session     = "session-1"
)

// flaky is the store with failures that tests switch on.
type flaky struct {
	*postgres.Store
	failTrustedAt  int
	trusted        int
	failPassphrase bool
}

// errFlaky is the failure injected by [flaky].
var errFlaky = errors.New("store failure")

// Trusted fails from the configured call on, counting from 1; zero never fails.
func (f *flaky) Trusted(ctx context.Context, ws uuid.UUID, host string, port int) ([]hostkey.Key, error) {
	f.trusted++
	if f.failTrustedAt > 0 && f.trusted >= f.failTrustedAt {
		return nil, errFlaky
	}
	return f.Store.Trusted(ctx, ws, host, port)
}

// Secret fails for passphrases when configured to.
func (f *flaky) Secret(ctx context.Context, ws, id uuid.UUID, kind site.SecretKind) (site.Sealed, error) {
	if f.failPassphrase && kind == site.SecretPassphrase {
		return site.Sealed{}, errFlaky
	}
	return f.Store.Secret(ctx, ws, id, kind)
}

// env is a database with a user, their personal workspace, the services around sites and an unlocked vault.
type env struct {
	t         *testing.T
	store     *postgres.Store
	flaky     *flaky
	user      uuid.UUID
	workspace uuid.UUID
	sites     *site.Service
	hostKeys  *hostkey.Service
	vaults    *vault.Service
	allow     *netguard.Policy
	quick     *quick.Service
}

// newEnv builds the environment. The vault is set up and unlocked in session-1.
func newEnv(t *testing.T) *env {
	t.Helper()
	dbURL := testutil.NewDatabase(t)
	if err := postgres.Migrate(t.Context(), dbURL); err != nil {
		t.Fatal(err)
	}
	store, err := postgres.Open(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	clock := testutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	user := auth.User{ID: uuid.New(), Email: "u@example.com", PasswordHash: "h", Role: auth.RoleUser, Locale: "en", Timezone: "UTC", CreatedAt: clock.Now()}
	if err := store.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	workspaces := workspace.NewService(store, clock, nil)
	personal, err := workspaces.EnsurePersonal(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.New(vault.NewKDF(1), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	vaults := vault.NewService(v, store, vault.NewKeyRing(clock, time.Hour))
	if _, err := vaults.Setup(t.Context(), user.ID, session, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	unreliable := &flaky{Store: store}
	allow := netguard.NewPolicy([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, nil)
	quickConnections := quick.NewService(workspaces, allow, nil, clock, nil)
	return &env{
		quick: quickConnections,
		t:     t, store: store, flaky: unreliable, user: user.ID, workspace: personal.ID, vaults: vaults, allow: allow,
		sites:    site.NewService(unreliable, workspaces, vaults, allow, clock, rand.Reader, nil),
		hostKeys: hostkey.NewService(unreliable, workspaces, clock),
	}
}

// addSite saves a site that points at a test server.
func (e *env) addSite(s *sftptest.Server, in site.Input, secrets site.SecretChanges) site.Site {
	e.t.Helper()
	return e.addSiteAt(s.Addr, in, secrets)
}

// addSiteAt saves a site that points at the given host:port.
func (e *env) addSiteAt(address string, in site.Input, secrets site.SecretChanges) site.Site {
	e.t.Helper()
	host, port, err := splitAddr(address)
	if err != nil {
		e.t.Fatal(err)
	}
	in.Host, in.Port = host, port
	if in.Username == "" {
		in.Username = sshUser
	}
	if in.Protocol == "" {
		in.Protocol = site.SFTP
	}
	created, err := e.sites.Create(e.t.Context(), e.user, e.workspace, session, in, secrets)
	if err != nil {
		e.t.Fatalf("Create() = %v", err)
	}
	return created
}

// splitAddr splits host:port into its parts.
func splitAddr(addr string) (string, int, error) {
	prefix, portText, ok := cut(addr)
	if !ok {
		return "", 0, errors.New("no port in " + addr)
	}
	port, err := strconv.Atoi(portText)
	return prefix, port, err
}

// cut splits at the last colon.
func cut(s string) (string, string, bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// key returns the pool key of a site.
func (e *env) key(s site.Site) connections.Key {
	return connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: s.ID}
}

// connector returns a connector with the given network policy.
func (e *env) connector(p *netguard.Policy) *connect.Connector {
	return connect.New(e.sites, e.quick, e.hostKeys, p, 5*time.Second)
}

// trust records the server key of s as trusted for its address.
func (e *env) trust(s *sftptest.Server) {
	e.t.Helper()
	host, port, _ := splitAddr(s.Addr)
	if _, err := e.hostKeys.Trust(e.t.Context(), e.user, e.workspace, host, port, s.HostKey.Type(), ssh.FingerprintSHA256(s.HostKey)); err != nil {
		e.t.Fatalf("Trust() = %v", err)
	}
}

// text returns a pointer to a string.
func text(s string) *string { return &s }

// TestConnector_Password verifies connecting with a stored password and the host key workflow.
//
// Scenarios:
//   - the first connection to an unknown server fails with its fingerprint and does not authenticate
//   - after trusting that fingerprint the connection works and the file system is usable
//   - a server that presents another key than the trusted one is refused as a mismatch
//   - a wrong stored password is ErrAuth
//   - a site without a stored password is ErrAuth
func TestConnector_Password(t *testing.T) {
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	target := e.addSite(server, site.Input{Name: "Files"}, site.SecretChanges{Password: text(sshPassword)})
	c := e.connector(e.allow)

	_, err := c.Connect(t.Context(), e.key(target), session)
	var unknown *remotefs.HostKeyError
	if !errors.Is(err, remotefs.ErrHostKeyUnknown) || !errors.As(err, &unknown) || unknown.Fingerprint != ssh.FingerprintSHA256(server.HostKey) {
		t.Fatalf("first Connect() = %v; want an unknown host key with the server fingerprint", err)
	}

	e.trust(server)
	fsys, err := c.Connect(t.Context(), e.key(target), session)
	if err != nil {
		t.Fatalf("Connect() after trusting = %v", err)
	}
	defer func() { _ = fsys.Close() }()
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() = %v", err)
	}

	impostor := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	other := e.addSite(impostor, site.Input{Name: "Impostor"}, site.SecretChanges{Password: text(sshPassword)})
	host, port, _ := splitAddr(impostor.Addr)
	if _, err := e.hostKeys.Trust(t.Context(), e.user, e.workspace, host, port, impostor.HostKey.Type(), ssh.FingerprintSHA256(server.HostKey)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(t.Context(), e.key(other), session); !errors.Is(err, remotefs.ErrHostKeyMismatch) {
		t.Errorf("Connect() to a server with another key = %v; want ErrHostKeyMismatch", err)
	}

	wrong := e.addSite(server, site.Input{Name: "Wrong"}, site.SecretChanges{Password: text("not the password")})
	if _, err := c.Connect(t.Context(), e.key(wrong), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with a wrong password = %v; want ErrAuth", err)
	}
	none := e.addSite(server, site.Input{Name: "None"}, site.SecretChanges{})
	if _, err := c.Connect(t.Context(), e.key(none), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() without a stored password = %v; want ErrAuth", err)
	}
}

// TestConnector_Key verifies connecting with a stored private key and passphrase.
//
// Scenarios:
//   - a protected key with its stored passphrase connects
//   - a key that is not authorized is ErrAuth, and a site without the private key is ErrAuth
func TestConnector_Key(t *testing.T) {
	e := newEnv(t)
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("open sesame"))
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(block))

	server := sftptest.Start(t, sftptest.Options{Keys: map[string][]ssh.PublicKey{sshUser: {public}}})
	e.trust(server)
	c := e.connector(e.allow)

	good := e.addSite(server, site.Input{Name: "Key", AuthMethod: site.AuthKey}, site.SecretChanges{PrivateKey: text(pemKey), Passphrase: text("open sesame")})
	fsys, err := c.Connect(t.Context(), e.key(good), session)
	if err != nil {
		t.Fatalf("Connect() with a key = %v", err)
	}
	_ = fsys.Close()

	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	strangerBlock, err := ssh.MarshalPrivateKey(stranger, "")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized := e.addSite(server, site.Input{Name: "Stranger", AuthMethod: site.AuthKey}, site.SecretChanges{PrivateKey: text(string(pem.EncodeToMemory(strangerBlock)))})
	if _, err := c.Connect(t.Context(), e.key(unauthorized), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() with an unauthorized key = %v; want ErrAuth", err)
	}
	keyless := e.addSite(server, site.Input{Name: "Keyless", AuthMethod: site.AuthKey}, site.SecretChanges{})
	if _, err := c.Connect(t.Context(), e.key(keyless), session); !errors.Is(err, remotefs.ErrAuth) {
		t.Errorf("Connect() without a stored key = %v; want ErrAuth", err)
	}
}

// TestConnector_Refusals verifies the cases that stop before or while connecting.
//
// Scenarios:
//   - a locked vault is site.ErrLocked and is not retried
//   - a site of another workspace or user is not found, and a stranger cannot connect to it
//   - a destination that the network guard forbids is refused when dialing, even though the site was saved
//   - a failing secret read other than a missing secret is returned
func TestConnector_Refusals(t *testing.T) {
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	target := e.addSite(server, site.Input{Name: "Files"}, site.SecretChanges{Password: text(sshPassword)})
	c := e.connector(e.allow)

	if _, err := c.Connect(t.Context(), e.key(target), "another-session"); !errors.Is(err, site.ErrLocked) || remotefs.Retryable(err) {
		t.Errorf("Connect() with a locked vault = %v; want a non-retryable ErrLocked", err)
	}
	stranger := e.key(target)
	stranger.UserID = uuid.New()
	if _, err := c.Connect(t.Context(), stranger, session); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Connect() for a stranger = %v; want workspace.ErrNotFound", err)
	}
	missing := e.key(target)
	missing.SiteID = uuid.New()
	if _, err := c.Connect(t.Context(), missing, session); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("Connect() for a missing site = %v; want site.ErrNotFound", err)
	}

	strict := e.connector(netguard.NewPolicy(nil, nil))
	if _, err := strict.Connect(t.Context(), e.key(target), session); !errors.Is(err, netguard.ErrBlocked) || remotefs.Retryable(err) {
		t.Errorf("Connect() to a forbidden destination = %v; want a non-retryable ErrBlocked", err)
	}
}

// TestConnector_WithPool verifies the connector inside the pool.
//
// Scenarios:
//   - the manager opens the connection through the connector and reuses it for the next operation
//   - an untrusted server key fails the acquisition at once, without retries
func TestConnector_WithPool(t *testing.T) {
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	target := e.addSite(server, site.Input{Name: "Files"}, site.SecretChanges{Password: text(sshPassword)})
	m := connections.New(e.connector(e.allow), testutil.NewFakeClock(time.Now()), connections.Config{})
	t.Cleanup(m.Close)

	if err := m.With(t.Context(), e.key(target), session, func(remotefs.RemoteFS) error { return nil }); !errors.Is(err, remotefs.ErrHostKeyUnknown) {
		t.Fatalf("With() before trusting = %v; want ErrHostKeyUnknown", err)
	}
	e.trust(server)
	list := func(fsys remotefs.RemoteFS) error { _, err := fsys.List(t.Context(), "/"); return err }
	for i := 0; i < 2; i++ {
		if err := m.With(t.Context(), e.key(target), session, list); err != nil {
			t.Fatalf("With() = %v", err)
		}
	}
	if idle, total := m.Stats(e.key(target)); idle != 1 || total != 1 {
		t.Errorf("Stats() = %d, %d; want one pooled connection", idle, total)
	}
}

// TestConnector_StoreFailures verifies that failures of the stores are returned and never taken for success.
//
// Scenarios:
//   - a failing lookup of the trusted key types fails the connection
//   - a failing check of the presented key, after the types were read, fails the handshake
//   - a failing read of the passphrase, other than a missing one, fails the connection
func TestConnector_StoreFailures(t *testing.T) {
	e := newEnv(t)
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}})
	e.trust(server)
	password := e.addSite(server, site.Input{Name: "Files"}, site.SecretChanges{Password: text(sshPassword)})
	c := e.connector(e.allow)

	for _, failAt := range []int{1, 2} {
		e.flaky.failTrustedAt, e.flaky.trusted = failAt, 0
		if _, err := c.Connect(t.Context(), e.key(password), session); !errors.Is(err, errFlaky) {
			t.Errorf("Connect() failing at call %d = %v; want the store error", failAt, err)
		}
	}
	e.flaky.failTrustedAt = 0

	keyed := e.addSite(server, site.Input{Name: "Keyed", AuthMethod: site.AuthKey}, site.SecretChanges{PrivateKey: text("key"), Passphrase: text("phrase")})
	e.flaky.failPassphrase = true
	if _, err := c.Connect(t.Context(), e.key(keyed), session); !errors.Is(err, errFlaky) {
		t.Errorf("Connect() with a failing passphrase read = %v; want the store error", err)
	}
}

// TestConnector_QuickConnections verifies connecting to quick connections.
//
// Scenarios:
//   - a quick connection with a password connects after its host key is trusted, without touching the vault
//   - one with a private key and its passphrase connects
//   - a quick connection without the credential its method needs is ErrAuth,
//   - an unknown host key is reported with its fingerprint, as for saved sites
//   - quick connections of another user are not found, and the wiped secrets of an ended connection are gone
func TestConnector_QuickConnections(t *testing.T) {
	e := newEnv(t)
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("open sesame"))
	if err != nil {
		t.Fatal(err)
	}
	server := sftptest.Start(t, sftptest.Options{Users: map[string]string{sshUser: sshPassword}, Keys: map[string][]ssh.PublicKey{sshUser: {public}}})
	host, port, _ := splitAddr(server.Addr)
	c := e.connector(e.allow)

	start := func(in site.Input, secrets quick.Secrets) uuid.UUID {
		t.Helper()
		in.Host, in.Port, in.Username, in.Protocol = host, port, sshUser, site.SFTP
		created, err := e.quick.Create(t.Context(), e.user, e.workspace, in, secrets)
		if err != nil {
			t.Fatalf("Create() = %v", err)
		}
		return created.Site.ID
	}
	withPassword := start(site.Input{}, quick.Secrets{Password: []byte(sshPassword)})

	if _, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: withPassword}, "no-vault-session"); !errors.Is(err, remotefs.ErrHostKeyUnknown) {
		t.Fatalf("Connect() before trusting = %v; want ErrHostKeyUnknown", err)
	}
	e.trust(server)
	fsys, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: withPassword}, "no-vault-session")
	if err != nil {
		t.Fatalf("Connect() to a quick connection = %v", err)
	}
	if _, err := fsys.List(t.Context(), "/"); err != nil {
		t.Errorf("List() = %v", err)
	}
	_ = fsys.Close()

	withKey := start(site.Input{AuthMethod: site.AuthKey}, quick.Secrets{PrivateKey: pem.EncodeToMemory(block), Passphrase: []byte("open sesame")})
	if fsys, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: withKey}, "s"); err != nil {
		t.Errorf("Connect() with a key = %v", err)
	} else {
		_ = fsys.Close()
	}

	keyless := start(site.Input{AuthMethod: site.AuthKey}, quick.Secrets{})
	passwordless := start(site.Input{}, quick.Secrets{})
	for _, id := range []uuid.UUID{keyless, passwordless} {
		if _, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: id}, "s"); !errors.Is(err, remotefs.ErrAuth) {
			t.Errorf("Connect() without credentials = %v; want ErrAuth", err)
		}
	}

	if _, err := c.Connect(t.Context(), connections.Key{UserID: uuid.New(), WorkspaceID: e.workspace, SiteID: withPassword}, "s"); err == nil {
		t.Error("another user connected through a quick connection they do not own")
	}
	if err := e.quick.Close(e.user, e.workspace, withPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(t.Context(), connections.Key{UserID: e.user, WorkspaceID: e.workspace, SiteID: withPassword}, "s"); !errors.Is(err, site.ErrNotFound) {
		t.Errorf("Connect() to an ended quick connection = %v; want site.ErrNotFound", err)
	}
}
