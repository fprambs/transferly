// Package fzimport reads the site manager of FileZilla (sitemanager.xml) and creates the sites and folders it describes. The file
// is untrusted input: it is read with a size limit, without any document type definition or entity, with a limit on how deep and
// how large the tree is, and every value goes through the same validation as a site typed by the user.
package fzimport

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fprambs/transferly/internal/core/site"
)

const (
	// MaxBytes is the largest file that is read.
	MaxBytes = 4 << 20
	// maxServers is how many sites one file may describe.
	maxServers = 2000
	// maxDepth is how deep folders may nest.
	maxDepth = 12
	// maxNameRunes is the longest name kept for a folder or a site; longer ones are cut.
	maxNameRunes = 200
)

// Errors returned by [Parse].
var (
	// ErrInvalidFile means the file is not a FileZilla site manager.
	ErrInvalidFile = errors.New("not a FileZilla site manager file")
	// ErrTooLarge means the file, or the tree it describes, is larger than the limits.
	ErrTooLarge = errors.New("the site manager file is too large")
)

// Server is a site described by the file.
type Server struct {
	// Path is the folders that hold the site, outermost first.
	Path []string
	// Name is the display name of the site.
	Name string
	// Protocol is the transfer protocol.
	Protocol site.Protocol
	// FTPEncryption is how an FTP site protects the connection, as FileZilla's protocol number says; empty for SFTP.
	FTPEncryption site.FTPEncryption
	// Host is the server name or address.
	Host string
	// Port is the server port; zero selects the default of the protocol.
	Port int
	// Username is the account name.
	Username string
	// AuthMethod is how the site logs in, as FileZilla's logon type says: anonymous, asking for the password, asking
	// whatever the server asks (interactive), or with a password.
	AuthMethod site.AuthMethod
	// Password is the account password when the file holds one that could be read.
	Password string
	// PasswordSkipped says the file has a password that was not read, because it cannot be decoded.
	PasswordSkipped bool
	// RemoteDir is the folder the site opens in, from FileZilla's default remote directory; empty for the root or when it cannot be read.
	RemoteDir string
	// Color is the label of the site, from FileZilla's background color; empty for none or an unknown one.
	Color site.Color
	// Account is the account of FileZilla's account logon type, which FTP servers may ask for after the password (4.8e).
	Account string
}

// Skipped is a site of the file that cannot be imported, and why.
type Skipped struct {
	// Path is the folders that hold the site.
	Path []string
	// Name is the name of the site.
	Name string
	// Reason is a stable code: unsupported_protocol, key_file_login or missing_host.
	Reason string
}

// rawServer is a Server element as it is written in the file.
type rawServer struct {
	Host      string `xml:"Host"`
	Port      string `xml:"Port"`
	Protocol  string `xml:"Protocol"`
	Logontype string `xml:"Logontype"`
	User      string `xml:"User"`
	Pass      struct {
		Encoding string `xml:"encoding,attr"`
		Value    string `xml:",chardata"`
	} `xml:"Pass"`
	Name      string `xml:"Name"`
	RemoteDir string `xml:"RemoteDir"`
	Account   string `xml:"Account"`
	Colour    string `xml:"Colour"`
}

// parser walks the tokens of the file.
type parser struct {
	dec     *xml.Decoder
	servers []Server
	skipped []Skipped
	count   int
}

// Parse reads a site manager file. Sites it cannot represent are reported as skipped rather than failing the file.
func Parse(r io.Reader) ([]Server, []Skipped, error) {
	limited := &io.LimitedReader{R: r, N: MaxBytes + 1}
	p := &parser{dec: xml.NewDecoder(limited)}
	p.dec.Strict = true
	if err := p.document(); err != nil {
		if limited.N <= 0 {
			return nil, nil, ErrTooLarge
		}
		return nil, nil, err
	}
	return p.servers, p.skipped, nil
}

// document reads the root element and the servers below it.
func (p *parser) document() error {
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: document type definitions are not accepted", ErrInvalidFile)
		case xml.StartElement:
			if t.Name.Local != "FileZilla3" {
				return fmt.Errorf("%w: the root element is %s", ErrInvalidFile, t.Name.Local)
			}
			return p.root()
		}
	}
}

// root reads the children of the root element and looks for the list of servers.
func (p *parser) root() error {
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: document type definitions are not accepted", ErrInvalidFile)
		case xml.StartElement:
			if t.Name.Local != "Servers" {
				if err := p.dec.Skip(); err != nil {
					return fmt.Errorf("%w: %w", ErrInvalidFile, err)
				}
				continue
			}
			return p.folder(nil)
		case xml.EndElement:
			return nil
		}
	}
}

// folder reads the children of the list of servers until the end of the element.
func (p *parser) folder(path []string) error {
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: document type definitions are not accepted", ErrInvalidFile)
		case xml.EndElement:
			return nil
		case xml.StartElement:
			switch t.Name.Local {
			case "Folder":
				if err := p.subfolder(path, 0); err != nil {
					return err
				}
			case "Server":
				if err := p.server(t, path); err != nil {
					return err
				}
			default:
				if err := p.dec.Skip(); err != nil {
					return fmt.Errorf("%w: %w", ErrInvalidFile, err)
				}
			}
		}
	}
}

