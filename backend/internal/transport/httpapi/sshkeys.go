package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/sshkey"
	"github.com/fprambs/transferly/internal/core/vault"
)

const (
	// maxKeyFileBytes is the largest key file inspected, the same bound saving a site applies.
	maxKeyFileBytes = 64 * 1024
	// maxPassphraseBytes is the longest passphrase checked against a key.
	maxPassphraseBytes = 1024
)

// keyStatuses tells, for the errors that still describe the key, what the user must do next.
var keyStatuses = []struct {
	err    error
	status SshKeyInfoStatus
}{
	{sshkey.ErrPublicKey, PublicKey},
	{sshkey.ErrPassphraseRequired, PassphraseRequired},
	{sshkey.ErrWrongPassphrase, WrongPassphrase},
}

// InspectSshKey describes a key file by its content, and checks the passphrase against it, without storing anything
// (4.8g). A file that is no key, or one Transferly does not use, is answered as saving a site would answer it.
func (s *server) InspectSshKey(_ context.Context, request InspectSshKeyRequestObject) (InspectSshKeyResponseObject, error) {
	var passphrase []byte
	if request.Body.Passphrase != nil {
		passphrase = []byte(*request.Body.Passphrase)
	}
	defer vault.Wipe(passphrase)
	if len(request.Body.Key) > maxKeyFileBytes || len(passphrase) > maxPassphraseBytes {
		return domainProblem{status: http.StatusUnprocessableEntity, code: "secret_too_large"}, nil
	}
	key, err := sshkey.Read([]byte(request.Body.Key), passphrase)
	status := Usable
	for _, k := range keyStatuses {
		if errors.Is(err, k.err) {
			status, err = k.status, nil
		}
	}
	if err != nil {
		return keyProblem(&site.KeyError{Field: string(site.SecretPrivateKey), Err: err}), nil
	}
	info := SshKeyInfo{Private: key.Private, Format: SshKeyInfoFormat(key.Format), Encrypted: key.Encrypted, Status: status}
	if key.Algorithm != "" {
		info.Algorithm = &key.Algorithm
	}
	if key.Bits != 0 {
		info.Bits = &key.Bits
	}
	if key.Fingerprint != "" {
		info.Fingerprint = &key.Fingerprint
	}
	return InspectSshKey200JSONResponse(info), nil
}

// VisitInspectSshKeyResponse implements [InspectSshKeyResponseObject].
func (p domainProblem) VisitInspectSshKeyResponse(w http.ResponseWriter) error { return p.write(w) }
