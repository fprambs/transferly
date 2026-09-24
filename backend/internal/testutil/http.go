package testutil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// Response is a fully read HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Fetch sends a request without a body and returns the fully read response.
func Fetch(ctx context.Context, method, url string) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return Response{}, fmt.Errorf("building request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("reading response: %w", err)
	}
	return Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// Do is [Fetch] bound to the test: it uses the test context and fails on error.
func Do(t testing.TB, method, url string) Response {
	t.Helper()
	r, err := Fetch(t.Context(), method, url)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return r
}
