package connectors_test

import (
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/fprambs/transferly/internal/core/connectors"
)

// sample is a connector with one field of each kind, a condition and a dependent default.
var sample = connectors.Descriptor{
	Type:     "sample",
	Name:     "Sample",
	Maturity: connectors.Beta,
	Fields: []connectors.Field{
		{Name: "host", Kind: connectors.Host, Required: true, Port: "port"},
		{Name: "mode", Kind: connectors.Choice, Choices: []string{"a", "b"}},
		{Name: "port", Kind: connectors.Number, Default: "22", Min: 1, Max: 100, Defaults: []connectors.Default{{When: connectors.Condition{Field: "mode", In: []string{"b"}}, Value: "99"}}},
		{Name: "count", Kind: connectors.Number},
		{Name: "secure", Kind: connectors.Boolean, Default: "true"},
		{Name: "note", Kind: connectors.Text, MaxLength: 3},
		{Name: "zone", Kind: connectors.TimeZone},
		{Name: "region", Kind: connectors.Text, Required: true, Default: "us", Fixed: []connectors.Default{{When: connectors.Condition{Field: "mode", In: []string{"a"}}, Value: "eu"}}},
		{Name: "extra", Kind: connectors.Text, Required: true, When: &connectors.Condition{Field: "mode"}},
		{Name: "key", Kind: connectors.PrivateKey, Passphrase: "phrase", When: &connectors.Condition{Field: "mode", In: []string{"a"}}},
		{Name: "phrase", Kind: connectors.Secret},
	},
}

// TestCatalog verifies registering and finding connectors.
//
// Use cases:
//   - the built-in catalog has SFTP and FTP, stable and enabled by default
//   - a connector registers once; a second one of its type, or one that contradicts itself, is refused
//   - an unknown type is not found
//   - all connectors are listed stable ones first, each group by name, also while others register at the same time
func TestCatalog(t *testing.T) {
	catalog := connectors.Builtin()
	sftp, err := catalog.Lookup("sftp")
	if err != nil || sftp.Name != "SFTP" || sftp.Maturity != connectors.Stable || !sftp.EnabledByDefault || !sftp.Capabilities.Resume {
		t.Fatalf("sftp = %+v, %v", sftp, err)
	}
	if err := catalog.Register(sample); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Register(sample); !errors.Is(err, connectors.ErrDuplicate) {
		t.Errorf("duplicate = %v", err)
	}
	if err := catalog.Register(connectors.Descriptor{Type: "x"}); !errors.Is(err, connectors.ErrInvalidDescriptor) {
		t.Errorf("nameless = %v", err)
	}
	if _, err := catalog.Lookup("gopher"); !errors.Is(err, connectors.ErrUnknown) {
		t.Errorf("unknown = %v", err)
	}
	var wg sync.WaitGroup
	for _, kind := range []string{"a-beta", "z-stable"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			maturity := connectors.Beta
			if kind == "z-stable" {
				maturity = connectors.Stable
			}
			_ = catalog.Register(connectors.Descriptor{Type: kind, Name: kind, Maturity: maturity})
		}()
	}
	wg.Wait()
	var names []string
	for _, d := range catalog.All() {
		names = append(names, d.Name)
	}
	if want := []string{"FTP / FTPS", "SFTP", "z-stable", "Azure Blob Storage", "Azure Files", "Google Cloud Storage", "Google Drive", "S3", "Sample", "WebDAV", "a-beta"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("all = %v; want %v", names, want)
	}
}

