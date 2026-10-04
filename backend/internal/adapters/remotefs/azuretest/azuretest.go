// Package azuretest is a fake of Azure Storage for tests (ADR-0025): the subset of the Blob and Files REST APIs the adapters
// use, in memory, with one container or one share, path-style URLs that take the account in the path, as emulators do, and
// failures that can be injected. Every request must be signed with the shared key of the account, carry a SAS token, or read
// a public container.
package azuretest

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
)

// The credentials and the resources of every fake.
const (
	Account   = "transferly"
	Container = "files"
	Share     = "share"
	// SAS is a token the fake accepts in place of the shared key.
	SAS = "sv=2023-11-03&ss=bf&srt=sco&sp=rwdlc&sig=fake"
)

// Key is the account key of the fake, in base64.
var Key = base64.StdEncoding.EncodeToString([]byte("transferly-azure-test-account-key"))

// lastWriteFormat is how Azure Files writes times.
const lastWriteFormat = "2006-01-02T15:04:05.0000000Z"

// Fault is a failure the fake answers instead of performing a request: an HTTP status and the code of Azure Storage.
type Fault struct {
	Status int
	Code   string
}

// Options configure a fake.
type Options struct {
	// Fault returns the failure to answer a request with, or nil to perform it.
	Fault func(r *http.Request) *Fault
	// Public lets anonymous requests read the container, as a container of public access does.
	Public bool
	// PendingCopies is how many times a copy reports itself pending before it reports success.
	PendingCopies int
}

// object is a blob, or a file or a directory of a share.
type object struct {
	data     []byte
	dir      bool
	modified time.Time
}

// Server is a running fake of the Blob or the Files service.
type Server struct {
	// URL is where it listens; the endpoint of the account is URL followed by the account.
	URL string

	opt     Options
	handler http.HandlerFunc
	mu      sync.Mutex
	objects map[string]*object
	staged  map[string]map[string][]byte
	pending map[string]int
	clock   time.Time
}

// Endpoint returns the endpoint of the account of the fake.
func (s *Server) Endpoint() string { return s.URL + "/" + Account }

// newServer returns a fake that serves with handler, not listening yet.
func newServer(opt Options, handler func(s *Server, w http.ResponseWriter, r *http.Request)) *Server {
	s := &Server{opt: opt, objects: map[string]*object{}, staged: map[string]map[string][]byte{}, pending: map[string]int{}, clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s.handler = s.guard(func(w http.ResponseWriter, r *http.Request) { handler(s, w, r) })
	return s
}

// NewFiles returns a fake of the Files service with the share [Share], which [Server.Handler] serves.
func NewFiles(opt Options) *Server { return newServer(opt, (*Server).files) }

// Handler serves the fake.
func (s *Server) Handler() http.Handler { return s.handler }

// start runs a fake until the test ends.
func start(t testing.TB, s *Server) *Server {
	t.Helper()
	server := httptest.NewServer(s.handler)
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

// StartBlob runs a fake of the Blob service with the container [Container] until the test ends.
func StartBlob(t testing.TB, opt Options) *Server { return start(t, newServer(opt, (*Server).blob)) }

// StartFiles runs a fake of the Files service with the share [Share] until the test ends.
func StartFiles(t testing.TB, opt Options) *Server { return start(t, NewFiles(opt)) }

// Put stores a blob or a file directly, as another program would, at a path below the container or the share.
func (s *Server) Put(name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[strings.TrimPrefix(name, "/")] = &object{data: data, modified: s.now()}
}

// PutDirectory makes a directory of the share directly, at a path below it.
func (s *Server) PutDirectory(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[strings.TrimPrefix(name, "/")] = &object{dir: true, modified: s.now()}
}

// now returns a time that grows with each call.
func (s *Server) now() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

// fail writes an error of Azure Storage: the code in its header, and in its body unless the request is a HEAD.
func fail(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("x-ms-error-code", code)
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
	}
}

// guard answers injected faults, checks that the request is for the account and authorized, and passes it on with its
// resource, the path after the account, as the path of the request.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.opt.Fault != nil {
			if f := s.opt.Fault(r); f != nil {
				fail(w, r, f.Status, f.Code)
				return
			}
		}
		resource, ok := strings.CutPrefix(r.URL.EscapedPath(), "/"+Account)
		if !ok {
			fail(w, r, http.StatusBadRequest, "InvalidUri")
			return
		}
		key, _ := base64.StdEncoding.DecodeString(Key)
		signed := r.Header.Get("Authorization") == "SharedKey "+Account+":"+azure.Sign(r, Account, key)
		anonymousRead := s.opt.Public && (r.Method == http.MethodGet || r.Method == http.MethodHead)
		if !signed && r.URL.Query().Get("sig") == "" && !anonymousRead {
			fail(w, r, http.StatusForbidden, "AuthenticationFailed")
			return
		}
		path, _ := url.PathUnescape(resource)
		r.URL.Path = path
		next(w, r)
	}
}

