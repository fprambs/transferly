package azblobfs

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestListingPaginationBudget rejects a service that keeps returning the same continuation token.
func TestListingPaginationBudget(t *testing.T) {
	f := fixed(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<EnumerationResults><NextMarker>repeat</NextMarker></EnumerationResults>")
	})
	if _, _, err := f.list(t.Context(), "", false, 0); !errors.Is(err, remotefs.ErrInvalid) {
		t.Fatal(err)
	}
}
