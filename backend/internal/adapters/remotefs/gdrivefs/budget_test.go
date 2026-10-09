package gdrivefs

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestListingPaginationBudget rejects a repeated Drive token even when its pages contain no files.
func TestListingPaginationBudget(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"nextPageToken":"repeat"}`) }))
	defer s.Close()
	f := &FS{client: s.Client(), api: s.URL}
	if _, err := f.children(t.Context(), "root", ""); !errors.Is(err, remotefs.ErrInvalid) {
		t.Fatal(err)
	}
}
