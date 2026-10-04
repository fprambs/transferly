// Package googletest is a fake of Google for tests (ADR-0024): the OAuth 2.0 endpoints, with a consent page that agrees at
// once, and the subset of the Drive v3 REST API the Drive adapter uses, kept in memory.
package googletest

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
)

// The credentials of the fake: the OAuth client it accepts and the account that signs in.
const (
	ClientID     = "transferly-test.apps.googleusercontent.com"
	ClientSecret = "transferly-test-secret"
	Email        = "tester@example.com"
)

// Mime types of Drive.
const (
	FolderMime   = "application/vnd.google-apps.folder"
	DocumentMime = "application/vnd.google-apps.document"
)

// Fault is a failure the fake answers instead of performing a request: an HTTP status and the reason Drive gives.
type Fault struct {
	// Status is the HTTP status of the answer.
	Status int
	// Reason is the reason of the error, such as notFound or insufficientPermissions.
	Reason string
}

// Options configure a fake.
type Options struct {
	// Fault returns the failure to answer a request with, or nil to perform it.
	Fault func(r *http.Request) *Fault
	// TokenLifetime is how long access tokens last; zero means an hour.
	TokenLifetime time.Duration
	// Deny makes the consent page refuse, as a person who declines does.
	Deny bool
	// PageSize caps the files of a page of a listing, below what the request asks; zero means no cap.
	PageSize int
}

// File is a file or a folder of the fake.
type File struct {
	ID, Name, MimeType string
	Parents            []string
	Content            []byte
	Created, Modified  time.Time
	Trashed            bool
}

// Server is a running fake.
type Server struct {
	// URL is where it listens; it serves the OAuth endpoints and the API under the same origin.
	URL string
	// Root is the ID of My Drive.
	Root string

	opt     Options
	mu      sync.Mutex
	files   map[string]*File
	drives  map[string]string
	codes   map[string]grant
	access  map[string]time.Time
	refresh map[string]bool
	uploads map[string]*upload
	clock   time.Time
}

// grant is an authorization code waiting to be exchanged.
type grant struct {
	challenge, redirect string
}

// upload is a resumable upload session.
type upload struct {
	file   *File
	update bool
	data   bytes.Buffer
}

// New returns a fake that is not listening yet, with an empty My Drive; [Server.Handler] serves it.
func New(opt Options) *Server {
	if opt.TokenLifetime == 0 {
		opt.TokenLifetime = time.Hour
	}
	s := &Server{
		opt: opt, files: map[string]*File{}, drives: map[string]string{}, codes: map[string]grant{}, access: map[string]time.Time{},
		refresh: map[string]bool{}, uploads: map[string]*upload{}, clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	s.Root = s.add(&File{Name: "My Drive", MimeType: FolderMime})
	return s
}

// Start runs a fake until the test ends.
func Start(t testing.TB, opt Options) *Server {
	t.Helper()
	s := New(opt)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

// id returns a new random identifier.
func id() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// now returns a time that grows with each call, so files sort by when they were made.
func (s *Server) now() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

// add stores a file and returns its ID.
func (s *Server) add(f *File) string {
	f.ID = id()
	f.Created = s.now()
	f.Modified = f.Created
	s.files[f.ID] = f
	return f.ID
}

// Put stores a file directly, as another program filling the drive would, and returns its ID.
func (s *Server) Put(parent string, f File) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.Parents = []string{parent}
	return s.add(&f)
}

// AddSharedDrive makes a shared drive of a name and returns its ID, which is also the ID of its root folder.
func (s *Server) AddSharedDrive(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.add(&File{Name: name, MimeType: FolderMime})
	s.drives[root] = name
	return root
}

// Get returns a copy of a file by ID.
func (s *Server) Get(fileID string) (File, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[fileID]
	if !ok {
		return File{}, false
	}
	return *f, true
}

// RefreshToken issues a refresh token directly, as if the account had signed in and consented.
func (s *Server) RefreshToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	refresh := "refresh-" + id()
	s.refresh[refresh] = true
	return refresh
}

