package connectors_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/fprambs/sentinel/internal/core/connectors"
)

// sample is a connector with one field of each kind.
var sample = connectors.Descriptor{
	Type:     "sample",
	Name:     "Sample",
	Maturity: connectors.Beta,
	Fields: []connectors.Field{
		{Name: "host", Kind: connectors.Text, Required: true, MaxLength: 5},
		{Name: "port", Kind: connectors.Number, Default: "22", Min: 1, Max: 100},
		{Name: "count", Kind: connectors.Number},
		{Name: "secure", Kind: connectors.Boolean, Default: "true"},
		{Name: "mode", Kind: connectors.Choice, Choices: []string{"a", "b"}},
		{Name: "note", Kind: connectors.Text},
		{Name: "region", Kind: connectors.Text, Required: true, Default: "us"},
	},
}

// TestCatalog verifies registering and finding connectors.
//
// Use cases:
//   - the built-in catalog has SFTP and FTP, stable and enabled by default, with their settings and ways of authenticating
//   - a connector registers once; a second one of its type, or one without type or name, is refused
//   - an unknown type is not found
//   - all connectors are listed stable ones first, each group by name, also while others register at the same time
func TestCatalog(t *testing.T) {
	catalog := connectors.Builtin()
	sftp, err := catalog.Lookup("sftp")
	if err != nil || sftp.Name != "SFTP" || sftp.Maturity != connectors.Stable || !sftp.EnabledByDefault || !sftp.Capabilities.Resume {
		t.Fatalf("sftp = %+v, %v", sftp, err)
	}
	ftp, _ := catalog.Lookup("ftp")
	if len(ftp.Fields) != 5 || ftp.Fields[3].Default != "optional" {
		t.Errorf("ftp fields = %+v", ftp.Fields)
	}
	if err := catalog.Register(sample); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Register(sample); !errors.Is(err, connectors.ErrDuplicate) {
		t.Errorf("duplicate = %v", err)
	}
	if err := catalog.Register(connectors.Descriptor{Type: "x"}); !errors.Is(err, connectors.ErrInvalidConfig) {
		t.Errorf("nameless = %v", err)
	}
	if _, err := catalog.Lookup("gopher"); !errors.Is(err, connectors.ErrUnknown) {
		t.Errorf("unknown = %v", err)
	}
	var wg sync.WaitGroup
	for _, kind := range []string{"a-beta", "z-stable"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			maturity := connectors.Beta
			if kind == "z-stable" {
				maturity = connectors.Stable
			}
			_ = catalog.Register(connectors.Descriptor{Type: kind, Name: kind, Maturity: maturity})
		}()
	}
	wg.Wait()
	var names []string
	for _, d := range catalog.All() {
		names = append(names, d.Name)
	}
	want := []string{"FTP / FTPS", "SFTP", "z-stable", "Sample", "a-beta"}
	if len(names) != len(want) {
		t.Fatalf("all = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("all = %v; want %v", names, want)
		}
	}
}

// TestValidate verifies a configuration against the settings of a connector.
//
// Use cases:
//   - a valid configuration keeps its values and gets the defaults of what it left out
//   - unknown settings, missing required ones, bad numbers, numbers out of range, bad booleans, choices not offered and texts too long are
//     refused, naming the field and the reason
//   - a number without bounds takes any value
func TestValidate(t *testing.T) {
	got, err := sample.Validate(map[string]string{"host": "a.b", "mode": "b", "count": "-7", "note": ""})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"host": "a.b", "port": "22", "count": "-7", "secure": "true", "mode": "b", "note": "", "region": "us"}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q; want %q", key, got[key], value)
		}
	}
	cases := map[string]struct {
		config map[string]string
		field  string
		reason string
	}{
		"unknown":  {map[string]string{"host": "a", "shade": "x"}, "shade", "unknown"},
		"required": {map[string]string{}, "host", "required"},
		"number":   {map[string]string{"host": "a", "port": "x"}, "port", "number"},
		"range":    {map[string]string{"host": "a", "port": "101"}, "port", "range"},
		"boolean":  {map[string]string{"host": "a", "secure": "yes"}, "secure", "boolean"},
		"choice":   {map[string]string{"host": "a", "mode": "c"}, "mode", "choice"},
		"length":   {map[string]string{"host": "abcdef"}, "host", "length"},
	}
	for name, c := range cases {
		_, err := sample.Validate(c.config)
		var field *connectors.FieldError
		if !errors.As(err, &field) || field.Field != c.field || field.Reason != c.reason || !errors.Is(err, connectors.ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
		if err != nil && err.Error() == "" {
			t.Errorf("%s: empty message", name)
		}
	}
}
