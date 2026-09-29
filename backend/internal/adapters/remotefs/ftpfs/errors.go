package ftpfs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/textproto"
	"strings"

	"github.com/fprambs/sentinel/internal/core/remotefs"
)

// FTP reply codes the adapter tells apart.
const (
	codeServiceClosing   = 421
	codeCannotOpenData   = 425
	codeTransferAborted  = 426
	codeFileBusy         = 450
	codeLocalError       = 451
	codeNoSpace          = 452
	codeLoginNeedAccount = 332
	codeNotLoggedIn      = 530
	codeNeedAccount      = 532
	codeFileUnavailable  = 550
	codePageTypeUnknown  = 551
	codeQuotaExceeded    = 552
	codeNameNotAllowed   = 553
	codeAuthNotSupported = 534
)

// classify maps an error of the FTP client to the sentinels of [remotefs]. A reply of the server is judged by its code
// and, for the ambiguous 550, by its text; anything that is not a reply, such as a closed connection or a timeout, is a
// transport failure.
func classify(err error) error {
	var reply *textproto.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, remotefs.ErrAuth):
		return err
	case !errors.As(err, &reply):
		return fmt.Errorf("%w: %w", remotefs.ErrConnectionLost, err)
	}
	switch reply.Code {
	case codeServiceClosing, codeCannotOpenData, codeTransferAborted:
		return fmt.Errorf("%w: %w", remotefs.ErrConnectionLost, err)
	case codeLoginNeedAccount, codeNotLoggedIn, codeNeedAccount:
		return fmt.Errorf("%w: %w", remotefs.ErrAuth, err)
	case codeNoSpace, codeQuotaExceeded:
		return fmt.Errorf("%w: %w", remotefs.ErrQuota, err)
	case codePageTypeUnknown:
		return fmt.Errorf("%w: %w", remotefs.ErrInvalid, err)
	case codeFileUnavailable, codeNameNotAllowed, codeFileBusy, codeLocalError:
		return fmt.Errorf("%w: %w", refine(reply.Msg), err)
	}
	return fmt.Errorf("%w: %w", remotefs.ErrInvalid, err)
}

// refine tells what a "file unavailable" reply means from its text, since FTP uses one code for missing paths,
// existing paths, non-empty directories and denied access. Without a hint it is a permission problem, the most common cause.
func refine(message string) error {
	text := strings.ToLower(message)
	contains := func(hints ...string) bool {
		for _, hint := range hints {
			if strings.Contains(text, hint) {
				return true
			}
		}
		return false
	}
	switch {
	case contains("not a directory"):
		return remotefs.ErrNotDirectory
	case contains("not empty"):
		return remotefs.ErrNotEmpty
	case contains("no such", "not found", "does not exist", "not exist", "doesn't exist", "cannot find", "can't find", "no file"):
		return remotefs.ErrNotFound
	case contains("already exist", "file exists", "exists"):
		return remotefs.ErrExists
	}
	return remotefs.ErrPermission
}

// fingerprintOf renders the SHA-256 fingerprint of a DER certificate in the notation Sentinel uses for host keys.
func fingerprintOf(der []byte) string {
	digest := sha256.Sum256(der)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(digest[:]), "=")
}
