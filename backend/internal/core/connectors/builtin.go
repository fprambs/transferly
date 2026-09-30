package connectors

import "github.com/fprambs/sentinel/internal/core/site"

// The settings SFTP and FTP share: where the server is and who logs in.
var (
	hostField     = Field{Name: "host", Kind: Text, Required: true, MaxLength: 255}
	usernameField = Field{Name: "username", Kind: Text, MaxLength: 255}
	charsetField  = Field{Name: "charset", Kind: Choice, Default: "utf-8", Choices: site.Charsets}
)

// SFTP describes the SFTP connector, which Sentinel has had from its start.
var SFTP = Descriptor{
	Type:     "sftp",
	Name:     "SFTP",
	Icon:     "server",
	Maturity: Stable,
	Capabilities: Capabilities{
		Folders: true, Permissions: true, SetTimes: true, Resume: true, Rename: true,
	},
	Fields: []Field{hostField, {Name: "port", Kind: Number, Default: "22", Min: 1, Max: 65535}, usernameField},
	Auth:   []Auth{AuthPassword, AuthKey, AuthAsk, AuthInteractive},

	EnabledByDefault: true,
}

// FTP describes the FTP connector, plain or with TLS, explicit or implicit.
var FTP = Descriptor{
	Type:     "ftp",
	Name:     "FTP / FTPS",
	Icon:     "server",
	Maturity: Stable,
	Capabilities: Capabilities{
		Folders: true, Permissions: true, SetTimes: true, Resume: true, Rename: true,
	},
	Fields: []Field{
		hostField,
		{Name: "port", Kind: Number, Default: "21", Min: 1, Max: 65535},
		usernameField,
		{Name: "encryption", Kind: Choice, Default: string(site.EncryptionOptional), Choices: []string{
			string(site.EncryptionOptional), string(site.EncryptionExplicit), string(site.EncryptionImplicit), string(site.EncryptionPlain),
		}},
		charsetField,
	},
	Auth: []Auth{AuthPassword, AuthAsk, AuthAnonymous, AuthAccount},

	EnabledByDefault: true,
}

// Builtin returns a catalog with the connectors Sentinel is built with.
func Builtin() *Catalog {
	c := NewCatalog()
	for _, d := range []Descriptor{SFTP, FTP} {
		c.descriptors[d.Type] = d
	}
	return c
}