// listing is the answer of a listing of blobs or of the entries of a directory.
type listing struct {
	XMLName  xml.Name `xml:"EnumerationResults"`
	Blobs    *entries `xml:"Blobs,omitempty"`
	Entries  *entries `xml:"Entries,omitempty"`
	NextMark string   `xml:"NextMarker"`
}

// entries are the items of a listing.
type entries struct {
	Blob      []item `xml:"Blob,omitempty"`
	Prefix    []item `xml:"BlobPrefix,omitempty"`
	File      []item `xml:"File,omitempty"`
	Directory []item `xml:"Directory,omitempty"`
}

// item is one blob, prefix, file or directory of a listing.
type item struct {
	Name       string      `xml:"Name"`
	Properties *properties `xml:"Properties,omitempty"`
}

// properties are the size and the times of an item.
type properties struct {
	LastModified  string `xml:"Last-Modified,omitempty"`
	ContentLength int    `xml:"Content-Length"`
	LastWriteTime string `xml:"LastWriteTime,omitempty"`
}

// reply writes a listing as XML.
func reply(w http.ResponseWriter, l listing) {
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(l)
}

// rangeOf reads the first byte of an x-ms-range header, or zero; last is -1 when the range has no end.
func rangeOf(r *http.Request) (first, last int) {
	m := regexp.MustCompile(`^bytes=(\d+)-(\d*)$`).FindStringSubmatch(r.Header.Get("x-ms-range"))
	if m == nil {
		return 0, -1
	}
	first, _ = strconv.Atoi(m[1])
	last = -1
	if m[2] != "" {
		last, _ = strconv.Atoi(m[2])
	}
	return first, last
}

// content answers the content of an object from the byte its range starts at.
func content(w http.ResponseWriter, r *http.Request, o *object) {
	first, _ := rangeOf(r)
	if first > 0 && first >= len(o.data) {
		fail(w, r, http.StatusRequestedRangeNotSatisfiable, "InvalidRange")
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(o.data)-first))
	w.Header().Set("Last-Modified", o.modified.Format(http.TimeFormat))
	w.Header().Set("x-ms-file-last-write-time", o.modified.Format(lastWriteFormat))
	if first > 0 {
		w.WriteHeader(http.StatusPartialContent)
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(o.data[first:])
	}
}

