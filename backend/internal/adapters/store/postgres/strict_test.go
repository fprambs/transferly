package postgres_test

import (
	"context"
	"slices"
	"testing"
)

// TestStrictStore verifies the SFTP servers that proved strict, kept across restarts.
//
// Use cases:
//   - none at first; servers remembered are listed in order, and remembering one again keeps it once
//   - both operations return the database error on a closed pool
func TestStrictStore(t *testing.T) {
	ctx := context.Background()
	store := openMigrated(t)
	if servers, err := store.StrictServers(ctx); err != nil || len(servers) != 0 {
		t.Fatalf("StrictServers() = %v, %v; want none", servers, err)
	}
	for _, address := range []string{"gateway.example:26701", "a.example:22", "gateway.example:26701"} {
		if err := store.RememberStrictServer(ctx, address); err != nil {
			t.Fatal(err)
		}
	}
	if servers, err := store.StrictServers(ctx); err != nil || !slices.Equal(servers, []string{"a.example:22", "gateway.example:26701"}) {
		t.Errorf("StrictServers() = %v, %v", servers, err)
	}

	store.Close()
	if _, err := store.StrictServers(ctx); err == nil {
		t.Error("StrictServers() hid the database error")
	}
	if err := store.RememberStrictServer(ctx, "x:22"); err == nil {
		t.Error("RememberStrictServer() hid the database error")
	}
}
