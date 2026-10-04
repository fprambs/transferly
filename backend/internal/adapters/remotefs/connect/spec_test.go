package connect_test

import (
	"strconv"

	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/site"
)

// proxySpec is the proxy of a test site.
type proxySpec struct {
	Kind, Host string
	Port       int
	Username   string
}

// jumpSpec is the jump host of a test site.
type jumpSpec struct {
	Host       string
	Port       int
	Username   string
	AuthMethod string
}

// spec describes a test site in the terms of the SFTP and FTP schemas: the connector (protocol, ftps meaning FTP with TLS), the FTP
// encryption, the host, port and username, the logon type, and its proxy and jump host. input turns it into the input of a site.
type spec struct {
	Name, Protocol, FTPEncryption, Host string
	Port                                int
	Username, AuthMethod                string
	Proxy                               proxySpec
	Jump                                jumpSpec
}

// input returns the site input a spec describes. ftps reads as FTP with implicit TLS on port 990 and explicit TLS elsewhere.
func (s spec) input() site.Input {
	connector, encryption := s.Protocol, s.FTPEncryption
	if connector == "" {
		connector = "sftp"
	}
	if connector == "ftps" {
		connector, encryption = "ftp", "explicit"
		if s.Port == 990 {
			encryption = "implicit"
		}
	}
	if connector == "ftp" && encryption == "" {
		encryption = "plain"
	}
	config := map[string]string{"host": s.Host, "username": s.Username, "auth": s.AuthMethod, "encryption": encryption}
	if s.Port != 0 {
		config["port"] = strconv.Itoa(s.Port)
	}
	if s.Proxy.Kind != "" {
		config["proxy_kind"], config["proxy_host"], config["proxy_username"] = s.Proxy.Kind, s.Proxy.Host, s.Proxy.Username
		config["proxy_port"] = strconv.Itoa(s.Proxy.Port)
	}
	if s.Jump.Host != "" {
		config["jump_host"], config["jump_username"], config["jump_auth"] = s.Jump.Host, s.Jump.Username, s.Jump.AuthMethod
		if s.Jump.Port != 0 {
			config["jump_port"] = strconv.Itoa(s.Jump.Port)
		}
	}
	for name, value := range config {
		if value == "" {
			delete(config, name)
		}
	}
	return site.Input{Name: s.Name, Connector: connector, Config: config}
}

// changes are the secrets a test site is saved with; a nil one is left out.
type changes struct {
	Password, PrivateKey, Passphrase, JumpPassword, JumpPrivateKey, JumpPassphrase, ProxyPassword, Account *string
}

// secrets returns the secret changes of a test site.
func (c changes) secrets() site.SecretChanges {
	out := site.SecretChanges{}
	for kind, value := range map[site.SecretKind]*string{
		site.SecretPassword: c.Password, site.SecretPrivateKey: c.PrivateKey, site.SecretPassphrase: c.Passphrase,
		site.SecretJumpPassword: c.JumpPassword, site.SecretJumpPrivateKey: c.JumpPrivateKey, site.SecretJumpPassphrase: c.JumpPassphrase,
		site.SecretProxyPassword: c.ProxyPassword, site.SecretAccount: c.Account,
	} {
		if value != nil {
			out[kind] = *value
		}
	}
	return out
}

// qsecrets are the secrets of a test quick connection; an empty one is left out.
type qsecrets struct {
	Password, PrivateKey, Passphrase []byte
}

// secrets returns the secrets of a test quick connection.
func (q qsecrets) secrets() quick.Secrets {
	out := quick.Secrets{}
	for kind, value := range map[site.SecretKind][]byte{site.SecretPassword: q.Password, site.SecretPrivateKey: q.PrivateKey, site.SecretPassphrase: q.Passphrase} {
		if len(value) > 0 {
			out[kind] = value
		}
	}
	return out
}
