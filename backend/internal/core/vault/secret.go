package vault

import (
	"fmt"
	"io"
)

// SecretRef identifies what a sealed secret belongs to. It is authenticated with the ciphertext,
// so a secret cannot be moved to another site, kind or key version without detection.
type SecretRef struct {
	// Site is the identifier of the site that owns the secret.
	Site string
	// Kind is the type of secret, such as password, private_key or passphrase.
	Kind string
	// DEKVersion is the version of the data-encryption key that seals the secret.
	DEKVersion int
}

// aad renders the reference as associated data.
func (r SecretRef) aad() []byte {
	return []byte(fmt.Sprintf("sentinel/secret/v1|%s|%s|%d", r.Site, r.Kind, r.DEKVersion))
}

// SealSecret encrypts a secret with the DEK, binding it to ref.
func SealSecret(random io.Reader, dek []byte, ref SecretRef, plaintext []byte) ([]byte, error) {
	return seal(random, dek, plaintext, ref.aad())
}

// OpenSecret decrypts a secret sealed for ref, returning [ErrCorrupt] on any mismatch.
func OpenSecret(dek []byte, ref SecretRef, sealed []byte) ([]byte, error) {
	return open(dek, sealed, ref.aad())
}