// TestDescriptor_Check verifies that descriptors that contradict themselves are refused.
//
// Use cases:
//   - the built-in descriptors and the sample are consistent
//   - a field without a name or declared twice, a condition, a dependent default or a fixed value on a field declared after it, a host without a number port
//     field, a private key without a secret passphrase field, and an OAuth field without a provider or with an account field that is not
//     text are refused; an OAuth field may go without an account field
func TestDescriptor_Check(t *testing.T) {
	for _, d := range []connectors.Descriptor{connectors.SFTP, connectors.FTP, connectors.S3, connectors.GCS, connectors.GDrive, connectors.AzureBlob, connectors.AzureFiles, connectors.WebDAV, sample} {
		if err := d.Check(); err != nil {
			t.Errorf("%s: %v", d.Type, err)
		}
	}
	text := func(name string) connectors.Field { return connectors.Field{Name: name, Kind: connectors.Text} }
	bad := map[string][]connectors.Field{
		"nameless":               {{Kind: connectors.Text}},
		"twice":                  {text("a"), text("a")},
		"condition later":        {{Name: "a", Kind: connectors.Text, When: &connectors.Condition{Field: "b"}}, text("b")},
		"default later":          {{Name: "a", Kind: connectors.Text, Defaults: []connectors.Default{{When: connectors.Condition{Field: "b"}}}}, text("b")},
		"fixed later":            {{Name: "a", Kind: connectors.Text, Fixed: []connectors.Default{{When: connectors.Condition{Field: "b"}}}}, text("b")},
		"host without port":      {{Name: "h", Kind: connectors.Host, Port: "p"}},
		"host text port":         {{Name: "h", Kind: connectors.Host, Port: "p"}, text("p")},
		"key without phrase":     {{Name: "k", Kind: connectors.PrivateKey, Passphrase: "p"}},
		"key text phrase":        {{Name: "k", Kind: connectors.PrivateKey, Passphrase: "p"}, text("p")},
		"oauth without provider": {{Name: "t", Kind: connectors.OAuth}},
		"oauth missing account":  {{Name: "t", Kind: connectors.OAuth, Provider: "google", Account: "a"}},
		"oauth secret account":   {{Name: "a", Kind: connectors.Secret}, {Name: "t", Kind: connectors.OAuth, Provider: "google", Account: "a"}},
	}
	if err := (connectors.Descriptor{Type: "x", Name: "X", Fields: []connectors.Field{{Name: "t", Kind: connectors.OAuth, Provider: "google"}}}).Check(); err != nil {
		t.Errorf("an OAuth field without an account field = %v; want it accepted", err)
	}
	for name, fields := range bad {
		if err := (connectors.Descriptor{Type: "x", Name: "X", Fields: fields}).Check(); !errors.Is(err, connectors.ErrInvalidDescriptor) {
			t.Errorf("%s: Check() = %v; want ErrInvalidDescriptor", name, err)
		}
	}
}

// TestValidate verifies a configuration against the settings of a connector.
//
// Use cases:
//   - a valid configuration is trimmed, keeps its values, gets the defaults of what it left out, a dependent default once its condition holds,
//     a fixed value whatever was given while its condition holds, and drops the fields that do not apply and the empty optional ones
//   - a field that applies only under a condition is required then
//   - unknown settings, secrets, missing required ones, bad numbers, numbers out of range, bad booleans, choices not offered, texts too long,
//     bad hosts and bad time zones are refused, naming the field and the reason
func TestValidate(t *testing.T) {
	got, err := sample.Validate(map[string]string{"host": " a.b ", "count": "-7", "note": ""})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"host": "a.b", "port": "22", "count": "-7", "secure": "true", "region": "us"}; !maps.Equal(got, want) {
		t.Errorf("Validate() = %v; want %v", got, want)
	}
	if fixed, err := sample.Validate(map[string]string{"host": "a.b", "mode": "a", "extra": "x", "region": "us"}); err != nil || fixed["region"] != "eu" {
		t.Errorf("Validate() with a fixed value = %v, %v; want region eu whatever was given", fixed, err)
	}
	got, err = sample.Validate(map[string]string{"host": "a.b", "mode": "b", "extra": "x", "zone": "America/Santiago"})
	if err != nil || got["port"] != "99" || got["extra"] != "x" || got["zone"] != "America/Santiago" {
		t.Errorf("Validate() with mode b = %v, %v; want port 99 and the extra setting", got, err)
	}
	cases := map[string]struct {
		config map[string]string
		field  string
		reason string
	}{
		"unknown":     {map[string]string{"host": "a", "shade": "x"}, "shade", connectors.ReasonUnknown},
		"secret":      {map[string]string{"host": "a", "phrase": "x"}, "phrase", connectors.ReasonSecret},
		"required":    {map[string]string{}, "host", connectors.ReasonRequired},
		"conditional": {map[string]string{"host": "a", "mode": "a"}, "extra", connectors.ReasonRequired},
		"number":      {map[string]string{"host": "a", "port": "x"}, "port", connectors.ReasonNumber},
		"range":       {map[string]string{"host": "a", "port": "101"}, "port", connectors.ReasonRange},
		"boolean":     {map[string]string{"host": "a", "secure": "yes"}, "secure", connectors.ReasonBoolean},
		"choice":      {map[string]string{"host": "a", "mode": "c"}, "mode", connectors.ReasonChoice},
		"length":      {map[string]string{"host": "a", "note": "ñandú"}, "note", connectors.ReasonLength},
		"host":        {map[string]string{"host": "a b"}, "host", connectors.ReasonHost},
		"zone":        {map[string]string{"host": "a", "zone": "Mars/Base"}, "zone", connectors.ReasonTimeZone},
		"local zone":  {map[string]string{"host": "a", "zone": "Local"}, "zone", connectors.ReasonTimeZone},
	}
	for name, c := range cases {
		_, err := sample.Validate(c.config)
		var field *connectors.FieldError
		if !errors.As(err, &field) || field.Field != c.field || field.Reason != c.reason || !errors.Is(err, connectors.ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
		if err != nil && err.Error() == "" {
			t.Errorf("%s: empty message", name)
		}
	}
}

