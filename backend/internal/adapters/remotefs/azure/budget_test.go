package azure

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// stalledBody models a server that never finishes an error body unless its connection is closed.
type stalledBody struct {
	closed chan struct{}
	once   sync.Once
}

// Read waits for cancellation rather than producing an unlimited stream.
func (b *stalledBody) Read([]byte) (int, error) { <-b.closed; return 0, io.EOF }

// Close ends the remote stream exactly once.
func (b *stalledBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

// TestErrorBodyDeadline verifies that an unresponsive error body cannot keep an operation alive indefinitely.
func TestErrorBodyDeadline(t *testing.T) {
	saved := errorReadTimeout
	errorReadTimeout = time.Millisecond
	defer func() { errorReadTimeout = saved }()
	b := &stalledBody{closed: make(chan struct{})}
	if err := failure(&http.Response{StatusCode: 500, Header: http.Header{}, Body: b}); err == nil {
		t.Fatal("missing remote error")
	}
	select {
	case <-b.closed:
	default:
		t.Fatal("remote body was not closed")
	}
}