// RevokeTokens forgets every refresh token, as revoking the access of Transferly in the Google account does.
func (s *Server) RevokeTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh = map[string]bool{}
	s.access = map[string]time.Time{}
}

// Handler serves the fake.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /o/oauth2/auth", s.consent)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("GET /oauth2/v3/userinfo", s.authorized(s.userinfo))
	mux.HandleFunc("GET /drive/v3/about", s.authorized(s.about))
	mux.HandleFunc("GET /drive/v3/drives", s.authorized(s.listDrives))
	mux.HandleFunc("GET /drive/v3/files", s.authorized(s.list))
	mux.HandleFunc("POST /drive/v3/files", s.authorized(s.createFolder))
	mux.HandleFunc("GET /drive/v3/files/{id}", s.authorized(s.get))
	mux.HandleFunc("PATCH /drive/v3/files/{id}", s.authorized(s.update))
	mux.HandleFunc("GET /drive/v3/files/{id}/export", s.authorized(s.export))
	mux.HandleFunc("POST /upload/drive/v3/files", s.authorized(s.startUpload))
	mux.HandleFunc("PATCH /upload/drive/v3/files/{id}", s.authorized(s.startUpload))
	mux.HandleFunc("PUT /upload/session/{id}", s.authorized(s.uploadChunk))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opt.Fault != nil {
			if fault := s.opt.Fault(r); fault != nil {
				fail(w, fault.Status, fault.Reason)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// fail writes an error of the Drive API.
func fail(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "message": reason, "errors": []map[string]string{{"reason": reason}}}})
}

// reply writes v as JSON.
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// consent answers the authorization page: it agrees at once and sends the browser back with a code, or with access_denied.
func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != ClientID || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	answer := url.Values{"state": {q.Get("state")}}
	if s.opt.Deny {
		answer.Set("error", "access_denied")
	} else {
		code := id()
		s.mu.Lock()
		s.codes[code] = grant{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
		s.mu.Unlock()
		answer.Set("code", code)
	}
	back.RawQuery = answer.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// token answers the token endpoint: it exchanges an authorization code, checking its PKCE verifier, or refreshes an access token.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	user, password, basic := r.BasicAuth()
	if !basic {
		user, password = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	if user != ClientID || password != ClientSecret {
		oauthError(w, "invalid_client")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answer := map[string]any{"token_type": "Bearer", "expires_in": int(s.opt.TokenLifetime.Seconds())}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		g, ok := s.codes[r.Form.Get("code")]
		delete(s.codes, r.Form.Get("code"))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || g.redirect != r.Form.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			oauthError(w, "invalid_grant")
			return
		}
		refresh := "refresh-" + id()
		s.refresh[refresh] = true
		answer["refresh_token"] = refresh
	case "refresh_token":
		if !s.refresh[r.Form.Get("refresh_token")] {
			oauthError(w, "invalid_grant")
			return
		}
	default:
		oauthError(w, "unsupported_grant_type")
		return
	}
	access := "access-" + id()
	s.access[access] = time.Now().Add(s.opt.TokenLifetime)
	answer["access_token"] = access
	reply(w, answer)
}

// oauthError writes an error of the token endpoint.
func oauthError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// authorized serves next only for a request with an access token the fake issued that has not expired.
func (s *Server) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		expiry, ok := s.access[token]
		s.mu.Unlock()
		if !ok || time.Now().After(expiry) {
			fail(w, http.StatusUnauthorized, "authError")
			return
		}
		next(w, r)
	}
}

// userinfo answers the email of the account.
func (s *Server) userinfo(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]any{"email": Email, "email_verified": true})
}

// about answers who the account is.
func (s *Server) about(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]any{"user": map[string]string{"emailAddress": Email}})
}

// listDrives answers the shared drives.
func (s *Server) listDrives(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	drives := []map[string]string{}
	for driveID, name := range s.drives {
		drives = append(drives, map[string]string{"id": driveID, "name": name})
	}
	slices.SortFunc(drives, func(a, b map[string]string) int { return strings.Compare(a["name"], b["name"]) })
	reply(w, map[string]any{"drives": drives})
}

