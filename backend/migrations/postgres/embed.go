// Package postgres embeds the goose migrations for PostgreSQL.
package postgres

import "embed"

// FS holds the SQL migration files at its root.
//
//go:embed *.sql
var FS embed.FS