// subfolder reads a folder: its name is the text before its first child.
func (p *parser) subfolder(path []string, depth int) error {
	if depth >= maxDepth {
		return ErrTooLarge
	}
	var name strings.Builder
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidFile, err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			name.Write(t)
		case xml.Directive:
			return fmt.Errorf("%w: document type definitions are not accepted", ErrInvalidFile)
		case xml.EndElement:
			return nil
		case xml.StartElement:
			inner := append(append([]string(nil), path...), clean(name.String()))
			if t.Name.Local == "Server" {
				if err := p.server(t, inner); err != nil {
					return err
				}
				continue
			}
			if t.Name.Local == "Folder" {
				if err := p.subfolder(inner, depth+1); err != nil {
					return err
				}
				continue
			}
			if err := p.dec.Skip(); err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidFile, err)
			}
		}
	}
}

// clean trims a name and cuts it to a reasonable length.
func clean(name string) string {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxNameRunes {
		name = string([]rune(name)[:maxNameRunes])
	}
	return name
}

// server reads one Server element and keeps it or reports why it was skipped.
func (p *parser) server(start xml.StartElement, path []string) error {
	if p.count++; p.count > maxServers {
		return ErrTooLarge
	}
	var raw rawServer
	if err := p.dec.DecodeElement(&raw, &start); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFile, err)
	}
	name := clean(raw.Name)
	if name == "" {
		name = clean(raw.Host)
	}
	if server, reason := convert(raw, name, path); reason != "" {
		p.skipped = append(p.skipped, Skipped{Path: path, Name: name, Reason: reason})
	} else {
		p.servers = append(p.servers, server)
	}
	return nil
}

// fileZillaProtocols maps the protocol numbers of FileZilla to a protocol and, for FTP, its encryption: 0 is FileZilla's
// default "explicit TLS if available", 3 implicit TLS, 4 required explicit TLS and 6 plain FTP.
var fileZillaProtocols = map[string]struct {
	protocol   site.Protocol
	encryption site.FTPEncryption
}{
	"":  {site.FTP, site.EncryptionOptional},
	"0": {site.FTP, site.EncryptionOptional},
	"1": {site.SFTP, ""},
	"3": {site.FTP, site.EncryptionImplicit},
	"4": {site.FTP, site.EncryptionExplicit},
	"6": {site.FTP, site.EncryptionPlain},
}

// fileZillaColours maps the background colors of FileZilla's site manager, by number, to the labels of a site; cyan and magenta
// become the nearest labels, teal and pink.
var fileZillaColours = map[string]site.Color{"1": "red", "2": "green", "3": "blue", "4": "yellow", "5": "teal", "6": "pink", "7": "orange"}

// convert maps the settings of FileZilla to the ones of a site, or gives the reason why that cannot be done.
func convert(raw rawServer, name string, path []string) (Server, string) {
	server := Server{
		Path: path, Name: name, Host: strings.TrimSpace(raw.Host), Username: strings.TrimSpace(raw.User), RemoteDir: remoteDir(raw.RemoteDir),
		Color: fileZillaColours[strings.TrimSpace(raw.Colour)],
	}
	if server.Host == "" {
		return Server{}, "missing_host"
	}
	if port := strings.TrimSpace(raw.Port); port != "" {
		if n, err := strconv.Atoi(port); err == nil {
			server.Port = n
		}
	}
	known, ok := fileZillaProtocols[strings.TrimSpace(raw.Protocol)]
	if !ok {
		return Server{}, "unsupported_protocol"
	}
	server.Protocol, server.FTPEncryption = known.protocol, known.encryption
	switch strings.TrimSpace(raw.Logontype) {
	case "0":
		server.Username, server.AuthMethod = site.AnonymousUser, site.AuthAnonymous
	case "5":
		return Server{}, "key_file_login"
	case "2":
		server.AuthMethod = site.AuthAsk
	case "3":
		server.AuthMethod = site.AuthInteractive
	case "4":
		server.AuthMethod, server.Account = site.AuthAccount, strings.TrimSpace(raw.Account)
		readPassword(&server, raw)
	default:
		server.AuthMethod = site.AuthPassword
		readPassword(&server, raw)
	}
	return server, ""
}

// readPassword decodes the password of a server.
func readPassword(server *Server, raw rawServer) {
	value := strings.TrimSpace(raw.Pass.Value)
	if value == "" {
		return
	}
	if raw.Pass.Encoding != "base64" {
		server.Password = value
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || !utf8.Valid(decoded) {
		server.PasswordSkipped = true
		return
	}
	server.Password = string(decoded)
}

// remoteDir reads FileZilla's encoding of a server path, such as "1 0 4 home 4 user" for /home/user: the server type, the length of a
// prefix and the prefix, then the length of each folder and the folder, so a name may hold spaces. Malformed text reads as the root.
func remoteDir(encoded string) string {
	rest := strings.TrimSpace(encoded)
	next := func() (string, bool) {
		size, tail, found := strings.Cut(rest, " ")
		n, err := strconv.Atoi(size)
		if !found || err != nil || n < 0 || n > len(tail) {
			return "", false
		}
		rest = strings.TrimPrefix(tail[n:], " ")
		return tail[:n], true
	}
	if _, tail, found := strings.Cut(rest, " "); found {
		rest = tail
	} else {
		return ""
	}
	if _, ok := next(); !ok {
		return ""
	}
	var folders []string
	for rest != "" {
		folder, ok := next()
		if !ok {
			return ""
		}
		folders = append(folders, folder)
	}
	if len(folders) == 0 {
		return ""
	}
	return "/" + strings.Join(folders, "/")
}
