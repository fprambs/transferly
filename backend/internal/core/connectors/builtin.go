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
	// AuthAccessKey signs the requests to an object store with an access key ID and its secret access key (ADR-0023).
	AuthAccessKey = "access_key"
	// AuthAccountKey signs the requests to Azure Storage with the key of the storage account (ADR-0025).
	AuthAccountKey = "account_key"
	// AuthSAS authorizes the requests to Azure Storage with a shared access signature (ADR-0025).
	AuthSAS = "sas"
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

// hostField is where an SFTP or FTP server is.
var hostField = Field{Name: FieldHost, Kind: Host, Group: GroupConnection, Required: true, Port: FieldPort}

// usernameField is who logs in to an SFTP or FTP server, the anonymous user for anonymous sites. It is shown with the logon type, which is
// declared before it.
var usernameField = Field{
	Name: FieldUsername, Kind: Text, Group: GroupAccess, Required: true, MaxLength: maxUsernameRunes,
	Fixed: []Default{{When: Condition{Field: FieldAuth, In: []string{AuthAnonymous}}, Value: AnonymousUser}},
}

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

// SFTP describes the SFTP connector, which Transferly has had from its start. A connection may tunnel through a jump host.
var SFTP = Descriptor{
	Type:     "sftp",
	Name:     "SFTP",
	Icon:     "sftp",
	Maturity: Stable,
	Capabilities: Capabilities{
		Folders: true, Permissions: true, SetTimes: true, Resume: true, Rename: true,
	},
	Fields: append(append([]Field{
		hostField,
		{Name: FieldPort, Kind: Number, Group: GroupConnection, Required: true, Default: "22", Min: 1, Max: maxPort},
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthPassword, Choices: []string{AuthPassword, AuthAsk, AuthInteractive, AuthKey, AuthAnonymous}},
		usernameField,
		{Name: "password", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthPassword)},
		{Name: "private_key", Kind: PrivateKey, Group: GroupAccess, Passphrase: "passphrase", When: whenAuth(AuthKey)},
		{Name: "passphrase", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthKey)},
	}, proxyFields()...), jumpFields()...),
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

// FTP describes the FTP connector, plain or with TLS, explicit or implicit. New sites require TLS (ADR-0012). Listings carry no modes
// Transferly can rely on, so it does not offer permissions.
var FTP = Descriptor{
	Type:     "ftp",
	Name:     "FTP / FTPS",
	Icon:     "ftps",
	Maturity: Stable,
	Capabilities: Capabilities{
		Folders: true, SetTimes: true, Resume: true, Rename: true,
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
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthPassword, Choices: []string{AuthPassword, AuthAsk, AuthInteractive, AuthAccount, AuthAnonymous}},
		usernameField,
		{Name: "password", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthPassword, AuthAccount)},
		{Name: "account", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthAccount)},
		{Name: "list_timezone", Kind: TimeZone, Group: GroupAdvanced, Required: true, Default: "UTC"},
		{Name: "charset", Kind: Choice, Group: GroupCharset, Required: true, Default: "utf-8", Choices: Charsets},
	}, proxyFields()...),
	Normalize: func(config map[string]string) map[string]string {
		if charset, ok := config["charset"]; ok {
			config["charset"] = strings.ToLower(charset)
		}
		return config
	},
	EnabledByDefault: true,
}

// S3 describes the connector of S3 buckets and the services compatible with its API (ADR-0023). Folders are prefixes the
// adapter emulates, objects are written whole and have no modes nor times that can be set, and renames copy on the server.
// It is new, so an admin enables it.
var S3 = Descriptor{
	Type:     "s3",
	Name:     "S3",
	Icon:     "cloud",
	Maturity: Beta,
	Capabilities: Capabilities{
		Rename: true,
	},
	Fields: append([]Field{
		{Name: FieldHost, Kind: Host, Group: GroupConnection, Required: true, Default: "s3.amazonaws.com", Port: FieldPort},
		{Name: "tls", Kind: Boolean, Group: GroupConnection, Required: true, Default: "true"},
		{
			Name: FieldPort, Kind: Number, Group: GroupConnection, Required: true, Default: "443", Min: 1, Max: maxPort,
			Defaults: []Default{{When: Condition{Field: "tls", In: []string{"false"}}, Value: "80"}},
		},
		{Name: "region", Kind: Text, Group: GroupConnection, MaxLength: 64},
		{Name: "bucket", Kind: Text, Group: GroupConnection, Required: true, MaxLength: 63},
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthAccessKey, Choices: []string{AuthAccessKey, AuthAnonymous}},
		{Name: "access_key_id", Kind: Text, Group: GroupAccess, Required: true, MaxLength: 128, When: whenAuth(AuthAccessKey)},
		{Name: "secret_access_key", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthAccessKey)},
	}, proxyFields()...),
}

