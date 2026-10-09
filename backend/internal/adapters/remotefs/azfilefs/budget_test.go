package azfilefs

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestListingPaginationBudget rejects repeated continuation tokens without accumulating an unbounded listing.
func TestListingPaginationBudget(t *testing.T) {
	f := fixed(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<EnumerationResults><NextMarker>repeat</NextMarker></EnumerationResults>")
	})
	if _, _, err := f.children(t.Context(), ""); !errors.Is(err, remotefs.ErrInvalid) {
		t.Fatal(err)
	}
}