// TestBuiltin verifies the schemas of SFTP and FTP, which hold the rules the site form had (ADR-0022).
//
// Use cases:
//   - an SFTP site defaults to port 22 and password logon; the secrets, the key and its passphrase are fields of the vault that apply to their
//     logon types only
//   - an anonymous site gets the anonymous user whatever was typed
//   - a proxy applies once its kind is chosen, with the port of that kind; a jump host applies once its host is given, with port 22 and
//     password logon
//   - FTP requires TLS by default, defaults to port 990 for implicit TLS and 21 otherwise, lists in UTC, uses UTF-8 and reads the character set
//     in any case; an SFTP site has no character set and FTP has no jump host
//   - a key logon is refused for FTP and an account logon for SFTP
func TestBuiltin(t *testing.T) {
	sftp, err := connectors.SFTP.Validate(map[string]string{"host": "h.example", "username": "deploy"})
	if want := map[string]string{"host": "h.example", "port": "22", "username": "deploy", "auth": "password"}; err != nil || !maps.Equal(sftp, want) {
		t.Errorf("SFTP defaults = %v, %v; want %v", sftp, err, want)
	}
	password, _ := connectors.SFTP.Field("password")
	key, _ := connectors.SFTP.Field("private_key")
	if !password.IsSecret() || !password.Applies(sftp) || key.Applies(sftp) || key.Passphrase != "passphrase" {
		t.Errorf("password %+v applies %v, key %+v applies %v", password, password.Applies(sftp), key, key.Applies(sftp))
	}
	if _, ok := connectors.SFTP.Field("charset"); ok {
		t.Error("SFTP has a character set")
	}

	anonymous, err := connectors.FTP.Validate(map[string]string{"host": "h", "username": "someone", "auth": "anonymous"})
	if err != nil || anonymous["username"] != connectors.AnonymousUser {
		t.Errorf("anonymous = %v, %v", anonymous, err)
	}

	proxied, err := connectors.SFTP.Validate(map[string]string{"host": "h", "username": "u", "proxy_kind": "socks5", "proxy_host": "p", "proxy_username": "pu"})
	if err != nil || proxied["proxy_port"] != "1080" || proxied["proxy_username"] != "pu" {
		t.Errorf("SOCKS5 proxy = %v, %v", proxied, err)
	}
	proxied, _ = connectors.FTP.Validate(map[string]string{"host": "h", "username": "u", "proxy_kind": "http", "proxy_host": "p"})
	if proxied["proxy_port"] != "8080" {
		t.Errorf("HTTP proxy = %v", proxied)
	}
	unproxied, _ := connectors.SFTP.Validate(map[string]string{"host": "h", "username": "u", "proxy_host": "p", "jump_username": "j"})
	if _, ok := unproxied["proxy_host"]; ok {
		t.Errorf("a proxy host without a kind was kept: %v", unproxied)
	}
	if _, ok := unproxied["jump_username"]; ok {
		t.Errorf("a jump username without a jump host was kept: %v", unproxied)
	}
	jumped, err := connectors.SFTP.Validate(map[string]string{"host": "h", "username": "u", "jump_host": "bastion", "jump_username": "j"})
	if err != nil || jumped["jump_port"] != "22" || jumped["jump_auth"] != "password" {
		t.Errorf("jump host = %v, %v", jumped, err)
	}
	if _, err := connectors.SFTP.Validate(map[string]string{"host": "h", "username": "u", "jump_host": "bastion"}); err == nil {
		t.Error("a jump host without a username was accepted")
	}

	ftp, err := connectors.FTP.Validate(map[string]string{"host": "h", "username": "u", "charset": "ISO-8859-1"})
	want := map[string]string{"host": "h", "encryption": "explicit", "port": "21", "username": "u", "auth": "password", "list_timezone": "UTC", "charset": "iso-8859-1"}
	if err != nil || !maps.Equal(ftp, want) {
		t.Errorf("FTP defaults = %v, %v; want %v", ftp, err, want)
	}
	implicit, _ := connectors.FTP.Validate(map[string]string{"host": "h", "username": "u", "encryption": "implicit"})
	if implicit["port"] != "990" {
		t.Errorf("implicit TLS port = %q; want 990", implicit["port"])
	}
	if _, err := connectors.FTP.Validate(map[string]string{"host": "h", "username": "u", "jump_host": "b"}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("FTP with a jump host = %v", err)
	}
	if _, err := connectors.FTP.Validate(map[string]string{"host": "h", "username": "u", "auth": "key"}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("FTP with a key = %v", err)
	}
	if _, err := connectors.SFTP.Validate(map[string]string{"host": "h", "username": "u", "auth": "account"}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("SFTP with an account = %v", err)
	}
}