// GCS describes the connector of Google Cloud Storage buckets, reached through the S3 compatible XML API with an HMAC key
// (ADR-0024). It behaves as S3 does, always over HTTPS; the endpoint only changes for tests and private gateways. It is new, so an
// admin enables it.
var GCS = Descriptor{
	Type:     "gcs",
	Name:     "Google Cloud Storage",
	Icon:     "gcs",
	Maturity: Beta,
	Capabilities: Capabilities{
		Rename: true,
	},
	Fields: append([]Field{
		{Name: "bucket", Kind: Text, Group: GroupConnection, Required: true, MaxLength: 222},
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthAccessKey, Choices: []string{AuthAccessKey, AuthAnonymous}},
		{Name: "access_key_id", Kind: Text, Group: GroupAccess, Required: true, MaxLength: 128, When: whenAuth(AuthAccessKey)},
		{Name: "secret_access_key", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthAccessKey)},
		{Name: FieldHost, Kind: Host, Group: GroupAdvanced, Required: true, Default: "storage.googleapis.com", Port: FieldPort},
		{Name: FieldPort, Kind: Number, Group: GroupAdvanced, Required: true, Default: "443", Min: 1, Max: maxPort},
	}, proxyFields()...),
}

// ProviderGoogle names the sign-in with Google of OAuth fields (ADR-0024).
const ProviderGoogle = "google"

// GDrive describes the connector of Google Drive (ADR-0024): My Drive of the Google account that signed in, or one of its shared
// drives by name. Its folders are real, renames are native and times can be set. It is new, so an admin enables it.
var GDrive = Descriptor{
	Type:     "gdrive",
	Name:     "Google Drive",
	Icon:     "gdrive",
	Maturity: Beta,
	Capabilities: Capabilities{
		Folders: true, SetTimes: true, Rename: true,
	},
	Fields: append([]Field{
		{Name: "shared_drive", Kind: Text, Group: GroupConnection, MaxLength: 255},
		{Name: "account_email", Kind: Text, Group: GroupAccess, MaxLength: 320},
		{Name: "refresh_token", Kind: OAuth, Group: GroupAccess, Provider: ProviderGoogle, Account: "account_email"},
	}, proxyFields()...),
}

// azureFields are the settings of a connector of Azure Storage (ADR-0025): the account, its container or share, how requests are
// authorized, and an endpoint that replaces the one of Azure, for sovereign clouds and emulators.
func azureFields(resource string, logons ...string) []Field {
	return append([]Field{
		{Name: "account", Kind: Text, Group: GroupConnection, Required: true, MaxLength: 24},
		{Name: resource, Kind: Text, Group: GroupConnection, Required: true, MaxLength: 63},
		{Name: FieldAuth, Kind: Choice, Group: GroupAccess, Required: true, Default: AuthAccountKey, Choices: logons},
		{Name: "account_key", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthAccountKey)},
		{Name: "sas_token", Kind: Secret, Group: GroupAccess, When: whenAuth(AuthSAS)},
		{Name: "tls", Kind: Boolean, Group: GroupAdvanced, Required: true, Default: "true"},
		{Name: FieldHost, Kind: Host, Group: GroupAdvanced, Port: FieldPort},
		{
			Name: FieldPort, Kind: Number, Group: GroupAdvanced, Required: true, Default: "443", Min: 1, Max: maxPort,
			Defaults: []Default{{When: Condition{Field: "tls", In: []string{"false"}}, Value: "80"}},
		},
	}, proxyFields()...)
}

// AzureBlob describes the connector of containers of Azure Blob Storage (ADR-0025). It behaves as S3 does: folders are prefixes
// the adapter emulates, blobs are written whole and have no modes nor times that can be set, and renames copy on the server. It
// is new, so an admin enables it.
var AzureBlob = Descriptor{
	Type:         "azblob",
	Name:         "Azure Blob Storage",
	Icon:         "azblob",
	Maturity:     Beta,
	Capabilities: Capabilities{Rename: true},
	Fields:       azureFields("container", AuthAccountKey, AuthSAS, AuthAnonymous),
}

// AzureFiles describes the connector of shares of Azure Files (ADR-0025): real directories, writes that resume, native renames and
// times that can be set. It is new, so an admin enables it.
var AzureFiles = Descriptor{
	Type:         "azfiles",
	Name:         "Azure Files",
	Icon:         "azfiles",
	Maturity:     Beta,
	Capabilities: Capabilities{Folders: true, SetTimes: true, Resume: true, Rename: true},
	Fields:       azureFields("share", AuthAccountKey, AuthSAS),
}

// Builtin returns a catalog with the connectors Transferly is built with.
func Builtin() *Catalog {
	c := NewCatalog()
	for _, d := range []Descriptor{SFTP, FTP, S3, GCS, GDrive, AzureBlob, AzureFiles} {
		c.descriptors[d.Type] = d
	}
	return c
}