// resource is the public form of a file, with the fields the adapter reads.
func resource(f *File) map[string]any {
	out := map[string]any{
		"id": f.ID, "name": f.Name, "mimeType": f.MimeType, "parents": f.Parents, "trashed": f.Trashed,
		"createdTime": f.Created.Format(time.RFC3339Nano), "modifiedTime": f.Modified.Format(time.RFC3339Nano),
	}
	if f.MimeType != FolderMime && !strings.HasPrefix(f.MimeType, "application/vnd.google-apps.") {
		out["size"] = strconv.Itoa(len(f.Content))
	}
	return out
}

// The terms of the queries the adapter sends.
var (
	parentTerm = regexp.MustCompile(`^'((?:[^'\\]|\\.)*)' in parents$`)
	nameTerm   = regexp.MustCompile(`^name = '((?:[^'\\]|\\.)*)'$`)
)

// unquote reverses the escaping of a value in a query.
func unquote(v string) string {
	return strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(v)
}

// matcher turns a query into a filter of files, or fails for a term the fake does not know.
func matcher(q string) (func(f *File) bool, error) {
	var tests []func(f *File) bool
	for _, term := range strings.Split(q, " and ") {
		term = strings.TrimSpace(term)
		if m := parentTerm.FindStringSubmatch(term); m != nil {
			parent := unquote(m[1])
			tests = append(tests, func(f *File) bool { return slices.Contains(f.Parents, parent) })
			continue
		}
		if term == "trashed = false" {
			tests = append(tests, func(f *File) bool { return !f.Trashed })
			continue
		}
		var names []string
		for _, alternative := range strings.Split(strings.Trim(term, "()"), " or ") {
			m := nameTerm.FindStringSubmatch(strings.TrimSpace(alternative))
			if m == nil {
				return nil, fmt.Errorf("unsupported query term %q", term)
			}
			names = append(names, unquote(m[1]))
		}
		tests = append(tests, func(f *File) bool { return slices.Contains(names, f.Name) })
	}
	return func(f *File) bool {
		for _, test := range tests {
			if !test(f) {
				return false
			}
		}
		return true
	}, nil
}

// list answers the files a query matches, oldest first, in pages.
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	match, err := matcher(r.URL.Query().Get("q"))
	if err != nil {
		fail(w, http.StatusBadRequest, "invalidQuery")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []*File
	for _, f := range s.files {
		if match(f) {
			found = append(found, f)
		}
	}
	slices.SortFunc(found, func(a, b *File) int { return a.Created.Compare(b.Created) })
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size <= 0 {
		size = 100
	}
	if s.opt.PageSize > 0 {
		size = min(size, s.opt.PageSize)
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("pageToken"))
	end := min(start+size, len(found))
	page := []map[string]any{}
	for _, f := range found[start:end] {
		page = append(page, resource(f))
	}
	answer := map[string]any{"files": page}
	if end < len(found) {
		answer["nextPageToken"] = strconv.Itoa(end)
	}
	reply(w, answer)
}

// file returns the file a request names, the root of My Drive included, or answers that it is missing.
func (s *Server) file(w http.ResponseWriter, r *http.Request) (*File, bool) {
	fileID := r.PathValue("id")
	if fileID == "root" {
		fileID = s.Root
	}
	f, ok := s.files[fileID]
	if !ok {
		fail(w, http.StatusNotFound, "notFound")
	}
	return f, ok
}

// get answers the metadata of a file, or its content from the byte a Range header names.
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.file(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("alt") != "media" {
		reply(w, resource(f))
		return
	}
	content := f.Content
	if from, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		start, _ := strconv.Atoi(strings.TrimSuffix(from, "-"))
		if start >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		content = content[start:]
		w.WriteHeader(http.StatusPartialContent)
	}
	_, _ = w.Write(content)
}