// TestWebDAV verifies the settings of WebDAV sites (ADR-0026).
//
// Use cases:
//   - a site uses HTTPS on port 443 and the root path by default, and port 80 without HTTPS
//   - a path without its leading slash gets it, and settings without a path are left as they are
//   - a public share signs in anonymously; an account logon is refused
func TestWebDAV(t *testing.T) {
	dav, err := connectors.WebDAV.Validate(map[string]string{"host": "cloud.example.com", "username": "alice"})
	if want := map[string]string{"host": "cloud.example.com", "tls": "true", "port": "443", "path": "/", "auth": "password", "username": "alice"}; err != nil || !maps.Equal(dav, want) {
		t.Errorf("WebDAV defaults = %v, %v; want %v", dav, err, want)
	}
	plain, _ := connectors.WebDAV.Validate(map[string]string{"host": "nas", "username": "u", "tls": "false", "path": "remote.php/dav/files/u"})
	if plain["port"] != "80" || plain["path"] != "/remote.php/dav/files/u" {
		t.Errorf("WebDAV over HTTP = %v", plain)
	}
	if got := connectors.WebDAV.Normalize(map[string]string{"host": "h"}); len(got) != 1 {
		t.Errorf("Normalize(no path) = %v", got)
	}
	public, err := connectors.WebDAV.Validate(map[string]string{"host": "h", "auth": "anonymous"})
	if err != nil || public["username"] != connectors.AnonymousUser {
		t.Errorf("public share = %v, %v", public, err)
	}
	if _, err := connectors.WebDAV.Validate(map[string]string{"host": "h", "username": "u", "auth": "account"}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("WebDAV with an account = %v", err)
	}
}

// TestValidHost verifies which hosts are accepted.
//
// Use cases:
//   - addresses, dotted names, names with underscores and a trailing dot are hosts
//   - empty values, schemes, ports, paths, spaces, bad labels and names over 253 characters are not
func TestValidHost(t *testing.T) {
	for _, host := range []string{"192.0.2.1", "2001:db8::1", "files.example.com", "my_host.local", "example.com."} {
		if !connectors.ValidHost(host) {
			t.Errorf("ValidHost(%q) = false", host)
		}
	}
	for _, host := range []string{"", "sftp://h", "h:22", "h/x", "a b", "-a.com", strings.Repeat("a.", 127) + "com"} {
		if connectors.ValidHost(host) {
			t.Errorf("ValidHost(%q) = true", host)
		}
	}
}
