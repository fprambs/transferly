package httpapi

import (
	"context"
	"strconv"

	"github.com/fprambs/transferly/internal/core/audit"
	"github.com/fprambs/transferly/internal/core/connectors"
)

// optional returns a pointer to a value, or nil for its zero value, so a field left out of a descriptor is left out of the answer.
func optional[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}

// conditionDTO converts a condition of a field to its public form, or nil for none.
func conditionDTO(c *connectors.Condition) *ConnectorCondition {
	if c == nil {
		return nil
	}
	out := ConnectorCondition{Field: c.Field}
	if len(c.In) > 0 {
		values := c.In
		out.In = &values
	}
	return &out
}

// fieldDTO converts a field of a connector's schema to its public form.
func fieldDTO(f connectors.Field) ConnectorField {
	out := ConnectorField{
		Name: f.Name, Kind: ConnectorFieldKind(f.Kind), Group: ConnectorFieldGroup(f.Group), Required: f.Required, Default: optional(f.Default),
		Min: optional(f.Min), Max: optional(f.Max), MaxLength: optional(f.MaxLength), When: conditionDTO(f.When), Port: optional(f.Port),
		Passphrase: optional(f.Passphrase),
	}
	if len(f.Choices) > 0 {
		choices := f.Choices
		out.Choices = &choices
	}
	if len(f.Defaults) > 0 {
		defaults := make([]ConnectorDefault, len(f.Defaults))
		for i, d := range f.Defaults {
			defaults[i] = ConnectorDefault{When: *conditionDTO(&d.When), Value: d.Value}
		}
		out.Defaults = &defaults
	}
	return out
}

// connectorDTO converts a connector of the catalog, and whether the instance offers it, to its public form.
func connectorDTO(e connectors.Entry) Connector {
	d := e.Descriptor
	fields := make([]ConnectorField, len(d.Fields))
	for i, f := range d.Fields {
		fields[i] = fieldDTO(f)
	}
	c := d.Capabilities
	return Connector{
		Type: d.Type, Name: d.Name, Icon: d.Icon, Maturity: ConnectorMaturity(d.Maturity), Fields: fields, EnabledByDefault: d.EnabledByDefault, Enabled: e.Enabled,
		Capabilities: ConnectorCapabilities{Folders: c.Folders, Permissions: c.Permissions, SetTimes: c.SetTimes, Resume: c.Resume, Rename: c.Rename, ServerCopy: c.ServerCopy},
	}
}

// ListConnectors answers the catalog of connectors Transferly is built with, and whether the instance offers each (ADR-0019).
func (s *server) ListConnectors(ctx context.Context, _ ListConnectorsRequestObject) (ListConnectorsResponseObject, error) {
	entries, err := s.deps.Connectors.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(ListConnectors200JSONResponse, len(entries))
	for i, e := range entries {
		out[i] = connectorDTO(e)
	}
	return out, nil
}

// SetConnectorEnabled enables or disables a connector for the instance, for its administrators, and records it in the audit log.
func (s *server) SetConnectorEnabled(ctx context.Context, request SetConnectorEnabledRequestObject) (SetConnectorEnabledResponseObject, error) {
	p, _ := principalFrom(ctx)
	entry, err := s.deps.Connectors.SetEnabled(ctx, p.User, request.Type, request.Body.Enabled)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.recordInstance(ctx, p.User, audit.AdminConnectorChanged, entry.Type, map[string]string{"enabled": strconv.FormatBool(entry.Enabled)})
	return SetConnectorEnabled200JSONResponse(connectorDTO(entry)), nil
}
