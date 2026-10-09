package files

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/workspace"
	"github.com/google/uuid"
)

// lockAuthorizer admits the operation so tests can isolate lock admission from remote I/O.
type lockAuthorizer struct{}

// Authorize admits a fixture actor without consulting a database.
func (lockAuthorizer) Authorize(context.Context, uuid.UUID, uuid.UUID, workspace.Action) (workspace.Role, error) {
	return workspace.RoleEditor, nil
}

// TestCopyLockCancellation verifies service cancellation before remote I/O and cleanup after partial pair acquisition.
func TestCopyLockCancellation(t *testing.T) {
	service := NewService(lockAuthorizer{}, nil)
	location := CopyLocation{SourcePath: "/source", DestinationPath: "/target"}
	key := uuid.Nil.String() + uuid.Nil.String() + uuid.Nil.String()
	release, err := service.copies.acquire(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Copy(ctx, location, PolicyFail); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	if len(service.copies.keys) != 0 {
		t.Fatal("canceled service admission retained a lock")
	}

	location.Source.Site = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	location.Destination.Site = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	key = uuid.Nil.String() + uuid.Nil.String() + location.Destination.Site.String()
	release, err = service.copies.acquire(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.copies.pair(ctx, location)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		service.copies.mu.Lock()
		waiting := service.copies.keys[key].users == 2
		service.copies.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pair did not reach its second lock")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(service.copies.keys) != 1 {
		t.Fatal("first reservation leaked after cancellation")
	}
	release()
	if len(service.copies.keys) != 0 {
		t.Fatal("unused lock keys retained")
	}
}
