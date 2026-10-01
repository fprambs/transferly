package sftpfs

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/pkg/sftp"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

const (
	statusFailure        = 4
	statusNoConnection   = 6
	statusConnectionLost = 7
	statusQuotaExceeded  = 15
)

// statusCode returns the SFTP status code carried by err, if any.
func statusCode(err error) (uint32, bool) {
	var status *sftp.StatusError
	if errors.As(err, &status) {
		return status.Code, true
	}
	return 0, false
}

// isFailure reports whether err is the generic failure status, which servers use for conditions such as
// "already exists", "not a directory" or "directory not empty" that SFTP version 3 cannot tell apart.
func isFailure(err error) bool {
	code, ok := statusCode(err)
	return ok && code == statusFailure
}

// classify maps an error of the SFTP client to the sentinels of [remotefs]. An error that is not a status of the
// server, such as a closed connection or an unexpected end of stream, is a transport failure. Generic failure
// statuses and other statuses are returned as they are, for the caller to refine.
func classify(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %w", remotefs.ErrNotFound, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %w", remotefs.ErrPermission, err)
	}
	code, isStatus := statusCode(err)
	switch {
	case isStatus && code == statusQuotaExceeded:
		return fmt.Errorf("%w: %w", remotefs.ErrQuota, err)
	case isStatus && (code == statusNoConnection || code == statusConnectionLost):
		return fmt.Errorf("%w: %w", remotefs.ErrConnectionLost, err)
	case isStatus:
		return err
	}
	return fmt.Errorf("%w: %w", remotefs.ErrConnectionLost, err)
}