// export answers a Google document converted to the format asked for: its content with the format named before it.
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.file(w, r)
	if !ok {
		return
	}
	_, _ = fmt.Fprintf(w, "%s:%s", r.URL.Query().Get("mimeType"), f.Content)
}

// metadata is what a request may change of a file.
type metadata struct {
	Name         *string  `json:"name"`
	MimeType     string   `json:"mimeType"`
	Parents      []string `json:"parents"`
	ModifiedTime *string  `json:"modifiedTime"`
	Trashed      *bool    `json:"trashed"`
}

// apply changes a file as metadata says.
func (m metadata) apply(f *File, now time.Time) {
	if m.Name != nil {
		f.Name = *m.Name
	}
	if m.Trashed != nil {
		f.Trashed = *m.Trashed
	}
	f.Modified = now
	if m.ModifiedTime != nil {
		if at, err := time.Parse(time.RFC3339Nano, *m.ModifiedTime); err == nil {
			f.Modified = at.UTC()
		}
	}
}

// createFolder creates a folder, the only file the API creates without content.
func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var m metadata
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.MimeType != FolderMime || m.Name == nil || len(m.Parents) != 1 {
		fail(w, http.StatusBadRequest, "badRequest")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := &File{Name: *m.Name, MimeType: FolderMime, Parents: m.Parents}
	s.add(f)
	reply(w, resource(f))
}

// update changes the name, the time or the trash of a file, and moves it between parents.
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	var m metadata
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		fail(w, http.StatusBadRequest, "badRequest")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.file(w, r)
	if !ok {
		return
	}
	if add := r.URL.Query().Get("addParents"); add != "" {
		f.Parents = slices.DeleteFunc(f.Parents, func(p string) bool { return p == r.URL.Query().Get("removeParents") })
		f.Parents = append(f.Parents, add)
	}
	m.apply(f, s.now())
	reply(w, resource(f))
}

// startUpload opens a resumable upload session, for a new file or for new content of an existing one.
func (s *Server) startUpload(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("uploadType") != "resumable" {
		fail(w, http.StatusBadRequest, "badRequest")
		return
	}
	var m metadata
	_ = json.NewDecoder(r.Body).Decode(&m)
	s.mu.Lock()
	defer s.mu.Unlock()
	session := &upload{}
	if r.PathValue("id") != "" {
		f, ok := s.file(w, r)
		if !ok {
			return
		}
		session.file, session.update = f, true
	} else {
		if m.Name == nil || len(m.Parents) != 1 {
			fail(w, http.StatusBadRequest, "badRequest")
			return
		}
		session.file = &File{Name: *m.Name, MimeType: "application/octet-stream", Parents: m.Parents}
	}
	key := id()
	s.uploads[key] = session
	w.Header().Set("Location", s.URL+"/upload/session/"+key)
}

// contentRange reads a Content-Range header: the first and last bytes of the chunk, and the total when it is known.
var contentRange = regexp.MustCompile(`^bytes (?:(\d+)-(\d+)|\*)/(\d+|\*)$`)

// uploadChunk receives one chunk of a resumable upload; the chunk that gives the total completes it.
func (s *Server) uploadChunk(w http.ResponseWriter, r *http.Request) {
	m := contentRange.FindStringSubmatch(r.Header.Get("Content-Range"))
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.uploads[r.PathValue("id")]
	if !ok || m == nil {
		fail(w, http.StatusNotFound, "notFound")
		return
	}
	if m[1] != "" {
		if start, _ := strconv.Atoi(m[1]); start != session.data.Len() {
			fail(w, http.StatusBadRequest, "badContentRange")
			return
		}
	}
	session.data.Write(body)
	if m[3] == "*" {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", session.data.Len()-1))
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	delete(s.uploads, r.PathValue("id"))
	f := session.file
	f.Content = bytes.Clone(session.data.Bytes())
	if session.update {
		f.Modified = s.now()
	} else {
		s.add(f)
	}
	reply(w, resource(f))
}
