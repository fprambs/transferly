package files_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/files"
	"github.com/fprambs/sentinel/internal/core/remotefs"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// otherSite returns the target moved to another site of the same workspace.
func otherSite(t files.Target) files.Target {
	t.Site = uuid.New()
	return t
}

// TestService_Copy verifies copying a file from one site to another through the server.
//
// Scenarios:
//   - the content arrives at the destination, the entry returned is the copy, and the source stays
//   - the destination observer learns the size of the source at the start and counts every byte
//   - create refuses an existing destination and overwrite replaces it
//   - copying a file onto itself, a directory, a missing source and an unusable path are refused
//   - a viewer cannot write the destination and an unknown user cannot read the source
//   - a source that breaks midway and a destination that fails to commit report the error
//   - a canceled context stops the copy
func TestService_Copy(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.put(t, "/src.txt", "0123456789")
	obs := &recorder{}
	l := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/dst.txt"}
	l.Destination.Observer = obs

	entry, err := f.svc.Copy(ctx, l, remotefs.WriteCreate)
	if err != nil || entry.Size != 10 || entry.Name != "dst.txt" {
		t.Fatalf("Copy() = %+v, %v", entry, err)
	}
	if len(obs.started) != 1 || obs.started[0] != 10 || obs.bytes != 10 {
		t.Errorf("observer saw %v started and %d bytes; want the total of 10 and 10 bytes", obs.started, obs.bytes)
	}
	d, err := f.svc.Download(ctx, f.editor, "/dst.txt", files.Range{})
	if err != nil || readAll(t, d) != "0123456789" {
		t.Errorf("the copy holds other data: %v", err)
	}
	if _, err := f.svc.Stat(ctx, f.editor, "/src.txt"); err != nil {
		t.Errorf("the source is gone: %v", err)
	}

	if _, err := f.svc.Copy(ctx, l, remotefs.WriteCreate); !errors.Is(err, remotefs.ErrExists) {
		t.Errorf("Copy() onto an existing file = %v; want ErrExists", err)
	}
	f.put(t, "/src.txt", "new")
	if _, err := f.svc.Copy(ctx, l, remotefs.WriteOverwrite); err != nil {
		t.Errorf("Copy() overwriting = %v", err)
	}
	if got, _ := f.svc.Stat(ctx, f.editor, "/dst.txt"); got.Size != 3 {
		t.Errorf("the overwritten copy has %d bytes; want 3", got.Size)
	}

	same := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: f.editor, DestinationPath: "/src.txt"}
	if _, err := f.svc.Copy(ctx, same, remotefs.WriteOverwrite); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Copy() onto itself = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.Mkdir(ctx, f.editor, "/dir"); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]files.CopyLocation{
		"directory":    {Source: f.editor, SourcePath: "/dir", Destination: otherSite(f.editor), DestinationPath: "/x"},
		"missing":      {Source: f.editor, SourcePath: "/nothing", Destination: otherSite(f.editor), DestinationPath: "/x"},
		"empty source": {Source: f.editor, SourcePath: "", Destination: otherSite(f.editor), DestinationPath: "/x"},
		"empty target": {Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: ""},
	} {
		if _, err := f.svc.Copy(ctx, bad, remotefs.WriteCreate); err == nil {
			t.Errorf("Copy() of a %s did not fail", name)
		}
	}

	viewer := files.CopyLocation{Source: f.viewer, SourcePath: "/src.txt", Destination: otherSite(f.viewer), DestinationPath: "/v.txt"}
	if _, err := f.svc.Copy(ctx, viewer, remotefs.WriteCreate); !errors.Is(err, workspace.ErrForbidden) {
		t.Errorf("Copy() by a viewer = %v; want ErrForbidden", err)
	}
	stranger := files.CopyLocation{Source: files.Target{Actor: uuid.New(), Workspace: f.editor.Workspace, Site: f.editor.Site, Session: "s"}, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/s.txt"}
	if _, err := f.svc.Copy(ctx, stranger, remotefs.WriteCreate); !errors.Is(err, workspace.ErrNotFound) {
		t.Errorf("Copy() by an unknown user = %v; want ErrNotFound", err)
	}

	f.fs.failRead = true
	broken := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/broken.txt"}
	if _, err := f.svc.Copy(ctx, broken, remotefs.WriteCreate); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Copy() of a source that breaks = %v; want ErrConnectionLost", err)
	}
	f.fs.failRead, f.fs.failCommit = false, true
	commit := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/commit.txt"}
	if _, err := f.svc.Copy(ctx, commit, remotefs.WriteCreate); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Copy() that fails to commit = %v; want ErrConnectionLost", err)
	}
	f.fs.failCommit = false

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	late := files.CopyLocation{Source: f.editor, SourcePath: "/src.txt", Destination: otherSite(f.editor), DestinationPath: "/late.txt"}
	if _, err := f.svc.Copy(canceled, late, remotefs.WriteCreate); !errors.Is(err, context.Canceled) {
		t.Errorf("Copy() with a canceled context = %v; want context.Canceled", err)
	}
}
