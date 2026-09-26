package fzimport_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/site"
)

// sample is a site manager as FileZilla writes it, with the odd cases the parser has to survive.
const sample = `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<FileZilla3 version="3.66.0" platform="*nix">
  <Settings><Item name="x">1</Item></Settings>
  <Servers>
    <Server>
      <Host>top.example.com</Host><Port>2121</Port><Protocol>0</Protocol><Logontype>1</Logontype>
      <User>alice</User><Pass encoding="base64">c2VjcmV0</Pass><Name>Top level</Name><Comments>ignored</Comments>
    </Server>
    <Folder expanded="1">Work
      <Server>
        <Host>sftp.example.com</Host><Port>22</Port><Protocol>1</Protocol><Logontype>1</Logontype>
        <User>bob</User><Pass encoding="base64">cMOhc3M=</Pass><Name>SFTP box</Name>
      </Server>
      <Folder>Nested <![CDATA[Deep]]>
        <Server>
          <Host>ftps.example.com</Host><Protocol>4</Protocol><Logontype>1</Logontype><User>carol</User><Pass>plain-pass</Pass>
        </Server>
        <Server>
          <Host>implicit.example.com</Host><Port>990</Port><Protocol>3</Protocol><Logontype>1</Logontype><User>dan</User>
        </Server>
        <Comment>skipped element</Comment>
      </Folder>
    </Folder>
    <Server><Host>anon.example.com</Host><Protocol>6</Protocol><Logontype>0</Logontype><Name>Anonymous</Name></Server>
    <Server><Host>ask.example.com</Host><Protocol>1</Protocol><Logontype>2</Logontype><User>eve</User><Pass encoding="base64">c2VjcmV0</Pass><Name>Asks</Name></Server>
    <Server><Host>bad.example.com</Host><Protocol>0</Protocol><Logontype>1</Logontype><User>frank</User><Pass encoding="base64">%%%</Pass><Name>Bad password</Name><Port>not-a-number</Port></Server>
    <Server><Host>key.example.com</Host><Protocol>1</Protocol><Logontype>5</Logontype><User>gina</User><Name>Key login</Name></Server>
    <Server><Host>web.example.com</Host><Protocol>2</Protocol><Logontype>1</Logontype><User>hank</User><Name>WebDAV</Name></Server>
    <Server><Host>implicit.example.com</Host><Port>9990</Port><Protocol>3</Protocol><Logontype>1</Logontype><User>ivy</User><Name>Odd implicit</Name></Server>
    <Server><Protocol>0</Protocol><User>nobody</User><Name>No host</Name></Server>
  </Servers>
</FileZilla3>`

// TestParse verifies reading a site manager file.
//
// Scenarios:
//   - the sites of the file are read with their folders, protocols, ports, users and passwords, in the order of the file
//   - passwords may be base64 or plain, the ones that cannot be decoded are reported, and asking for the password or anonymous logins keep no password
//   - names default to the host, folder names are trimmed and CDATA is read
//   - sites that cannot be represented are reported with a reason: unsupported protocols, implicit FTPS off port 990, key file logins and a missing host
//   - a port that is not a number is left for the default
func TestParse(t *testing.T) {
	servers, skipped, err := fzimport.Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	byName := map[string]fzimport.Server{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if len(servers) != 7 || len(skipped) != 4 {
		t.Fatalf("read %d servers and %d skipped; want 7 and 4: %+v %+v", len(servers), len(skipped), servers, skipped)
	}
	top := byName["Top level"]
	if top.Host != "top.example.com" || top.Port != 2121 || top.Protocol != site.FTP || top.Username != "alice" || top.Password != "secret" || len(top.Path) != 0 {
		t.Errorf("top level = %+v", top)
	}
	if box := byName["SFTP box"]; box.Protocol != site.SFTP || box.Password != "páss" || len(box.Path) != 1 || box.Path[0] != "Work" {
		t.Errorf("sftp box = %+v", box)
	}
	nested := byName["ftps.example.com"]
	if nested.Protocol != site.FTPS || nested.Password != "plain-pass" || len(nested.Path) != 2 || nested.Path[1] != "Nested Deep" {
		t.Errorf("nested = %+v", nested)
	}
	if implicit := byName["implicit.example.com"]; implicit.Protocol != site.FTPS || implicit.Port != 990 {
		t.Errorf("implicit = %+v", implicit)
	}
	if anon := byName["Anonymous"]; anon.Username != "anonymous" || anon.Protocol != site.FTP || anon.Password != "" {
		t.Errorf("anonymous = %+v", anon)
	}
	if asks := byName["Asks"]; asks.Password != "" || asks.Username != "eve" {
		t.Errorf("asks = %+v", asks)
	}
	if bad := byName["Bad password"]; !bad.PasswordSkipped || bad.Password != "" || bad.Port != 0 {
		t.Errorf("bad password = %+v", bad)
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Name] = s.Reason
	}
	want := map[string]string{"Key login": "key_file_login", "WebDAV": "unsupported_protocol", "Odd implicit": "implicit_ftps_port", "No host": "missing_host"}
	for name, reason := range want {
		if reasons[name] != reason {
			t.Errorf("%q skipped for %q; want %q (all: %v)", name, reasons[name], reason, reasons)
		}
	}
}

