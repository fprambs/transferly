package httpapi

import (
	"context"

	"github.com/fprambs/sentinel/internal/core/connectors"
)

// optional returns a pointer to a value, or nil for its zero value, so a field left out of a descriptor is left out of the answer.
func optional[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}

// connectorDTO converts a descriptor to its public form.
func connectorDTO(d connectors.Descriptor) Connector {
	fields := make([]ConnectorField, len(d.Fields))
	for i, f := range d.Fields {
		fields[i] = ConnectorField{Name: f.Name, Kind: ConnectorFieldKind(f.Kind), Required: f.Required, Default: optional(f.Default), Min: optional(f.Min), Max: optional(f.Max), MaxLength: optional(f.MaxLength)}
		if len(f.Choices) > 0 {
			choices := f.Choices
			fields[i].Choices = &choices
		}
	}
	auth := make([]ConnectorAuth, len(d.Auth))
	for i, a := range d.Auth {
		auth[i] = ConnectorAuth(a)
	}
	c := d.Capabilities
	return Connector{
		Type: d.Type, Name: d.Name, Icon: d.Icon, Maturity: ConnectorMaturity(d.Maturity), Fields: fields, Auth: auth, EnabledByDefault: d.EnabledByDefault,
		Capabilities: ConnectorCapabilities{Folders: c.Folders, Permissions: c.Permissions, SetTimes: c.SetTimes, Resume: c.Resume, Rename: c.Rename, ServerCopy: c.ServerCopy},
	}
}

// ListConnectors answers the catalog of connectors Sentinel is built with (ADR-0019).
func (s *server) ListConnectors(_ context.Context, _ ListConnectorsRequestObject) (ListConnectorsResponseObject, error) {
	all := s.deps.Connectors.All()
	out := make(ListConnectors200JSONResponse, len(all))
	for i, d := range all {
		out[i] = connectorDTO(d)
	}
	return out, nil
}