// blob serves the Blob service.
func (s *Server) blob(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	container, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if container != Container {
		fail(w, r, http.StatusNotFound, "ContainerNotFound")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case name == "" && q.Get("comp") == "list":
		s.listBlobs(w, q)
	case name == "" && q.Get("restype") == "container":
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		o, ok := s.objects[name]
		if !ok {
			fail(w, r, http.StatusNotFound, "BlobNotFound")
			return
		}
		if s.pending[name] > 0 {
			s.pending[name]--
			w.Header().Set("x-ms-copy-status", "pending")
		} else {
			w.Header().Set("x-ms-copy-status", "success")
		}
		content(w, r, o)
	case r.Method == http.MethodDelete:
		if _, ok := s.objects[name]; !ok {
			fail(w, r, http.StatusNotFound, "BlobNotFound")
			return
		}
		delete(s.objects, name)
		w.WriteHeader(http.StatusAccepted)
	case q.Get("comp") == "block":
		body, _ := io.ReadAll(r.Body)
		if s.staged[name] == nil {
			s.staged[name] = map[string][]byte{}
		}
		s.staged[name][q.Get("blockid")] = body
		w.WriteHeader(http.StatusCreated)
	case q.Get("comp") == "blocklist":
		var list struct {
			Latest []string `xml:"Latest"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = xml.Unmarshal(body, &list)
		var data bytes.Buffer
		for _, id := range list.Latest {
			data.Write(s.staged[name][id])
		}
		delete(s.staged, name)
		s.write(w, r, name, data.Bytes())
	case r.Header.Get("x-ms-copy-source") != "":
		source, _ := url.Parse(r.Header.Get("x-ms-copy-source"))
		from := strings.TrimPrefix(source.Path, "/"+Account+"/"+Container+"/")
		o, ok := s.objects[from]
		if !ok {
			fail(w, r, http.StatusNotFound, "BlobNotFound")
			return
		}
		s.objects[name] = &object{data: bytes.Clone(o.data), modified: s.now()}
		status := "success"
		if s.opt.PendingCopies > 0 {
			s.pending[name], status = s.opt.PendingCopies, "pending"
		}
		w.Header().Set("x-ms-copy-status", status)
		w.WriteHeader(http.StatusAccepted)
	case r.Header.Get("x-ms-blob-type") == "BlockBlob":
		body, _ := io.ReadAll(r.Body)
		s.write(w, r, name, body)
	default:
		fail(w, r, http.StatusBadRequest, "UnsupportedHttpVerb")
	}
}

// write stores a blob, unless the request asks for it not to exist and it does.
func (s *Server) write(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if _, ok := s.objects[name]; ok && r.Header.Get("If-None-Match") == "*" {
		fail(w, r, http.StatusPreconditionFailed, "ConditionNotMet")
		return
	}
	s.objects[name] = &object{data: data, modified: s.now()}
	w.WriteHeader(http.StatusCreated)
}

// listBlobs answers the blobs of a prefix, one level when a delimiter is given, in pages; a page that ends on a folded prefix
// is followed by the names after everything under it, as Azure does.
func (s *Server) listBlobs(w http.ResponseWriter, q url.Values) {
	prefix, delimiter, marker := q.Get("prefix"), q.Get("delimiter"), q.Get("marker")
	size, _ := strconv.Atoi(q.Get("maxresults"))
	var names []string
	for name := range s.objects {
		skipped := delimiter != "" && strings.HasSuffix(marker, delimiter) && strings.HasPrefix(name, marker)
		if strings.HasPrefix(name, prefix) && name > marker && !skipped {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	out := listing{Blobs: &entries{}}
	seen := map[string]bool{}
	count := 0
	for _, name := range names {
		if size > 0 && count == size {
			out.NextMark = out.lastName()
			break
		}
		rest := strings.TrimPrefix(name, prefix)
		if i := strings.Index(rest, delimiter); delimiter != "" && i >= 0 {
			folded := prefix + rest[:i+1]
			if !seen[folded] {
				seen[folded] = true
				out.Blobs.Prefix = append(out.Blobs.Prefix, item{Name: folded})
				count++
			}
			continue
		}
		o := s.objects[name]
		out.Blobs.Blob = append(out.Blobs.Blob, item{Name: name, Properties: &properties{LastModified: o.modified.Format(http.TimeFormat), ContentLength: len(o.data)}})
		count++
	}
	reply(w, out)
}

// lastName is the name of the last item of a page of blobs, from which the next page starts.
func (l listing) lastName() string {
	last := ""
	for _, i := range append(slices.Clone(l.Blobs.Blob), l.Blobs.Prefix...) {
		last = max(last, i.Name)
	}
	return last
}

// files serves the Files service.
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	share, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if share != Share {
		fail(w, r, http.StatusNotFound, "ShareNotFound")
		return
	}
	name = strings.TrimSuffix(name, "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	o, exists := s.objects[name]
	if name == "" {
		o, exists = &object{dir: true}, true
	}
	parent, parentOK := s.objects[pathDir(name)]
	parentIsDir := pathDir(name) == "" || (parentOK && parent.dir)
	directory := q.Get("restype") == "directory"
	switch {
	case q.Get("restype") == "share":
	case directory && q.Get("comp") == "list":
		if !exists || !o.dir {
			fail(w, r, http.StatusNotFound, "ResourceNotFound")
			return
		}
		s.listDirectory(w, name, q)
	case q.Get("comp") == "rename":
		s.rename(w, r, name, exists, parentIsDir)
	case directory && r.Method == http.MethodGet:
		if !exists || !o.dir {
			fail(w, r, http.StatusNotFound, "ResourceNotFound")
			return
		}
	case directory && r.Method == http.MethodPut:
		switch {
		case exists:
			fail(w, r, http.StatusConflict, "ResourceAlreadyExists")
		case !parentIsDir:
			fail(w, r, http.StatusNotFound, "ParentNotFound")
		default:
			s.objects[name] = &object{dir: true, modified: s.now()}
			w.WriteHeader(http.StatusCreated)
		}
	case directory && r.Method == http.MethodDelete:
		switch {
		case !exists || !o.dir:
			fail(w, r, http.StatusNotFound, "ResourceNotFound")
		case s.hasChildren(name):
			fail(w, r, http.StatusConflict, "DirectoryNotEmpty")
		default:
			delete(s.objects, name)
			w.WriteHeader(http.StatusAccepted)
		}
	case r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodDelete || q.Get("comp") != "":
		if !exists || o.dir {
			fail(w, r, http.StatusNotFound, "ResourceNotFound")
			return
		}
		s.fileOperation(w, r, name, o)
	case r.Header.Get("x-ms-type") == "file":
		if !parentIsDir || (exists && o.dir) {
			fail(w, r, http.StatusNotFound, "ParentNotFound")
			return
		}
		size, _ := strconv.Atoi(r.Header.Get("x-ms-content-length"))
		s.objects[name] = &object{data: make([]byte, size), modified: s.now()}
		w.WriteHeader(http.StatusCreated)
	default:
		fail(w, r, http.StatusBadRequest, "UnsupportedHttpVerb")
	}
}

// fileOperation reads, deletes, resizes, retimes or writes a range of an existing file.
func (s *Server) fileOperation(w http.ResponseWriter, r *http.Request, name string, o *object) {
	switch {
	case r.Method == http.MethodDelete:
		delete(s.objects, name)
		w.WriteHeader(http.StatusAccepted)
	case r.URL.Query().Get("comp") == "properties":
		if size := r.Header.Get("x-ms-content-length"); size != "" {
			n, _ := strconv.Atoi(size)
			o.data = append(o.data, make([]byte, max(0, n-len(o.data)))...)[:n]
		}
		o.modified = s.now()
		if at, err := time.Parse(lastWriteFormat, r.Header.Get("x-ms-file-last-write-time")); err == nil {
			o.modified = at
		}
	case r.URL.Query().Get("comp") == "range":
		first, last := rangeOf(r)
		body, _ := io.ReadAll(r.Body)
		if last >= len(o.data) || last-first+1 != len(body) {
			fail(w, r, http.StatusRequestedRangeNotSatisfiable, "InvalidRange")
			return
		}
		copy(o.data[first:], body)
		o.modified = s.now()
		w.WriteHeader(http.StatusCreated)
	default:
		content(w, r, o)
	}
}

// rename moves a file or a directory, with everything below it, to name, from the path its rename source names.
func (s *Server) rename(w http.ResponseWriter, r *http.Request, name string, exists, parentIsDir bool) {
	source, _ := url.Parse(r.Header.Get("x-ms-file-rename-source"))
	from := strings.TrimSuffix(strings.TrimPrefix(source.Path, "/"+Account+"/"+Share+"/"), "/")
	moved, ok := s.objects[from]
	switch {
	case !ok:
		fail(w, r, http.StatusNotFound, "ResourceNotFound")
		return
	case exists:
		fail(w, r, http.StatusConflict, "ResourceAlreadyExists")
		return
	case !parentIsDir:
		fail(w, r, http.StatusNotFound, "ParentNotFound")
		return
	}
	for key, o := range s.objects {
		if strings.HasPrefix(key, from+"/") {
			delete(s.objects, key)
			s.objects[name+strings.TrimPrefix(key, from)] = o
		}
	}
	delete(s.objects, from)
	s.objects[name] = moved
}

// hasChildren reports whether a directory holds anything.
func (s *Server) hasChildren(dir string) bool {
	for key := range s.objects {
		if pathDir(key) == dir {
			return true
		}
	}
	return false
}

// listDirectory answers the files and directories of a directory, with their times, in pages.
func (s *Server) listDirectory(w http.ResponseWriter, dir string, q url.Values) {
	size, _ := strconv.Atoi(q.Get("maxresults"))
	var names []string
	for key := range s.objects {
		if pathDir(key) == dir && key > q.Get("marker") {
			names = append(names, key)
		}
	}
	slices.Sort(names)
	out := listing{Entries: &entries{}}
	for i, key := range names {
		if size > 0 && i == size {
			out.NextMark = names[i-1]
			break
		}
		o := s.objects[key]
		entry := item{Name: key[strings.LastIndex(key, "/")+1:], Properties: &properties{ContentLength: len(o.data), LastWriteTime: o.modified.Format(lastWriteFormat)}}
		if o.dir {
			out.Entries.Directory = append(out.Entries.Directory, entry)
		} else {
			out.Entries.File = append(out.Entries.File, entry)
		}
	}
	reply(w, out)
}

// pathDir is the directory of a path below the share, empty for the root.
func pathDir(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[:i]
	}
	return ""
}