// TestParse_Limits verifies how far the reader trusts the file.
//
// Scenarios:
//   - a folder that nests too deeply, more sites than the limit and a file above the size limit fail with ErrTooLarge
//   - long names are cut
func TestParse_Limits(t *testing.T) {
	deep := "<FileZilla3><Servers>" + strings.Repeat("<Folder>x", 14) + strings.Repeat("</Folder>", 14) + "</Servers></FileZilla3>"
	if _, _, err := fzimport.Parse(strings.NewReader(deep)); !errors.Is(err, fzimport.ErrTooLarge) {
		t.Errorf("deep tree = %v; want ErrTooLarge", err)
	}
	var many strings.Builder
	many.WriteString("<FileZilla3><Servers>")
	for i := 0; i <= 2000; i++ {
		fmt.Fprintf(&many, "<Server><Host>h%d.example.com</Host><Name>s%d</Name></Server>", i, i)
	}
	many.WriteString("</Servers></FileZilla3>")
	if _, _, err := fzimport.Parse(strings.NewReader(many.String())); !errors.Is(err, fzimport.ErrTooLarge) {
		t.Errorf("many servers = %v; want ErrTooLarge", err)
	}
	huge := "<FileZilla3><Servers><Server><Host>h.example.com</Host><Name>" + strings.Repeat("a", fzimport.MaxBytes) + "</Name></Server></Servers></FileZilla3>"
	if _, _, err := fzimport.Parse(strings.NewReader(huge)); !errors.Is(err, fzimport.ErrTooLarge) {
		t.Errorf("huge file = %v; want ErrTooLarge", err)
	}
	long := "<FileZilla3><Servers><Folder>" + strings.Repeat("f", 300) + "<Server><Host>h.example.com</Host><Name>" + strings.Repeat("n", 300) + "</Name></Server></Folder></Servers></FileZilla3>"
	servers, _, err := fzimport.Parse(strings.NewReader(long))
	if err != nil || len(servers) != 1 || len(servers[0].Name) != 200 || len(servers[0].Path[0]) != 200 {
		t.Errorf("long names = %+v, %v; want them cut to 200", servers, err)
	}
}

// TestParse_Refusals verifies the files that are not accepted.
//
// Scenarios:
//   - text that is not XML, another root element, an empty file and a truncated file are ErrInvalidFile
//   - document type definitions and entities are refused wherever they appear, so nothing can expand
//   - a file without servers is empty, not an error
func TestParse_Refusals(t *testing.T) {
	bad := map[string]string{
		"not xml":                 "hello",
		"empty":                   "",
		"another root":            "<Other></Other>",
		"truncated":               "<FileZilla3><Servers><Server><Host>x",
		"doctype first":           `<!DOCTYPE FileZilla3 [<!ENTITY a "b">]><FileZilla3><Servers></Servers></FileZilla3>`,
		"doctype in root":         `<FileZilla3><!DOCTYPE x><Servers></Servers></FileZilla3>`,
		"directive in list":       `<FileZilla3><Servers><!ENTITY a "b"></Servers></FileZilla3>`,
		"directive in dir":        `<FileZilla3><Servers><Folder>x<!ENTITY a "b"></Folder></Servers></FileZilla3>`,
		"undefined entity":        `<FileZilla3><Servers><Server><Host>&a;</Host></Server></Servers></FileZilla3>`,
		"broken skipped":          `<FileZilla3><Settings><a></Settings><Servers></Servers></FileZilla3>`,
		"broken server":           `<FileZilla3><Servers><Server><Host>x</Server></Servers></FileZilla3>`,
		"broken in list":          `<FileZilla3><Servers><Other><a></Other></Servers></FileZilla3>`,
		"broken in folder":        `<FileZilla3><Servers><Folder>x<Other><a></Other></Folder></Servers></FileZilla3>`,
		"broken in list end":      `<FileZilla3><Servers></Folder></Servers></FileZilla3>`,
		"broken server in folder": `<FileZilla3><Servers><Folder>x<Server><Host>x</Folder></Servers></FileZilla3>`,
		"eof in folder":           `<FileZilla3><Servers><Folder>x`,
		"eof in list":             `<FileZilla3><Servers>`,
		"eof in root":             `<FileZilla3>`,
	}
	for name, doc := range bad {
		if _, _, err := fzimport.Parse(strings.NewReader(doc)); !errors.Is(err, fzimport.ErrInvalidFile) {
			t.Errorf("%s = %v; want ErrInvalidFile", name, err)
		}
	}
	servers, skipped, err := fzimport.Parse(strings.NewReader("<FileZilla3><Settings/></FileZilla3>"))
	if err != nil || len(servers) != 0 || len(skipped) != 0 {
		t.Errorf("file without servers = %v, %v, %v", servers, skipped, err)
	}
}
