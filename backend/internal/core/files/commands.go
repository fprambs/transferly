package files

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/fprambs/transferly/internal/core/remotefs"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// WithCommands returns the service with the rule that says whether connections may work on their servers by running commands
// there (ADR-0027); allowed is asked on every operation, so an administrator's change applies at once. Without it they may.
func (s *Service) WithCommands(allowed func() bool) *Service {
	s.commands = allowed
	return s
}

// commandsOf returns the connection as one that works on its server, when it is one and the instance allows it.
func (s *Service) commandsOf(fsys remotefs.RemoteFS) (remotefs.Commands, bool) {
	if s.commands != nil && !s.commands() {
		return nil, false
	}
	c, ok := fsys.(remotefs.Commands)
	return c, ok
}

// Tools reports what the server of a connection can do on its own, nothing when it can do nothing or the instance does not allow
// it. Asking needs only to browse.
func (s *Service) Tools(ctx context.Context, t Target) (remotefs.Tools, error) {
	return run(ctx, s, t, workspace.ActionBrowse, nil, func(fsys remotefs.RemoteFS) (remotefs.Tools, error) {
		c, ok := s.commandsOf(fsys)
		if !ok {
			return remotefs.Tools{}, nil
		}
		return c.Tools(ctx)
	})
}

// requireCommands returns the connection as one that works on its server, or [remotefs.ErrUnsupported].
func (s *Service) requireCommands(fsys remotefs.RemoteFS) (remotefs.Commands, error) {
	c, ok := s.commandsOf(fsys)
	if !ok {
		return nil, fmt.Errorf("%w: the server of this connection cannot run commands", remotefs.ErrUnsupported)
	}
	return c, nil
}

// Usage returns the bytes a file or folder takes on the disk of its server, which measures it. Asking needs only to browse.
func (s *Service) Usage(ctx context.Context, t Target, p string) (int64, error) {
	return run(ctx, s, t, workspace.ActionBrowse, []string{p}, func(fsys remotefs.RemoteFS) (int64, error) {
		c, err := s.requireCommands(fsys)
		if err != nil {
			return 0, err
		}
		return c.ServerUsage(ctx, p)
	})
}

// archiveSuffixes are the extensions of the archives a server packs and unpacks, by format.
var archiveSuffixes = map[string][]string{remotefs.FormatZip: {".zip"}, remotefs.FormatTarGz: {".tar.gz", ".tgz"}}

// FormatOf returns the archive format a file name says, or empty when it is not an archive a server unpacks.
func FormatOf(name string) string {
	format, _ := formatOf(name)
	return format
}

// formatOf returns the archive format a file name says and the length of its extension, or empty when it is not an archive a
// server unpacks.
func formatOf(name string) (string, int) {
	lower := strings.ToLower(name)
	for format, suffixes := range archiveSuffixes {
		for _, suffix := range suffixes {
			if strings.HasSuffix(lower, suffix) && len(lower) > len(suffix) {
				return format, len(suffix)
			}
		}
	}
	return "", 0
}

// Compress packs the entries names of the folder dir into a new archive named name in the same folder, in format, on the
// server. It needs to upload, and answers the archive.
func (s *Service) Compress(ctx context.Context, t Target, dir string, names []string, name, format string) (remotefs.Entry, error) {
	if _, ok := archiveSuffixes[format]; !ok {
		return remotefs.Entry{}, fmt.Errorf("%w: unknown archive format %q", remotefs.ErrInvalid, format)
	}
	if !safeName(name) || FormatOf(name) != format {
		return remotefs.Entry{}, fmt.Errorf("%w: %q is not a name for a %s archive", remotefs.ErrInvalid, name, format)
	}
	for _, n := range names {
		if !safeName(n) {
			return remotefs.Entry{}, fmt.Errorf("%w: %q is not a name in the folder", remotefs.ErrInvalid, n)
		}
	}
	target := path.Join(dir, name)
	return run(ctx, s, t, workspace.ActionUpload, []string{dir, target}, func(fsys remotefs.RemoteFS) (remotefs.Entry, error) {
		c, err := s.requireCommands(fsys)
		if err != nil {
			return remotefs.Entry{}, err
		}
		if err := c.ServerArchive(ctx, dir, names, target, format); err != nil {
			return remotefs.Entry{}, err
		}
		return fsys.Stat(ctx, target)
	})
}

// Extract unpacks an archive on the server into a new folder next to it, named after it: "photos.zip" into "photos", or
// "photos (1)" when that name is taken. It needs to upload, and answers the folder.
func (s *Service) Extract(ctx context.Context, t Target, archive string) (remotefs.Entry, error) {
	format, suffix := formatOf(path.Base(archive))
	if format == "" {
		return remotefs.Entry{}, fmt.Errorf("%w: %q is not a ZIP or tar.gz archive", remotefs.ErrInvalid, path.Base(archive))
	}
	folder := archive[:len(archive)-suffix]
	if err := s.Check(ctx, t, workspace.ActionUpload, archive, folder); err != nil {
		return remotefs.Entry{}, err
	}
	if _, err := s.Stat(ctx, t, folder); err == nil {
		if folder, err = s.freeName(ctx, t, folder); err != nil {
			return remotefs.Entry{}, err
		}
	} else if !isNotFound(err) {
		return remotefs.Entry{}, err
	}
	return run(ctx, s, t, workspace.ActionUpload, []string{archive, folder}, func(fsys remotefs.RemoteFS) (remotefs.Entry, error) {
		c, err := s.requireCommands(fsys)
		if err != nil {
			return remotefs.Entry{}, err
		}
		if err := fsys.Mkdir(ctx, folder); err != nil {
			return remotefs.Entry{}, err
		}
		if err := c.ServerExtract(ctx, archive, format, folder); err != nil {
			_ = fsys.RemoveAll(context.WithoutCancel(ctx), folder)
			return remotefs.Entry{}, err
		}
		return fsys.Stat(ctx, folder)
	})
}

// serverChecksum asks the server of a connection for the digest of a file. It reports false when the server cannot, so the
// caller reads the file itself.
func (s *Service) serverChecksum(ctx context.Context, fsys remotefs.RemoteFS, p, algorithm string) (string, bool, error) {
	c, ok := s.commandsOf(fsys)
	if !ok {
		return "", false, nil
	}
	sum, err := c.ServerChecksum(ctx, p, algorithm)
	if errors.Is(err, remotefs.ErrUnsupported) {
		return "", false, nil
	}
	return sum, true, err
}

// serverCopy copies a file within one site on its server. It reports false when the server cannot, so the caller moves the
// data itself.
func (s *Service) serverCopy(ctx context.Context, t Target, from, to string, overwrite bool) (bool, error) {
	return run(ctx, s, t, workspace.ActionUpload, []string{from, to}, func(fsys remotefs.RemoteFS) (bool, error) {
		c, ok := s.commandsOf(fsys)
		if !ok {
			return false, nil
		}
		err := c.ServerCopy(ctx, from, to, overwrite)
		if errors.Is(err, remotefs.ErrUnsupported) {
			return false, nil
		}
		return true, err
	})
}
