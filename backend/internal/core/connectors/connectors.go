// Package connectors is the catalog of the connectors Sentinel is built with (ADR-0019): every kind of storage a site can reach, such as
// SFTP, FTP or S3, described by what it is, what it can do, how it is configured and how it authenticates. The site form, the validation
// of the API and the store of the admins follow from these descriptors, so a new connector touches no other code.
package connectors

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Maturity tells how far a connector is along.
type Maturity string

// The maturities of connectors.
const (
	Stable Maturity = "stable"
	Beta   Maturity = "beta"
)

// FieldKind is the type of a field of the configuration of a connector.
type FieldKind string

// The kinds of fields.
const (
	Text    FieldKind = "text"
	Number  FieldKind = "number"
	Boolean FieldKind = "boolean"
	Choice  FieldKind = "choice"
)

// Field is one setting of a connector, from which the site form draws its input and the API validates it.
type Field struct {
	// Name is the key of the setting.
	Name string
	// Kind is its type.
	Kind FieldKind
	// Required says a value must be given.
	Required bool
	// Default is the value used when none is given, or empty.
	Default string
	// Choices are the values a choice allows.
	Choices []string
	// Min and Max bound a number, when Max is above zero.
	Min, Max int
	// MaxLength bounds a text, when above zero.
	MaxLength int
}

// Auth is a way a connector authenticates.
type Auth string

// The ways of authenticating.
const (
	AuthPassword    Auth = "password"
	AuthKey         Auth = "key"
	AuthAsk         Auth = "ask"
	AuthAnonymous   Auth = "anonymous"
	AuthInteractive Auth = "interactive"
	AuthAccount     Auth = "account"
	AuthAccessKeys  Auth = "access-keys"
	AuthOAuth       Auth = "oauth"
)

// Capabilities are what the storage of a connector can do, which features adapt to.
type Capabilities struct {
	// Folders says the storage has real folders; object stores only have prefixes.
	Folders bool
	// Permissions says files have modes that can be read and changed.
	Permissions bool
	// SetTimes says the time of a file can be set.
	SetTimes bool
	// Resume says a write can continue at an offset.
	Resume bool
	// Rename says an entry can be renamed without copying it.
	Rename bool
	// ServerCopy says the storage copies files by itself.
	ServerCopy bool
}

// Descriptor tells everything about a connector.
type Descriptor struct {
	// Type is its stable identifier, such as sftp or s3.
	Type string
	// Name is how it is shown.
	Name string
	// Icon names the icon it is shown with.
	Icon string
	// Maturity tells how far it is along.
	Maturity Maturity
	// Capabilities are what its storage can do.
	Capabilities Capabilities
	// Fields are its settings.
	Fields []Field
	// Auth are the ways it authenticates.
	Auth []Auth
	// EnabledByDefault says a new installation offers it without an admin enabling it.
	EnabledByDefault bool
}

var (
	// ErrDuplicate means a connector of that type is already in the catalog.
	ErrDuplicate = errors.New("a connector of that type is already registered")
	// ErrUnknown means no connector of that type is in the catalog.
	ErrUnknown = errors.New("no connector of that type")
	// ErrInvalidConfig means a configuration does not follow the schema of its connector.
	ErrInvalidConfig = errors.New("the configuration does not follow the connector")
)

// FieldError is an [ErrInvalidConfig] of one field, which the answer to the client names.
type FieldError struct {
	// Field is the name of the setting.
	Field string
	// Reason says what is wrong: required, unknown, number, range, boolean, choice or length.
	Reason string
}

// Error implements [error].
func (e *FieldError) Error() string {
	return fmt.Sprintf("%v: %s is %s", ErrInvalidConfig, e.Field, e.Reason)
}

// Is matches [ErrInvalidConfig].
func (e *FieldError) Is(target error) bool { return target == ErrInvalidConfig }

// Catalog holds the connectors Sentinel is built with. It is safe for concurrent use.
type Catalog struct {
	mu          sync.RWMutex
	descriptors map[string]Descriptor
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{descriptors: map[string]Descriptor{}}
}

// Register adds a connector, refusing a type already registered or a descriptor without type or name.
func (c *Catalog) Register(d Descriptor) error {
	if d.Type == "" || d.Name == "" {
		return fmt.Errorf("%w: a connector needs a type and a name", ErrInvalidConfig)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.descriptors[d.Type]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicate, d.Type)
	}
	c.descriptors[d.Type] = d
	return nil
}

// Lookup returns the connector of a type.
func (c *Catalog) Lookup(kind string) (Descriptor, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.descriptors[kind]
	if !ok {
		return Descriptor{}, fmt.Errorf("%w: %s", ErrUnknown, kind)
	}
	return d, nil
}

// All returns every connector, stable ones first, each group sorted by name.
func (c *Catalog) All() []Descriptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	all := make([]Descriptor, 0, len(c.descriptors))
	for _, d := range c.descriptors {
		all = append(all, d)
	}
	slices.SortFunc(all, func(a, b Descriptor) int {
		if (a.Maturity == Stable) != (b.Maturity == Stable) {
			if a.Maturity == Stable {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return all
}

// Validate checks a configuration against the fields of a connector and returns it with the defaults filled in. Settings the connector
// does not have are refused, so a typo is not silently ignored.
func (d Descriptor) Validate(config map[string]string) (map[string]string, error) {
	known := map[string]Field{}
	for _, f := range d.Fields {
		known[f.Name] = f
	}
	for name := range config {
		if _, ok := known[name]; !ok {
			return nil, &FieldError{Field: name, Reason: "unknown"}
		}
	}
	out := map[string]string{}
	for _, f := range d.Fields {
		value, given := config[f.Name]
		if !given || value == "" {
			if f.Required && f.Default == "" {
				return nil, &FieldError{Field: f.Name, Reason: "required"}
			}
			value = f.Default
		}
		if value != "" {
			if reason := f.check(value); reason != "" {
				return nil, &FieldError{Field: f.Name, Reason: reason}
			}
		}
		out[f.Name] = value
	}
	return out, nil
}

// check tells what is wrong with a value of a field, or nothing.
func (f Field) check(value string) string {
	switch f.Kind {
	case Number:
		n, err := strconv.Atoi(value)
		if err != nil {
			return "number"
		}
		if f.Max > 0 && (n < f.Min || n > f.Max) {
			return "range"
		}
	case Boolean:
		if value != "true" && value != "false" {
			return "boolean"
		}
	case Choice:
		if !slices.Contains(f.Choices, value) {
			return "choice"
		}
	default:
		if f.MaxLength > 0 && len(value) > f.MaxLength {
			return "length"
		}
	}
	return ""
}
