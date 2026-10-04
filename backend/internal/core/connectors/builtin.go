package connectors

import "strings"

// Charsets lists the character sets an FTP site may use for file names, by their lower case IANA names; UTF-8 is the default.
var Charsets = []string{
	"utf-8", "iso-8859-1", "iso-8859-2", "iso-8859-15", "windows-1250", "windows-1251", "windows-1252", "koi8-r", "shift_jis", "euc-jp", "gbk", "big5", "euc-kr",
}

// The logon types, as values of the auth field (ADR-0010, ADR-0016).
const (
	// AuthPassword logs in with a stored password.
	AuthPassword = "password"
	// AuthKey logs in with an SSH private key.
	AuthKey = "key"
	// AuthAsk logs in with a password that is asked for when connecting and never stored.
	AuthAsk = "ask"
	// AuthAnonymous logs in to a public server as the anonymous user, with no stored secret.
	AuthAnonymous = "anonymous"
	// AuthAccount logs in to an FTP server with a password and an account (ACCT), as FileZilla's account logon type.
	AuthAccount = "account"
	// AuthInteractive asks the person connecting whatever the server asks, and stores nothing.
	AuthInteractive = "interactive"
)

// The FTP encryptions, as FileZilla offers them (ADR-0012).
const (
	// EncryptionOptional upgrades with AUTH TLS when the server offers it and carries on in clear text when it does not.
	EncryptionOptional = "optional"
	// EncryptionExplicit requires the upgrade with AUTH TLS.
	EncryptionExplicit = "explicit"
	// EncryptionImplicit expects TLS from the first byte, as on port 990.
	EncryptionImplicit = "implicit"
	// EncryptionPlain sends everything, credentials included, in clear text.
	EncryptionPlain = "plain"
)

// The proxy protocols a connection can go through.
const (
	// ProxySOCKS5 tunnels the connection through a SOCKS5 proxy.
	ProxySOCKS5 = "socks5"
	// ProxyHTTP tunnels the connection through an HTTP CONNECT proxy.
	ProxyHTTP = "http"
)

// AnonymousUser is the account name of every anonymous site.
const AnonymousUser = "anonymous"

// The names of the settings several connectors share; features that are not specific to a connector read them (ADR-0022).
const (
	FieldHost     = "host"
	FieldPort     = "port"
	FieldUsername = "username"
	FieldAuth     = "auth"
)

// The bounds of the settings SFTP and FTP share.
const (
	maxUsernameRunes = 255
	maxPort          = 65535
)

// Shared settings of SFTP and FTP: where the server is and who logs in.
var (
	hostField     = Field{Name: FieldHost, Kind: Host, Group: GroupConnection, Required: true, Port: FieldPort}
	usernameField = Field{Name: FieldUsername, Kind: Text, Group: GroupConnection, Required: true, MaxLength: maxUsernameRunes}
)

// whenAuth applies a field to the given logon types.
func whenAuth(types ...string) *Condition { return &Condition{Field: FieldAuth, In: types} }

// proxyFields are the settings of the proxy a connection goes through, which apply once a proxy kind is chosen.
func proxyFields() []Field {
	set := &Condition{Field: "proxy_kind"}
	return []Field{
		{Name: "proxy_kind", Kind: Choice, Group: GroupProxy, Choices: []string{ProxySOCKS5, ProxyHTTP}},
		{Name: "proxy_host", Kind: Host, Group: GroupProxy, Required: true, Port: "proxy_port", When: set},
		{
			Name: "proxy_port", Kind: Number, Group: GroupProxy, Required: true, Min: 1, Max: maxPort, When: set,
			Defaults: []Default{{When: Condition{Field: "proxy_kind", In: []string{ProxySOCKS5}}, Value: "1080"}, {When: Condition{Field: "proxy_kind", In: []string{ProxyHTTP}}, Value: "8080"}},
		},
		{Name: "proxy_username", Kind: Text, Group: GroupProxy, MaxLength: maxUsernameRunes, When: set},
		{Name: "proxy_password", Kind: Secret, Group: GroupProxy, When: set},
	}
}

// anonymousUser gives anonymous sites the anonymous user, whatever was typed.
func anonymousUser(config map[string]string) map[string]string {
	if config[FieldAuth] == AuthAnonymous {
		config[FieldUsername] = AnonymousUser
	}
	return config
}

