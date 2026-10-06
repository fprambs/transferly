package httpapi_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// codePattern finds the stable codes the API answers with in its sources: the codes of writeProblem and problemBody, the table of
// domain errors, and the codes a problem is given or changed to.
var codePattern = regexp.MustCompile(`(?:writeProblem\([^,]+,[^,]+,|problemBody\([^,]+,|http\.Status\w+,|code:|code =|code :=|p\.code(?:, [^=]+)? =)\s*"([a-z][a-z0-9_]*)"`)

// serverCodes returns every stable code the sources of this package answer with, sorted.
func serverCodes(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, ".gen.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range codePattern.FindAllStringSubmatch(string(source), -1) {
			codes = append(codes, match[1])
		}
	}
	slices.Sort(codes)
	return slices.Compact(codes)
}

// guide is the part of a catalog of error guidance this test reads.
type guide struct {
	Codes map[string]struct {
		Cause string `json:"cause"`
		Next  string `json:"next"`
	} `json:"codes"`
}

// TestErrorCodes_Guided checks that every stable code the API answers with has guidance in every language of the interface (4.16): a
// probable cause and a next step, so a new code cannot reach users unexplained.
//
// Use cases:
//   - the codes are read from the sources of this package, so adding one without guidance fails here
//   - each language of the interface has a non-empty cause and next step for each code
func TestErrorCodes_Guided(t *testing.T) {
	t.Parallel()
	_, self, _, _ := runtime.Caller(0)
	dir := filepath.Dir(self)
	codes := serverCodes(t, dir)
	if len(codes) < 100 {
		t.Fatalf("found only %d codes; the pattern no longer matches the sources", len(codes))
	}
	locales, err := filepath.Glob(filepath.Join(dir, "../../../../frontend/src/i18n/locales/*/errors.json"))
	if err != nil || len(locales) == 0 {
		t.Fatalf("no error catalogs found: %v", err)
	}
	for _, locale := range locales {
		data, err := os.ReadFile(locale)
		if err != nil {
			t.Fatal(err)
		}
		var catalog guide
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		var missing []string
		for _, code := range codes {
			if entry, ok := catalog.Codes[code]; !ok || entry.Cause == "" || entry.Next == "" {
				missing = append(missing, code)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s has no guidance for %d codes: %s", filepath.Base(filepath.Dir(locale)), len(missing), strings.Join(missing, " "))
		}
	}
}
