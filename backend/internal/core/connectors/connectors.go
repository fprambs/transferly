// Package connectors is the catalog of the connectors Transferly is built with (ADR-0019): every kind of storage a site can reach, such as
// SFTP, FTP or S3, described by what it is, what it can do and how it is configured. A site is its connector plus a configuration that
// follows the connector's schema (ADR-0022), so the site form, the validation of the API and the store of the admins follow from these
// descriptors and a new connector touches no other code.
package connectors

import (
	"cmp"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
	// Text is free text, bounded by MaxLength.
	Text FieldKind = "text"
	// Number is a whole number, bounded by Min and Max.
	Number FieldKind = "number"
	// Boolean is true or false.
	Boolean FieldKind = "boolean"
	// Choice is one of Choices.
	Choice FieldKind = "choice"
	// Host is a server name or address the network policy of the instance must allow, together with the port field Port names.
	Host FieldKind = "host"
	// TimeZone is an IANA time zone.
	TimeZone FieldKind = "timezone"
	// Secret is sealed in the vault and never returned; it is given apart from the configuration.
	Secret FieldKind = "secret"
	// PrivateKey is a secret that must be a usable private key, decrypted with the passphrase field Passphrase names.
	PrivateKey FieldKind = "private_key"
	// OAuth is a secret the form fills by signing in with the provider Provider names, such as the refresh token of a Google
	// account, instead of having it typed; the text field Account names keeps which account signed in (ADR-0024).
	OAuth FieldKind = "oauth"
)

// Group is the tab of the site form a field is shown on.
type Group string

// The groups of fields, in the order the form shows them.
const (
	GroupConnection Group = "connection"
	GroupAccess     Group = "access"
	GroupAdvanced   Group = "advanced"
	GroupCharset    Group = "charset"
	GroupProxy      Group = "proxy"
	GroupJump       Group = "jump"
)

// Condition says when a field applies, from the value of a field declared before it.
type Condition struct {
	// Field is the name of the other field.
	Field string
	// In are the values of the other field for which the condition holds; when empty it holds whenever the other field has a value.
	In []string
}

// holds reports whether the condition holds for the values settled so far.
func (c Condition) holds(values map[string]string) bool {
	value := values[c.Field]
	if len(c.In) == 0 {
		return value != ""
	}
	return slices.Contains(c.In, value)
}

// Default is a default value that applies while a condition holds, such as port 990 for implicit FTPS.
type Default struct {
	// When is the condition.
	When Condition
	// Value is the default while it holds.
	Value string
}

// Field is one setting of a connector, from which the site form draws its input and the API validates it.
type Field struct {
	// Name is the key of the setting, in snake case. For secrets it is also the kind of the secret in the vault.
	Name string
	// Kind is its type.
	Kind FieldKind
	// Group is the tab of the form it is shown on.
	Group Group
	// Required says a value must be given, unless a default fills it in.
	Required bool
	// Default is the value used when none is given, or empty.
	Default string
	// Defaults are defaults that depend on other fields; the first whose condition holds wins over Default.
	Defaults []Default
	// Fixed are values the field takes while their condition holds, whatever is given, such as the anonymous user of anonymous sites; the
	// form shows them read only.
	Fixed []Default
	// Choices are the values a choice allows.
	Choices []string
	// Min and Max bound a number, when Max is above zero.
	Min, Max int
	// MaxLength bounds a text or a host in characters, when above zero.
	MaxLength int
	// When, if set, says when the field applies; a field that does not apply is dropped, and so is its stored secret.
	When *Condition
	// Port names the port field of a host.
	Port string
	// Passphrase names the passphrase field of a private key.
	Passphrase string
	// Provider names who an OAuth field signs in with, such as google.
	Provider string
	// Account names the text field of an OAuth field that keeps the account that signed in, if it has one.
	Account string
	// Elevated marks a boolean field that, set to true, lets the site act beyond its account on the server, as sudo does
	// (ADR-0027); only admins may save a site with it on.
	Elevated bool
}

