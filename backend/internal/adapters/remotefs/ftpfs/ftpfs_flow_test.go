package ftpfs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs"
	"github.com/fprambs/transferly/internal/adapters/remotefs/ftpfs/ftptest"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// bigFile is large enough that a transfer is still running when the test acts on it.
const bigFile = 64 << 20

// TestFS_Cancellation verifies that canceling the context of a transfer in flight ends it.
//
// Scenarios:
//   - canceling a download cuts it, closing it reports the cancellation, and the connection is then unusable
//   - canceling an upload cuts it in the same way
func TestFS_Cancellation(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{})
	if err := afero.WriteFile(server.FS, "/big", bytes.Repeat([]byte("x"), bigFile), 0o644); err != nil {
		t.Fatal(err)
	}

	fsys, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	reader, err := fsys.OpenRead(ctx, "/big", 0)
	if err != nil {
		t.Fatalf("OpenRead() = %v", err)
	}
	if _, err := io.ReadFull(reader, make([]byte, 1024)); err != nil {
		t.Fatalf("first read = %v", err)
	}
	cancel()
	if _, err := io.Copy(io.Discard, reader); err == nil {
		t.Error("reading after cancel reached the end without an error")
	}
	if err := reader.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("Close after cancel = %v; want context.Canceled", err)
	}
	if err := fsys.Ping(t.Context()); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Ping after a cut transfer = %v; want ErrConnectionLost", err)
	}

	uploader, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	writer, err := uploader.OpenWrite(ctx, "/up", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatalf("OpenWrite() = %v", err)
	}
	if _, err := writer.Write(bytes.Repeat([]byte("y"), 1024)); err != nil {
		t.Fatalf("first write = %v", err)
	}
	cancel()
	if err := writer.Close(); !errors.Is(err, context.Canceled) {
		t.Errorf("Close after cancel = %v; want context.Canceled", err)
	}
	if _, err := uploader.List(t.Context(), "/"); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("List after a cut upload = %v; want ErrConnectionLost", err)
	}
}

// TestFS_OneCommandAtATime verifies that operations take turns on the control connection and honor their context while waiting.
//
// Scenarios:
//   - while a download is open another operation waits, and gives up with its own context error
//   - once the download is closed the waiting kind of operation works, and closing twice is harmless
func TestFS_OneCommandAtATime(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{})
	if err := afero.WriteFile(server.FS, "/f", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := fsys.OpenRead(t.Context(), "/f", 0)
	if err != nil {
		t.Fatal(err)
	}

	waiting, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := fsys.List(waiting, "/"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("List while a download is open = %v; want context.DeadlineExceeded", err)
	}
	if got, err := io.ReadAll(reader); err != nil || string(got) != "hello" {
		t.Fatalf("download = %q, %v", got, err)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Errorf("second Close = %v", err)
	}
	if entries, err := fsys.List(t.Context(), "/"); err != nil || len(entries) != 1 {
		t.Errorf("List after the download = %v, %v", entries, err)
	}
}

// TestFS_ServerGone verifies what happens when the server disappears.
//
// Scenarios:
//   - every operation on a connection whose server stopped reports a retryable lost connection
//   - a download the server cut half way ends early, and closing it reports the lost connection so it never looks complete
func TestFS_ServerGone(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{})
	if err := afero.WriteFile(server.FS, "/big", bytes.Repeat([]byte("x"), bigFile), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := fsys.OpenRead(t.Context(), "/big", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(reader, make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}

	server.Stop()
	_, _ = io.Copy(io.Discard, reader)
	if err := reader.Close(); !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("Close of a download the server cut = %v; want ErrConnectionLost, so a short file never looks complete", err)
	}

	for name, err := range map[string]error{
		"Ping":  fsys.Ping(t.Context()),
		"List":  second(fsys.List(t.Context(), "/")),
		"Stat":  second(fsys.Stat(t.Context(), "/big")),
		"Mkdir": fsys.Mkdir(t.Context(), "/d"),
	} {
		if !errors.Is(err, remotefs.ErrConnectionLost) || !remotefs.Retryable(err) {
			t.Errorf("%s on a stopped server = %v; want a retryable ErrConnectionLost", name, err)
		}
	}
}

