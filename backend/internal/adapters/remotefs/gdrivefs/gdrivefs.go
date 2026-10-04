// Package gdrivefs implements [remotefs.RemoteFS] on Google Drive (ADR-0024): My Drive of a Google account, or one of its shared
// drives, through a small client of the Drive v3 REST API. Paths are resolved to the IDs of Drive one folder at a time; of files of
// the same name in a folder, the oldest is the one its path names. Google Docs, Sheets, Slides and Drawings are exported to Office
// formats and PDF when they are read, under their name with the extension of that format.
package gdrivefs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"

	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// Config tells how to reach a drive.
type Config struct {
	// APIURL is the origin of the Drive API, https://www.googleapis.com; tests point it at a fake.
	APIURL string
	// Client sends the requests, authorized with the token of the account, as [oauth2.NewClient] makes it.
	Client *http.Client
	// SharedDrive is the name of a shared drive to show instead of My Drive; empty shows My Drive.
	SharedDrive string
}

// DefaultAPIURL is the origin of the Drive API of Google.
const DefaultAPIURL = "https://www.googleapis.com"

// chunkSize is the size of the chunks of resumable uploads: a multiple of 256 KiB, as Drive requires, that bounds the memory of
// each upload.
var chunkSize = 8 << 20

// The mime types of Drive the adapter tells apart.
const (
	folderMime   = "application/vnd.google-apps.folder"
	googlePrefix = "application/vnd.google-apps."
)

// export is the format a Google document is exported to, and the extension added to its name.
type export struct {
	mime, ext string
}

