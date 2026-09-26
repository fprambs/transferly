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

	"github.com/fprambs/sentinel/internal/core/site"
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
	// implicitFTPSPort is the only port on which implicit FTPS is supported.
	implicitFTPSPort = 990
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
	// Host is the server name or address.
	Host string
	// Port is the server port; zero selects the default of the protocol.
	Port int
	// Username is the account name.
	Username string
	// Password is the account password when the file holds one that could be read.
	Password string
	// PasswordSkipped says the file has a password that was not read, because it cannot be decoded.
	PasswordSkipped bool
}

// Skipped is a site of the file that cannot be imported, and why.
type Skipped struct {
	// Path is the folders that hold the site.
	Path []string
	// Name is the name of the site.
	Name string
	// Reason is a stable code: unsupported_protocol, implicit_ftps_port, key_file_login or missing_host.
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
	Name string `xml:"Name"`
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

// convert maps the settings of FileZilla to the ones of a site, or gives the reason why that cannot be done.
func convert(raw rawServer, name string, path []string) (Server, string) {
	server := Server{Path: path, Name: name, Host: strings.TrimSpace(raw.Host), Username: strings.TrimSpace(raw.User)}
	if server.Host == "" {
		return Server{}, "missing_host"
	}
	if port := strings.TrimSpace(raw.Port); port != "" {
		if n, err := strconv.Atoi(port); err == nil {
			server.Port = n
		}
	}
	switch strings.TrimSpace(raw.Protocol) {
	case "", "0", "6":
		server.Protocol = site.FTP
	case "1":
		server.Protocol = site.SFTP
	case "4":
		server.Protocol = site.FTPS
	case "3":
		if server.Port != implicitFTPSPort {
			return Server{}, "implicit_ftps_port"
		}
		server.Protocol = site.FTPS
	default:
		return Server{}, "unsupported_protocol"
	}
	switch strings.TrimSpace(raw.Logontype) {
	case "0":
		server.Username = "anonymous"
	case "5":
		return Server{}, "key_file_login"
	case "2", "3":
	default:
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