// TestFS_ServerFaults verifies how failures of the server are reported.
//
// Scenarios:
//   - a server that refuses to describe, read, write, create, remove or rename a path gives a permission error, not a crash
//   - a failed upload is reported when it is closed, and a failed listing of a directory that exists keeps its own error
//   - the state probes that refine an error do not hide it when they fail too
func TestFS_ServerFaults(t *testing.T) {
	denied := func(op string) func(string, string) error {
		return func(got, _ string) error {
			if got == op {
				return os.ErrPermission
			}
			return nil
		}
	}
	for _, op := range []string{"stat", "read", "write", "mkdir", "remove", "rename"} {
		server := ftptest.Start(t, ftptest.Options{Fault: denied(op)})
		fsys, err := connect(t, server, ftptest.Plain, nil)
		if err != nil {
			t.Fatal(err)
		}
		setup := func() {
			t.Helper()
			if err := afero.WriteFile(server.FS, "/f", []byte("data"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		setup()

		switch op {
		case "stat":
			_, err = fsys.Stat(t.Context(), "/f")
			if err == nil {
				t.Errorf("Stat with a failing server = nil")
			}
			_, err = fsys.List(t.Context(), "/")
			_ = err
			err = fsys.Mkdir(t.Context(), "/d")
			_ = err
		case "read":
			_, err = fsys.OpenRead(t.Context(), "/f", 0)
			if !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("OpenRead with a failing server = %v; want ErrPermission", err)
			}
		case "write":
			w, openErr := fsys.OpenWrite(t.Context(), "/new", 0, remotefs.WriteOverwrite)
			if openErr != nil {
				t.Fatalf("OpenWrite = %v", openErr)
			}
			_, _ = w.Write([]byte("x"))
			if err := w.Close(); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("Close of a refused upload = %v; want ErrPermission", err)
			}
			if _, err := w.Write([]byte("more")); err == nil {
				t.Error("Write after a refused upload = nil")
			}
		case "mkdir":
			if err := fsys.Mkdir(t.Context(), "/d"); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("Mkdir with a failing server = %v; want ErrPermission", err)
			}
		case "remove":
			if err := fsys.Remove(t.Context(), "/f"); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("Remove with a failing server = %v; want ErrPermission", err)
			}
			if err := fsys.RemoveAll(t.Context(), "/f"); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("RemoveAll with a failing server = %v; want ErrPermission", err)
			}
		case "rename":
			if err := fsys.Rename(t.Context(), "/f", "/g"); !errors.Is(err, remotefs.ErrPermission) {
				t.Errorf("Rename with a failing server = %v; want ErrPermission", err)
			}
		}
	}
}

