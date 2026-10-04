// Package s3test runs an in-memory S3 server for tests, with one bucket and failures that can be injected.
package s3test

import (
	"bytes"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// Bucket is the bucket every test server has.
const Bucket = "transferly"

// Fault is a failure the server answers instead of performing a request: an HTTP status and an S3 error code.
type Fault struct {
	// Status is the HTTP status of the answer.
	Status int
	// Code is the S3 error code, such as AccessDenied; empty answers with no body, as to a HEAD request.
	Code string
}

// Options configure a test server.
type Options struct {
	// Plain serves plain HTTP instead of HTTPS. Uploads over plain HTTP are signed in chunks, which gofakes3 does not
	// read, so a plain server only suits tests that do not write.
	Plain bool
	// Fault returns the failure to answer a request with, or nil to perform it.
	Fault func(r *http.Request) *Fault
}

// Server is a running test server.
type Server struct {
	// Addr is the host:port the server listens on, over HTTPS unless the server is plain.
	Addr string
	// RootCAs trust the certificate of the server; nil for a plain server.
	RootCAs *x509.CertPool
	// Backend holds the buckets and objects the server serves.
	Backend *s3mem.Backend
}

// Start runs a server with the bucket [Bucket] until the test ends. Besides what gofakes3 serves, it copies parts of objects
// on the server (UploadPartCopy), which copies of objects too large for one request use.
func Start(t testing.TB, opt Options) *Server {
	t.Helper()
	backend := s3mem.New()
	if err := backend.CreateBucket(Bucket); err != nil {
		t.Fatal(err)
	}
	fake := gofakes3.New(backend, gofakes3.WithoutVersioning()).Server()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opt.Fault != nil {
			if fault := opt.Fault(r); fault != nil {
				answer(w, r, fault)
				return
			}
		}
		if r.Method == http.MethodPut && r.URL.Query().Has("partNumber") && r.Header.Get("X-Amz-Copy-Source") != "" {
			copyPart(w, r, backend, fake)
			return
		}
		fake.ServeHTTP(w, r)
	})
	server := httptest.NewUnstartedServer(handler)
	if opt.Plain {
		server.Start()
	} else {
		server.StartTLS()
	}
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	started := &Server{Addr: address.Host, Backend: backend}
	if !opt.Plain {
		started.RootCAs = x509.NewCertPool()
		started.RootCAs.AddCert(server.Certificate())
	}
	return started
}

// answer writes the error document of an S3 failure.
func answer(w http.ResponseWriter, r *http.Request, fault *Fault) {
	if fault.Code == "" || r.Method == http.MethodHead {
		w.WriteHeader(fault.Status)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(fault.Status)
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
		Message string   `xml:"Message"`
	}{Code: fault.Code, Message: fault.Code})
}

// copyPart performs an UploadPartCopy, which gofakes3 lacks: it reads the range of the source object and uploads it as the
// part, then answers with the result of a copy.
func copyPart(w http.ResponseWriter, r *http.Request, backend *s3mem.Backend, fake http.Handler) {
	source, _ := url.PathUnescape(strings.TrimPrefix(r.Header.Get("X-Amz-Copy-Source"), "/"))
	bucket, key, _ := strings.Cut(source, "/")
	var span *gofakes3.ObjectRangeRequest
	if value := r.Header.Get("X-Amz-Copy-Source-Range"); value != "" {
		span = &gofakes3.ObjectRangeRequest{}
		_, _ = fmt.Sscanf(value, "bytes=%d-%d", &span.Start, &span.End)
	}
	object, err := backend.GetObject(bucket, key, span)
	if err != nil {
		answer(w, r, &Fault{Status: http.StatusNotFound, Code: "NoSuchKey"})
		return
	}
	data, _ := io.ReadAll(object.Contents)
	part := r.Clone(r.Context())
	part.Body, part.ContentLength = io.NopCloser(bytes.NewReader(data)), int64(len(data))
	part.Header.Del("X-Amz-Copy-Source")
	part.Header.Del("X-Amz-Copy-Source-Range")
	part.Header.Set("Content-Length", strconv.Itoa(len(data)))
	part.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	uploaded := httptest.NewRecorder()
	fake.ServeHTTP(uploaded, part)
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(struct {
		XMLName      xml.Name `xml:"CopyPartResult"`
		ETag         string   `xml:"ETag"`
		LastModified string   `xml:"LastModified"`
	}{ETag: uploaded.Header().Get("ETag"), LastModified: time.Now().UTC().Format(time.RFC3339)})
}