// SFTP describes the SFTP connector, which Transferly has had from its start. A connection may tunnel through a jump host.
var SFTP = Descriptor{
	Type:     "sftp",
	Name:     "SFTP",
	Icon:     "server",
	Maturity: Stable,
	Capabilities: Capabilities{
		Folders: true, Permissions: true, SetTimes: true, Resume: true, Rename: true,
	},
	Fields: append(append([]Field{
		hostField,
		{Name: FieldPort, Kind: Number, Group: GroupConnection, Required: true, Default: "22", Min: 1, Max: maxPort},
		usernameField,
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthPassword, Choices: []string{AuthPassword, AuthAsk, AuthInteractive, AuthKey, AuthAnonymous}},
		{Name: "password", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthPassword)},
		{Name: "private_key", Kind: PrivateKey, Group: GroupAccess, Passphrase: "passphrase", When: whenAuth(AuthKey)},
		{Name: "passphrase", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthKey)},
	}, proxyFields()...), jumpFields()...),
	Normalize:        anonymousUser,
	EnabledByDefault: true,
}

// jumpFields are the settings of the SSH server an SFTP connection tunnels through, which apply once its host is given.
func jumpFields() []Field {
	set := &Condition{Field: "jump_host"}
	whenJumpAuth := func(types ...string) *Condition { return &Condition{Field: "jump_auth", In: types} }
	return []Field{
		{Name: "jump_host", Kind: Host, Group: GroupJump, Port: "jump_port"},
		{Name: "jump_port", Kind: Number, Group: GroupJump, Required: true, Default: "22", Min: 1, Max: maxPort, When: set},
		{Name: "jump_username", Kind: Text, Group: GroupJump, Required: true, MaxLength: maxUsernameRunes, When: set},
		{Name: "jump_auth", Kind: Choice, Group: GroupJump, Required: true, Default: AuthPassword, Choices: []string{AuthPassword, AuthKey}, When: set},
		{Name: "jump_password", Kind: Secret, Group: GroupJump, When: whenJumpAuth(AuthPassword)},
		{Name: "jump_private_key", Kind: PrivateKey, Group: GroupJump, Passphrase: "jump_passphrase", When: whenJumpAuth(AuthKey)},
		{Name: "jump_passphrase", Kind: Secret, Group: GroupJump, When: whenJumpAuth(AuthKey)},
	}
}

// FTP describes the FTP connector, plain or with TLS, explicit or implicit. New sites require TLS (ADR-0012).
var FTP = Descriptor{
	Type:     "ftp",
	Name:     "FTP / FTPS",
	Icon:     "server",
	Maturity: Stable,
	Capabilities: Capabilities{
		Folders: true, Permissions: true, SetTimes: true, Resume: true, Rename: true,
	},
	Fields: append([]Field{
		hostField,
		{
			Name: "encryption", Kind: Choice, Group: GroupConnection, Required: true, Default: EncryptionExplicit,
			Choices: []string{EncryptionOptional, EncryptionExplicit, EncryptionImplicit, EncryptionPlain},
		},
		{
			Name: FieldPort, Kind: Number, Group: GroupConnection, Required: true, Default: "21", Min: 1, Max: maxPort,
			Defaults: []Default{{When: Condition{Field: "encryption", In: []string{EncryptionImplicit}}, Value: "990"}},
		},
		usernameField,
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthPassword, Choices: []string{AuthPassword, AuthAsk, AuthInteractive, AuthAccount, AuthAnonymous}},
		{Name: "password", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthPassword, AuthAccount)},
		{Name: "account", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthAccount)},
		{Name: "list_timezone", Kind: TimeZone, Group: GroupAdvanced, Required: true, Default: "UTC"},
		{Name: "charset", Kind: Choice, Group: GroupCharset, Required: true, Default: "utf-8", Choices: Charsets},
	}, proxyFields()...),
	Normalize: func(config map[string]string) map[string]string {
		if charset, ok := config["charset"]; ok {
			config["charset"] = strings.ToLower(charset)
		}
		return anonymousUser(config)
	},
	EnabledByDefault: true,
}

// Builtin returns a catalog with the connectors Transferly is built with.
func Builtin() *Catalog {
	c := NewCatalog()
	for _, d := range []Descriptor{SFTP, FTP} {
		c.descriptors[d.Type] = d
	}
	return c
}