// TestFS_ListingTimes verifies the zone of LIST times.
//
// Scenarios:
//   - a server with only LIST reports times without a zone, which are read in the zone of the configuration
//   - the same server read in another zone gives a time that many hours apart, and both are UTC with minute or day precision
//   - a server with MLSD needs no zone and reports second precision
func TestFS_ListingTimes(t *testing.T) {
	modified := time.Now().UTC().Add(-time.Hour)
	list := func(opt ftptest.Options, zone *time.Location) remotefs.Entry {
		t.Helper()
		server := ftptest.Start(t, opt)
		if err := afero.WriteFile(server.FS, "/f", []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := server.FS.Chtimes("/f", modified, modified); err != nil {
			t.Fatal(err)
		}
		fsys, err := connect(t, server, ftptest.Plain, func(c *ftpfs.Config) { c.Location = zone })
		if err != nil {
			t.Fatal(err)
		}
		entries, err := fsys.List(t.Context(), "/")
		if err != nil || len(entries) != 1 {
			t.Fatalf("List = %v, %v", entries, err)
		}
		return entries[0]
	}

	utc := list(ftptest.Options{DisableMLSD: true}, nil)
	east := list(ftptest.Options{DisableMLSD: true}, time.FixedZone("UTC+3", 3*3600))
	if utc.ModTimePrecision == remotefs.PrecisionSecond || utc.ModTime.Location() != time.UTC {
		t.Errorf("LIST entry = %+v; want minute or day precision in UTC", utc)
	}
	if got := utc.ModTime.Sub(east.ModTime); got != 3*time.Hour {
		t.Errorf("the same listing read in UTC+3 differs by %v from UTC; want 3h", got)
	}
	precise := list(ftptest.Options{}, nil)
	if precise.ModTimePrecision != remotefs.PrecisionSecond || precise.ModTime.Sub(modified).Abs() > time.Second {
		t.Errorf("MLSD entry = %+v; want the exact modification time", precise)
	}
}

// TestFS_Metadata verifies the optional features.
//
// Scenarios:
//   - permissions cannot be changed and resuming is only possible at the end of a file
//   - a server without MFMT cannot change times and says so, while one with it can
func TestFS_Metadata(t *testing.T) {
	without := ftptest.Start(t, ftptest.Options{DisableMFMT: true})
	fsys, err := connect(t, without, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if caps := fsys.Capabilities(); caps.Chtimes || caps.Chmod || caps.Rewrite || !caps.Resume {
		t.Errorf("Capabilities() = %+v; want resume only", caps)
	}
	if err := fsys.Chtimes(t.Context(), "/f", time.Now()); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Chtimes without MFMT = %v; want ErrInvalid", err)
	}
	if err := fsys.Chmod(t.Context(), "/f", 0o600); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("Chmod = %v; want ErrInvalid", err)
	}

	with := ftptest.Start(t, ftptest.Options{})
	fsys, err = connect(t, with, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fsys.Capabilities().Chtimes {
		t.Error("Capabilities().Chtimes = false on a server with MFMT")
	}
}

// TestFS_ProbeFailures verifies that the lookups which refine an error do not hide it when they fail too.
//
// Scenarios:
//   - when the server refuses to describe the path or its neighbors, every operation reports the refusal instead of guessing
//   - a directory that cannot be removed says so, and one that is not a directory cannot be listed
func TestFS_ProbeFailures(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{Fault: func(op, name string) error {
		if op == "stat" && (name == "/deny" || name == "/deny/x" || name == "/to") {
			return os.ErrPermission
		}
		if op == "remove" && name == "/empty" {
			return os.ErrPermission
		}
		return nil
	}})
	for _, dir := range []string{"/empty", "/other"} {
		if err := server.FS.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := afero.WriteFile(server.FS, "/from", []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	denied := map[string]error{
		"Stat":                second(fsys.Stat(ctx, "/deny")),
		"OpenRead":            second(fsys.OpenRead(ctx, "/deny", 0)),
		"OpenWrite":           second(fsys.OpenWrite(ctx, "/deny", 0, remotefs.WriteOverwrite)),
		"OpenWrite below":     second(fsys.OpenWrite(ctx, "/deny/x", 0, remotefs.WriteOverwrite)),
		"Remove":              fsys.Remove(ctx, "/deny"),
		"RemoveAll":           fsys.RemoveAll(ctx, "/deny"),
		"Rename from":         fsys.Rename(ctx, "/deny", "/y"),
		"Rename to":           fsys.Rename(ctx, "/from", "/to"),
		"Mkdir below":         fsys.Mkdir(ctx, "/deny/x"),
		"Rename below":        fsys.Rename(ctx, "/from", "/deny/x"),
		"Remove a stuck dir":  fsys.Remove(ctx, "/empty"),
		"Chtimes of a hidden": fsys.Chtimes(ctx, "/deny", time.Now()),
	}
	for name, err := range denied {
		if err == nil {
			t.Errorf("%s with a server that refuses to describe paths = nil; want the refusal", name)
		}
	}
	if err := fsys.Remove(ctx, "/empty"); !errors.Is(err, remotefs.ErrPermission) {
		t.Errorf("Remove of a directory the server refuses to remove = %v; want ErrPermission", err)
	}
}

// TestFS_StoppedByContext verifies that a context that ends in the middle of an operation cuts it.
//
// Scenarios:
//   - a lookup that never finishes is abandoned when its context is canceled, for reads, writes, and plain operations
//   - the connection is unusable afterwards
func TestFS_StoppedByContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	server := ftptest.Start(t, ftptest.Options{Fault: func(op, name string) error {
		if op == "stat" && name == "/slow" {
			<-release
		}
		return nil
	}})

	for name, run := range map[string]func(context.Context, *ftpfs.FS) error{
		"Stat":     func(ctx context.Context, f *ftpfs.FS) error { return second(f.Stat(ctx, "/slow")) },
		"OpenRead": func(ctx context.Context, f *ftpfs.FS) error { return second(f.OpenRead(ctx, "/slow", 0)) },
		"OpenWrite": func(ctx context.Context, f *ftpfs.FS) error {
			return second(f.OpenWrite(ctx, "/slow", 0, remotefs.WriteOverwrite))
		},
	} {
		fsys, err := connect(t, server, ftptest.Plain, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		err = run(ctx, fsys)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s stopped by its context = %v; want context.DeadlineExceeded", name, err)
		}
		if err := fsys.Ping(t.Context()); !errors.Is(err, remotefs.ErrConnectionLost) {
			t.Errorf("%s: Ping afterwards = %v; want ErrConnectionLost", name, err)
		}
	}
}

// TestFS_StreamEdges verifies the corners of the transfer streams.
//
// Scenarios:
//   - a negative read offset is refused, and a write stream can be closed twice
//   - an operation that waits for its turn while the file system is closed reports the lost connection
func TestFS_StreamEdges(t *testing.T) {
	server := ftptest.Start(t, ftptest.Options{})
	if err := afero.WriteFile(server.FS, "/f", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys, err := connect(t, server, ftptest.Plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.OpenRead(t.Context(), "/f", -1); !errors.Is(err, remotefs.ErrInvalid) {
		t.Errorf("OpenRead with a negative offset = %v; want ErrInvalid", err)
	}
	writer, err := fsys.OpenWrite(t.Context(), "/g", 0, remotefs.WriteOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("x"))
	if err := writer.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Errorf("second Close = %v", err)
	}

	reader, err := fsys.OpenRead(t.Context(), "/f", 0)
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		_, err := fsys.List(t.Context(), "/")
		waiting <- err
	}()
	time.Sleep(100 * time.Millisecond)
	_ = fsys.Close()
	_ = reader.Close()
	if err := <-waiting; !errors.Is(err, remotefs.ErrConnectionLost) {
		t.Errorf("List that waited while the file system closed = %v; want ErrConnectionLost", err)
	}
}