// IsSecret reports whether the field is sealed in the vault instead of being part of the configuration.
func (f Field) IsSecret() bool { return f.Kind == Secret || f.Kind == PrivateKey || f.Kind == OAuth }

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
	// Fields are its settings, each declared after the fields its condition and dependent defaults look at.
	Fields []Field
	// Normalize, if set, applies the rules a schema cannot say to a trimmed configuration before it is validated, such as the anonymous
	// user of anonymous sites. It returns the configuration to validate.
	Normalize func(config map[string]string) map[string]string
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
	// ErrInvalidDescriptor means a descriptor contradicts itself, such as a condition on a field declared after it.
	ErrInvalidDescriptor = errors.New("invalid connector descriptor")
)

// The reasons a [FieldError] gives.
const (
	ReasonRequired = "required"
	ReasonUnknown  = "unknown"
	ReasonSecret   = "secret"
	ReasonNumber   = "number"
	ReasonRange    = "range"
	ReasonBoolean  = "boolean"
	ReasonChoice   = "choice"
	ReasonLength   = "length"
	ReasonHost     = "host"
	ReasonTimeZone = "timezone"
)

// FieldError is an [ErrInvalidConfig] of one field, which the answer to the client names.
type FieldError struct {
	// Field is the name of the setting.
	Field string
	// Reason says what is wrong, one of the Reason constants or a reason a caller adds, such as a private key that cannot be read.
	Reason string
}

// Error implements [error].
func (e *FieldError) Error() string {
	return fmt.Sprintf("%v: %s is %s", ErrInvalidConfig, e.Field, e.Reason)
}

// Is matches [ErrInvalidConfig].
func (e *FieldError) Is(target error) bool { return target == ErrInvalidConfig }

// Catalog holds the connectors Transferly is built with. It is safe for concurrent use.
type Catalog struct {
	mu          sync.RWMutex
	descriptors map[string]Descriptor
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{descriptors: map[string]Descriptor{}}
}

// Register adds a connector, refusing a type already registered or a descriptor that contradicts itself (see [Descriptor.Check]).
func (c *Catalog) Register(d Descriptor) error {
	if err := d.Check(); err != nil {
		return err
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
		return cmp.Or(cmp.Compare(rank(a.Maturity), rank(b.Maturity)), strings.Compare(a.Name, b.Name))
	})
	return all
}

// rank orders maturities: stable connectors before the others.
func rank(m Maturity) int {
	if m == Stable {
		return 0
	}
	return 1
}

// Check reports a descriptor that contradicts itself: no type or name, two fields of one name, a condition or a dependent default that
// looks at a field not declared before, a host whose port field is missing, a private key whose passphrase field is missing or not a
// secret, or an OAuth field without a provider or whose account field is not text.
func (d Descriptor) Check() error {
	if d.Type == "" || d.Name == "" {
		return fmt.Errorf("%w: a connector needs a type and a name", ErrInvalidDescriptor)
	}
	seen := map[string]Field{}
	for _, f := range d.Fields {
		if _, twice := seen[f.Name]; twice || f.Name == "" {
			return fmt.Errorf("%w: field %q is empty or declared twice", ErrInvalidDescriptor, f.Name)
		}
		looked := make([]string, 0, len(f.Defaults)+len(f.Fixed)+1)
		if f.When != nil {
			looked = append(looked, f.When.Field)
		}
		for _, def := range append(slices.Clone(f.Defaults), f.Fixed...) {
			looked = append(looked, def.When.Field)
		}
		for _, name := range looked {
			if _, before := seen[name]; !before {
				return fmt.Errorf("%w: field %q looks at %q, which is not declared before it", ErrInvalidDescriptor, f.Name, name)
			}
		}
		seen[f.Name] = f
	}
	for _, f := range d.Fields {
		if port, ok := seen[f.Port]; f.Kind == Host && (!ok || port.Kind != Number) {
			return fmt.Errorf("%w: host %q has no port field", ErrInvalidDescriptor, f.Name)
		}
		if phrase, ok := seen[f.Passphrase]; f.Kind == PrivateKey && (!ok || phrase.Kind != Secret) {
			return fmt.Errorf("%w: private key %q has no passphrase field", ErrInvalidDescriptor, f.Name)
		}
		if account, ok := seen[f.Account]; f.Kind == OAuth && (f.Provider == "" || (f.Account != "" && (!ok || account.Kind != Text))) {
			return fmt.Errorf("%w: OAuth field %q has no provider, or its account field is not text", ErrInvalidDescriptor, f.Name)
		}
		if f.Elevated && f.Kind != Boolean {
			return fmt.Errorf("%w: elevated field %q is not a boolean", ErrInvalidDescriptor, f.Name)
		}
	}
	return nil
}