// exports are the Google documents that can be read, by their mime type; other Google files, such as forms and shortcuts, are not
// listed.
var exports = map[string]export{
	"application/vnd.google-apps.document":     {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"},
	"application/vnd.google-apps.spreadsheet":  {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"},
	"application/vnd.google-apps.presentation": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx"},
	"application/vnd.google-apps.drawing":      {"application/pdf", ".pdf"},
}

// fileFields are the fields of a file the adapter asks for.
const fileFields = "id,name,mimeType,size,modifiedTime,createdTime"

// file is a file or a folder of Drive.
type file struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	MimeType string    `json:"mimeType"`
	Size     string    `json:"size"`
	Modified time.Time `json:"modifiedTime"`
}

// folder reports whether the file is a folder.
func (f file) folder() bool { return f.MimeType == folderMime }

// document returns the export of a Google document, if the file is one.
func (f file) document() (export, bool) {
	e, ok := exports[f.MimeType]
	return e, ok
}

// display is the name a file is shown under: a Google document with the extension of its export, and empty for Google files that
// cannot be read.
func (f file) display() string {
	if e, ok := f.document(); ok {
		return f.Name + e.ext
	}
	if strings.HasPrefix(f.MimeType, googlePrefix) && !f.folder() {
		return ""
	}
	return f.Name
}

// size is the size of the content of a file; Google documents report none until they are exported.
func (f file) size() int64 {
	n, _ := strconv.ParseInt(f.Size, 10, 64)
	return n
}

// entry describes the file at p.
func (f file) entry(p string) remotefs.Entry {
	e := remotefs.Entry{Name: path.Base(p), Path: p, Size: f.size(), Mode: 0o644, ModTime: f.Modified.UTC(), ModTimePrecision: remotefs.PrecisionSecond}
	if f.folder() {
		e.Mode, e.Size = fs.ModeDir|0o755, 0
	}
	return e
}

// FS is a drive seen as a file system. It is safe for concurrent use.
type FS struct {
	api    string
	client *http.Client
	root   file
	drive  string
	closed atomic.Bool
	mu     sync.Mutex
	cache  map[string]file
}

// apiError is an error the Drive API answered.
type apiError struct {
	Status int
	Reason string
}

// Error implements error.
func (e *apiError) Error() string { return fmt.Sprintf("drive answered %d %s", e.Status, e.Reason) }

// Dial checks that the drive can be reached with the token of cfg and returns its file system: My Drive, or the shared drive of
// the name cfg gives.
func Dial(ctx context.Context, cfg Config) (*FS, error) {
	f := &FS{api: strings.TrimSuffix(cfg.APIURL, "/"), client: cfg.Client, cache: map[string]file{}}
	if err := f.Ping(ctx); err != nil {
		return nil, err
	}
	if cfg.SharedDrive == "" {
		if err := f.call(ctx, http.MethodGet, "/drive/v3/files/root", url.Values{"fields": {"id"}}, nil, &f.root); err != nil {
			return nil, wrap("open", "/", err)
		}
		f.root.MimeType = folderMime
		return f, nil
	}
	var drives struct {
		Drives []struct{ ID, Name string } `json:"drives"`
	}
	if err := f.call(ctx, http.MethodGet, "/drive/v3/drives", url.Values{"pageSize": {"100"}}, nil, &drives); err != nil {
		return nil, wrap("open", cfg.SharedDrive, err)
	}
	for _, d := range drives.Drives {
		if d.Name == cfg.SharedDrive {
			f.root, f.drive = file{ID: d.ID, MimeType: folderMime}, d.ID
			return f, nil
		}
	}
	return nil, fmt.Errorf("shared drive %q: %w", cfg.SharedDrive, remotefs.ErrNotFound)
}

// call sends a request to the API, with query and a JSON body when given, and decodes the JSON answer into out when given.
func (f *FS) call(ctx context.Context, method, endpoint string, query url.Values, body, out any) error {
	resp, err := f.send(ctx, method, f.api+endpoint, query, body, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: unreadable answer: %w", remotefs.ErrConnectionLost, err)
	}
	return nil
}

// send sends a request and returns its answer when it succeeded; a failure answered by the API is an [apiError].
func (f *FS) send(ctx context.Context, method, target string, query url.Values, body any, header http.Header) (*http.Response, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("supportsAllDrives", "true")
	var reader io.Reader
	if raw, ok := body.([]byte); ok {
		reader = bytes.NewReader(raw)
	} else if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target+"?"+query.Encode(), reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", remotefs.ErrInvalid, err)
	}
	for name, values := range header {
		req.Header[name] = values
	}
	if _, raw := body.([]byte); body != nil && !raw {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer func() { _ = resp.Body.Close() }()
		var answer struct {
			Error struct {
				Errors []struct{ Reason string } `json:"errors"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&answer)
		reason := ""
		if len(answer.Error.Errors) > 0 {
			reason = answer.Error.Errors[0].Reason
		}
		return nil, &apiError{Status: resp.StatusCode, Reason: reason}
	}
	return resp, nil
}

// wrap turns a failure into the errors of the port, keeping cancellations as they are and destinations the network guard refused
// as [netguard.ErrBlocked].
func wrap(op, p string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, remotefs.ErrConnectionLost) || errors.Is(err, remotefs.ErrInvalid) {
		return err
	}
	if errors.Is(err, netguard.ErrBlocked) {
		return fmt.Errorf("%s %q: %w", op, p, err)
	}
	var refused *oauth2.RetrieveError
	if errors.As(err, &refused) {
		return fmt.Errorf("%s %q: %w: Google refused the token of the account, which must sign in again: %w", op, p, remotefs.ErrAuth, err)
	}
	kind := remotefs.ErrConnectionLost
	var answer *apiError
	if errors.As(err, &answer) {
		switch {
		case answer.Status == http.StatusUnauthorized:
			kind = remotefs.ErrAuth
		case answer.Status == http.StatusNotFound:
			kind = remotefs.ErrNotFound
		case answer.Reason == "storageQuotaExceeded" || answer.Reason == "quotaExceeded":
			kind = remotefs.ErrQuota
		case answer.Status == http.StatusForbidden && !strings.HasSuffix(answer.Reason, "RateLimitExceeded") && answer.Reason != "rateLimitExceeded":
			kind = remotefs.ErrPermission
		case answer.Status < http.StatusInternalServerError && answer.Status != http.StatusTooManyRequests && answer.Status != http.StatusForbidden:
			kind = remotefs.ErrInvalid
		}
	}
	return fmt.Errorf("%s %q: %w: %w", op, p, kind, err)
}

// begin starts an operation on p: it fails when ctx is done or the file system is closed, and returns p cleaned.
func (f *FS) begin(ctx context.Context, op, p string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.closed.Load() {
		return "", fmt.Errorf("%s %q: %w", op, p, remotefs.ErrConnectionLost)
	}
	if p == "" || p[0] != '/' || strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%s %q: %w: the path must be absolute", op, p, remotefs.ErrInvalid)
	}
	return path.Clean(p), nil
}

// quote escapes a value for a query of Drive.
func quote(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// children lists the files of a folder that are not in the trash, oldest first, keeping those whose query terms match.
func (f *FS) children(ctx context.Context, parent, terms string) ([]file, error) {
	q := url.Values{
		"q":                         {fmt.Sprintf("%s in parents and trashed = false%s", quote(parent), terms)},
		"fields":                    {"nextPageToken,files(" + fileFields + ")"},
		"orderBy":                   {"createdTime"},
		"pageSize":                  {"1000"},
		"includeItemsFromAllDrives": {"true"},
	}
	if f.drive != "" {
		q.Set("corpora", "drive")
		q.Set("driveId", f.drive)
	}
	var all []file
	for {
		var page struct {
			Files []file `json:"files"`
			Next  string `json:"nextPageToken"`
		}
		if err := f.call(ctx, http.MethodGet, "/drive/v3/files", q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Files...)
		if page.Next == "" {
			return all, nil
		}
		q.Set("pageToken", page.Next)
	}
}

// child finds the file of a folder shown under name: the oldest of those of that name, or of a Google document whose name with the
// extension of its export is name.
func (f *FS) child(ctx context.Context, parent, name string) (file, bool, error) {
	names := []string{quote(name)}
	for _, e := range exports {
		if base, ok := strings.CutSuffix(name, e.ext); ok && base != "" {
			names = append(names, quote(base))
		}
	}
	terms := " and (name = " + strings.Join(names, " or name = ") + ")"
	found, err := f.children(ctx, parent, terms)
	if err != nil {
		return file{}, false, err
	}
	for _, candidate := range found {
		if candidate.display() == name {
			return candidate, true, nil
		}
	}
	return file{}, false, nil
}

// resolve finds the file at p, walking from the root one folder at a time and remembering what it found; ok is false when nothing
// is there, a path below a file included.
func (f *FS) resolve(ctx context.Context, op, p string) (file, bool, error) {
	f.mu.Lock()
	cached, hit := f.cache[p]
	f.mu.Unlock()
	if p == "/" {
		return f.root, true, nil
	}
	if hit {
		return cached, true, nil
	}
	parent, ok, err := f.resolve(ctx, op, path.Dir(p))
	if err != nil || !ok || !parent.folder() {
		return file{}, false, err
	}
	found, ok, err := f.child(ctx, parent.ID, path.Base(p))
	if err != nil {
		return file{}, false, wrap(op, p, err)
	}
	if ok {
		f.mu.Lock()
		f.cache[p] = found
		f.mu.Unlock()
	}
	return found, ok, nil
}

// forget drops what was remembered of paths, after a change.
func (f *FS) forget() {
	f.mu.Lock()
	clear(f.cache)
	f.mu.Unlock()
}

// parentFolder finds the folder p is in, or fails as the port says when it is missing or a file.
func (f *FS) parentFolder(ctx context.Context, op, p string) (file, error) {
	parent, ok, err := f.resolve(ctx, op, path.Dir(p))
	switch {
	case err != nil:
		return file{}, err
	case !ok:
		return file{}, fmt.Errorf("%s %q: %w: %q does not exist", op, p, remotefs.ErrNotFound, path.Dir(p))
	case !parent.folder():
		return file{}, fmt.Errorf("%s %q: %w: %q is a file", op, p, remotefs.ErrNotDirectory, path.Dir(p))
	}
	return parent, nil
}

// List implements remotefs.RemoteFS.
func (f *FS) List(ctx context.Context, dir string) ([]remotefs.Entry, error) {
	p, err := f.begin(ctx, "list", dir)
	if err != nil {
		return nil, err
	}
	folder, ok, err := f.resolve(ctx, "list", p)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotFound)
	case !folder.folder():
		return nil, fmt.Errorf("list %q: %w", dir, remotefs.ErrNotDirectory)
	}
	found, err := f.children(ctx, folder.ID, "")
	if err != nil {
		return nil, wrap("list", dir, err)
	}
	entries := []remotefs.Entry{}
	seen := map[string]bool{}
	for _, child := range found {
		name := child.display()
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, child.entry(path.Join(p, name)))
	}
	return entries, nil
}

// Stat implements remotefs.RemoteFS.
func (f *FS) Stat(ctx context.Context, name string) (remotefs.Entry, error) {
	p, err := f.begin(ctx, "stat", name)
	if err != nil {
		return remotefs.Entry{}, err
	}
	found, ok, err := f.resolve(ctx, "stat", p)
	switch {
	case err != nil:
		return remotefs.Entry{}, err
	case !ok:
		return remotefs.Entry{}, fmt.Errorf("stat %q: %w", name, remotefs.ErrNotFound)
	}
	return found.entry(p), nil
}

// OpenRead implements remotefs.RemoteFS. A Google document is exported, and the bytes before offset are skipped.
func (f *FS) OpenRead(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	p, err := f.begin(ctx, "read", name)
	if err != nil {
		return nil, err
	}
	found, ok, err := f.resolve(ctx, "read", p)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("read %q: %w", name, remotefs.ErrNotFound)
	case found.folder():
		return nil, fmt.Errorf("read %q: %w: it is a folder", name, remotefs.ErrInvalid)
	}
	if e, ok := found.document(); ok {
		body, err := f.stream(ctx, "/drive/v3/files/"+found.ID+"/export", url.Values{"mimeType": {e.mime}}, nil)
		if err != nil {
			return nil, wrap("read", name, err)
		}
		r := &reader{body: body, path: name}
		if _, err := io.CopyN(io.Discard, r, offset); err != nil && !errors.Is(err, io.EOF) {
			_ = r.Close()
			return nil, err
		}
		return r, nil
	}
	switch size := found.size(); {
	case offset < 0 || offset > size:
		return nil, fmt.Errorf("read %q at %d: %w: the file has %d bytes", name, offset, remotefs.ErrInvalid, size)
	case offset == size:
		return io.NopCloser(strings.NewReader("")), nil
	}
	header := http.Header{}
	if offset > 0 {
		header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	body, err := f.stream(ctx, "/drive/v3/files/"+found.ID, url.Values{"alt": {"media"}}, header)
	if err != nil {
		return nil, wrap("read", name, err)
	}
	return &reader{body: body, path: name}, nil
}

// stream sends a GET request to the API and hands over the body of its answer, which the caller closes.
func (f *FS) stream(ctx context.Context, endpoint string, query url.Values, header http.Header) (io.ReadCloser, error) {
	resp, err := f.send(ctx, http.MethodGet, f.api+endpoint, query, nil, header)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// reader streams content, turning its failures into the errors of the port.
type reader struct {
	body io.ReadCloser
	path string
}

// Read implements io.Reader.
func (r *reader) Read(b []byte) (int, error) {
	n, err := r.body.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, wrap("read", r.path, err)
	}
	return n, err
}

// Close implements io.Closer.
func (r *reader) Close() error { return r.body.Close() }

// OpenWrite implements remotefs.RemoteFS through a resumable upload session; overwriting uploads a new version of the same file.
// Google documents cannot be written, and writes cannot resume.
func (f *FS) OpenWrite(ctx context.Context, name string, offset int64, mode remotefs.WriteMode) (io.WriteCloser, error) {
	p, err := f.begin(ctx, "write", name)
	if err != nil {
		return nil, err
	}
	switch {
	case p == "/":
		return nil, fmt.Errorf("write %q: %w: it is a folder", name, remotefs.ErrInvalid)
	case mode == remotefs.WriteResume:
		return nil, fmt.Errorf("write %q at %d: %w: Drive uploads cannot resume", name, offset, remotefs.ErrInvalid)
	case mode != remotefs.WriteCreate && mode != remotefs.WriteOverwrite:
		return nil, fmt.Errorf("write %q: %w: unknown write mode %d", name, remotefs.ErrInvalid, mode)
	}
	parent, err := f.parentFolder(ctx, "write", p)
	if err != nil {
		return nil, err
	}
	existing, exists, err := f.resolve(ctx, "write", p)
	_, document := existing.document()
	switch {
	case err != nil:
		return nil, err
	case exists && (existing.folder() || document):
		return nil, fmt.Errorf("write %q: %w: it is a folder or a Google document", name, remotefs.ErrInvalid)
	case exists && mode == remotefs.WriteCreate:
		return nil, fmt.Errorf("write %q: %w", name, remotefs.ErrExists)
	}
	method, endpoint, body := http.MethodPost, "/upload/drive/v3/files", map[string]any{"name": path.Base(p), "parents": []string{parent.ID}}
	if exists {
		method, endpoint, body = http.MethodPatch, "/upload/drive/v3/files/"+existing.ID, map[string]any{}
	}
	resp, err := f.send(ctx, method, f.api+endpoint, url.Values{"uploadType": {"resumable"}}, body, nil)
	if err != nil {
		return nil, wrap("write", name, err)
	}
	_ = resp.Body.Close()
	return &writer{ctx: ctx, fs: f, session: resp.Header.Get("Location"), path: name}, nil
}

// writer sends what is written to an upload session in chunks; closing it sends the last chunk, which completes the upload, unless
// its context ended first.
type writer struct {
	ctx     context.Context
	fs      *FS
	session string
	path    string
	buf     []byte
	sent    int64
	done    bool
}

// Write implements io.Writer.
func (w *writer) Write(b []byte) (int, error) {
	switch {
	case w.fs.closed.Load():
		return 0, fmt.Errorf("write %q: %w", w.path, remotefs.ErrConnectionLost)
	case w.done:
		return 0, fmt.Errorf("write %q: %w: the upload is closed", w.path, remotefs.ErrInvalid)
	}
	w.buf = append(w.buf, b...)
	for len(w.buf) >= chunkSize {
		if err := w.chunk(w.buf[:chunkSize], false); err != nil {
			return 0, err
		}
		w.buf = append(w.buf[:0], w.buf[chunkSize:]...)
	}
	return len(b), nil
}

// chunk sends a chunk of the upload; the last one says the total size, which completes it.
func (w *writer) chunk(data []byte, last bool) error {
	total, span := "*", "*"
	if last {
		total = strconv.FormatInt(w.sent+int64(len(data)), 10)
	}
	if len(data) > 0 {
		span = fmt.Sprintf("%d-%d", w.sent, w.sent+int64(len(data))-1)
	}
	header := http.Header{"Content-Range": {"bytes " + span + "/" + total}}
	resp, err := w.fs.send(w.ctx, http.MethodPut, w.session, nil, data, header)
	if err != nil {
		return wrap("write", w.path, err)
	}
	_ = resp.Body.Close()
	if !last && resp.StatusCode != http.StatusPermanentRedirect {
		return fmt.Errorf("write %q: %w: Drive answered %d to a chunk", w.path, remotefs.ErrConnectionLost, resp.StatusCode)
	}
	w.sent += int64(len(data))
	return nil
}

// Close implements io.Closer: it sends the last chunk and waits for Drive to store the file. When the context of the write ended,
// as when the data being uploaded failed half way, the session is abandoned and nothing is stored.
func (w *writer) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.ctx.Err(); err != nil {
		return err
	}
	defer w.fs.forget()
	return w.chunk(w.buf, true)
}

// Mkdir implements remotefs.RemoteFS.
func (f *FS) Mkdir(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "mkdir", name)
	if err != nil {
		return err
	}
	switch _, exists, err := f.resolve(ctx, "mkdir", p); {
	case err != nil:
		return err
	case exists:
		return fmt.Errorf("mkdir %q: %w", name, remotefs.ErrExists)
	}
	parent, err := f.parentFolder(ctx, "mkdir", p)
	if err != nil {
		return err
	}
	defer f.forget()
	if err := f.call(ctx, http.MethodPost, "/drive/v3/files", nil, map[string]any{"name": path.Base(p), "mimeType": folderMime, "parents": []string{parent.ID}}, nil); err != nil {
		return wrap("mkdir", name, err)
	}
	return nil
}

// trash moves a file to the trash of Drive, where it can be restored.
func (f *FS) trash(ctx context.Context, name string, target file) error {
	defer f.forget()
	if err := f.call(ctx, http.MethodPatch, "/drive/v3/files/"+target.ID, nil, map[string]any{"trashed": true}, nil); err != nil {
		return wrap("remove", name, err)
	}
	return nil
}

// Remove implements remotefs.RemoteFS: the file, or the empty folder, goes to the trash.
func (f *FS) Remove(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "remove", name)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", name, remotefs.ErrPermission)
	}
	target, ok, err := f.resolve(ctx, "remove", p)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("remove %q: %w", name, remotefs.ErrNotFound)
	case target.folder():
		inside, err := f.children(ctx, target.ID, "")
		if err != nil {
			return wrap("remove", name, err)
		}
		if len(inside) > 0 {
			return fmt.Errorf("remove %q: %w", name, remotefs.ErrNotEmpty)
		}
	}
	return f.trash(ctx, name, target)
}

// RemoveAll implements remotefs.RemoteFS: the file or the folder, with everything in it, goes to the trash.
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	p, err := f.begin(ctx, "remove", name)
	if err != nil {
		return err
	}
	if p == "/" {
		return fmt.Errorf("remove %q: %w: the root cannot be removed", name, remotefs.ErrPermission)
	}
	target, ok, err := f.resolve(ctx, "remove", p)
	if err != nil || !ok {
		return err
	}
	return f.trash(ctx, name, target)
}

// Rename implements remotefs.RemoteFS: Drive renames and moves files itself. A Google document keeps the extension of its export.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	src, err := f.begin(ctx, "rename", from)
	if err != nil {
		return err
	}
	dst, err := f.begin(ctx, "rename", to)
	if err != nil {
		return err
	}
	if src == "/" || strings.HasPrefix(dst+"/", src+"/") {
		return fmt.Errorf("rename %q to %q: %w: a folder cannot move into itself", from, to, remotefs.ErrInvalid)
	}
	target, ok, err := f.resolve(ctx, "rename", src)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("rename %q: %w", from, remotefs.ErrNotFound)
	}
	switch _, exists, err := f.resolve(ctx, "rename", dst); {
	case err != nil:
		return err
	case exists:
		return fmt.Errorf("rename %q to %q: %w", from, to, remotefs.ErrExists)
	}
	newName := path.Base(dst)
	if e, ok := target.document(); ok {
		base, kept := strings.CutSuffix(newName, e.ext)
		if !kept || base == "" {
			return fmt.Errorf("rename %q to %q: %w: a Google document keeps the extension %s", from, to, remotefs.ErrInvalid, e.ext)
		}
		newName = base
	}
	parent, err := f.parentFolder(ctx, "rename", dst)
	if err != nil {
		return err
	}
	query := url.Values{}
	if old, _, _ := f.resolve(ctx, "rename", path.Dir(src)); old.ID != parent.ID {
		query.Set("addParents", parent.ID)
		query.Set("removeParents", old.ID)
	}
	defer f.forget()
	if err := f.call(ctx, http.MethodPatch, "/drive/v3/files/"+target.ID, query, map[string]any{"name": newName}, nil); err != nil {
		return wrap("rename", from, err)
	}
	return nil
}

// Chmod implements remotefs.RemoteFS; Drive shares files instead of giving them modes.
func (f *FS) Chmod(ctx context.Context, name string, _ fs.FileMode) error {
	if _, err := f.begin(ctx, "chmod", name); err != nil {
		return err
	}
	return fmt.Errorf("chmod %q: %w: Drive files have no permissions", name, remotefs.ErrInvalid)
}

// Chtimes implements remotefs.RemoteFS.
func (f *FS) Chtimes(ctx context.Context, name string, mtime time.Time) error {
	p, err := f.begin(ctx, "chtimes", name)
	if err != nil {
		return err
	}
	target, ok, err := f.resolve(ctx, "chtimes", p)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("chtimes %q: %w", name, remotefs.ErrNotFound)
	}
	defer f.forget()
	if err := f.call(ctx, http.MethodPatch, "/drive/v3/files/"+target.ID, nil, map[string]any{"modifiedTime": mtime.UTC().Format(time.RFC3339Nano)}, nil); err != nil {
		return wrap("chtimes", name, err)
	}
	return nil
}

// Capabilities implements remotefs.RemoteFS: times can be set; uploads cannot resume and files have no modes.
func (f *FS) Capabilities() remotefs.Capabilities { return remotefs.Capabilities{Chtimes: true} }

// Ping implements remotefs.RemoteFS: it asks who the account is.
func (f *FS) Ping(ctx context.Context) error {
	if _, err := f.begin(ctx, "ping", "/"); err != nil {
		return err
	}
	if err := f.call(ctx, http.MethodGet, "/drive/v3/about", url.Values{"fields": {"user(emailAddress)"}}, nil, nil); err != nil {
		return wrap("ping", "/", err)
	}
	return nil
}

// Close implements remotefs.RemoteFS. Later operations, and uploads still open, fail with [remotefs.ErrConnectionLost].
func (f *FS) Close() error {
	f.closed.Store(true)
	f.client.CloseIdleConnections()
	return nil
}
