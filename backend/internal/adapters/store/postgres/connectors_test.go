package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestConnectorStore verifies the settings administrators give the connectors of the instance (ADR-0019).
//
// Use cases:
//   - no settings at first; a stored one is read back, and storing it again replaces it, also without a known author
//   - both operations return the database error on a closed pool
func TestConnectorStore(t *testing.T) {
	ctx := context.Background()
	store := openMigrated(t)
	settings, err := store.ConnectorSettings(ctx)
	if err != nil || len(settings) != 0 {
		t.Fatalf("ConnectorSettings() = %v, %v; want none", settings, err)
	}
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if err := store.SetConnector(ctx, "ftp", false, uuid.Nil, at); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnector(ctx, "ftp", true, uuid.Nil, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnector(ctx, "sftp", false, uuid.Nil, at); err != nil {
		t.Fatal(err)
	}
	if settings, err = store.ConnectorSettings(ctx); err != nil || len(settings) != 2 || !settings["ftp"] || settings["sftp"] {
		t.Errorf("ConnectorSettings() = %v, %v; want ftp on and sftp off", settings, err)
	}

	store.Close()
	if _, err := store.ConnectorSettings(ctx); err == nil {
		t.Error("ConnectorSettings() hid the database error")
	}
	if err := store.SetConnector(ctx, "ftp", true, uuid.Nil, at); err == nil {
		t.Error("SetConnector() hid the database error")
	}
}