// Elevates reports whether settings turn on a field that lets the site act beyond its account on the server (ADR-0027).
func (d Descriptor) Elevates(config map[string]string) bool {
	return slices.ContainsFunc(d.Fields, func(f Field) bool { return f.Elevated && config[f.Name] == "true" })
}

// Field returns the field of a name.
func (d Descriptor) Field(name string) (Field, bool) {
	i := slices.IndexFunc(d.Fields, func(f Field) bool { return f.Name == name })
	if i < 0 {
		return Field{}, false
	}
	return d.Fields[i], true
}

// Applies reports whether the field applies to a settled configuration.
func (f Field) Applies(config map[string]string) bool {
	return f.When == nil || f.When.holds(config)
}

// Validate checks a configuration against the fields of a connector and returns it settled: values trimmed, the rules of [Descriptor.Normalize]
// applied, fixed values set, the defaults filled in, and the fields that do not apply dropped. Settings the connector does not have are refused, so a typo is not
// silently ignored, and so are secrets, which are given apart and never stored in the configuration.
func (d Descriptor) Validate(config map[string]string) (map[string]string, error) {
	trimmed := make(map[string]string, len(config))
	for name, value := range config {
		trimmed[name] = strings.TrimSpace(value)
	}
	if d.Normalize != nil {
		trimmed = d.Normalize(trimmed)
	}
	for name := range trimmed {
		f, ok := d.Field(name)
		switch {
		case !ok:
			return nil, &FieldError{Field: name, Reason: ReasonUnknown}
		case f.IsSecret():
			return nil, &FieldError{Field: name, Reason: ReasonSecret}
		}
	}
	out := map[string]string{}
	for _, f := range d.Fields {
		if f.IsSecret() || !f.Applies(out) {
			continue
		}
		value := trimmed[f.Name]
		if fixed, ok := f.fixedFor(out); ok {
			value = fixed
		}
		if value == "" {
			value = f.defaultFor(out)
		}
		if value == "" {
			if f.Required {
				return nil, &FieldError{Field: f.Name, Reason: ReasonRequired}
			}
			continue
		}
		if reason := f.check(value); reason != "" {
			return nil, &FieldError{Field: f.Name, Reason: reason}
		}
		out[f.Name] = value
	}
	return out, nil
}

// defaultFor returns the default of a field for the values settled so far: the first dependent default whose condition holds, or Default.
func (f Field) defaultFor(values map[string]string) string {
	for _, def := range f.Defaults {
		if def.When.holds(values) {
			return def.Value
		}
	}
	return f.Default
}

// fixedFor returns the value a field is fixed to for the values settled so far, and whether it is.
func (f Field) fixedFor(values map[string]string) (string, bool) {
	for _, fixed := range f.Fixed {
		if fixed.When.holds(values) {
			return fixed.Value, true
		}
	}
	return "", false
}

// check tells what is wrong with a value of a field, or nothing.
func (f Field) check(value string) string {
	switch f.Kind {
	case Number:
		n, err := strconv.Atoi(value)
		if err != nil {
			return ReasonNumber
		}
		if f.Max > 0 && (n < f.Min || n > f.Max) {
			return ReasonRange
		}
	case Boolean:
		if value != "true" && value != "false" {
			return ReasonBoolean
		}
	case Choice:
		if !slices.Contains(f.Choices, value) {
			return ReasonChoice
		}
	case Host:
		if !ValidHost(value) {
			return ReasonHost
		}
	case TimeZone:
		if _, err := time.LoadLocation(value); err != nil || value == "Local" {
			return ReasonTimeZone
		}
	default:
		if f.MaxLength > 0 && utf8.RuneCountInString(value) > f.MaxLength {
			return ReasonLength
		}
	}
	return ""
}

// maxHostLength is the longest host name, as DNS allows.
const maxHostLength = 253

// labelPattern matches one label of a host name.
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$`)

// ValidHost reports whether host is an IP address or a dotted host name, without scheme, port, path or spaces.
func ValidHost(host string) bool {
	if host == "" || len(host) > maxHostLength {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}
