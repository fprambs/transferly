package vault

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"io"
)

// KeyPair is the X25519 key pair of a user. The public half lets other members seal a workspace key for the user; the private half
// is stored sealed with the user's DEK.
type KeyPair struct {
	// Public is the 32 byte public key.
	Public []byte
	// Private is the 32 byte private key. The caller wipes it.
	Private []byte
}

// GenerateKeyPair creates an X25519 key pair drawing randomness from random.
func GenerateKeyPair(random io.Reader) (KeyPair, error) {
	private, err := newPrivateKey(random)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Public: private.PublicKey().Bytes(), Private: private.Bytes()}, nil
}

// newPrivateKey reads 32 random bytes from random and turns them into an X25519 private key. Any 32 bytes are a valid X25519 key,
// and reading them here keeps the source of randomness in the caller's hands: [ecdh.Curve.GenerateKey] ignores it.
func newPrivateKey(random io.Reader) (*ecdh.PrivateKey, error) {
	seed := make([]byte, KeyLength)
	defer Wipe(seed)
	if _, err := io.ReadFull(random, seed); err != nil {
		return nil, fmt.Errorf("reading random bytes: %w", err)
	}
	return ecdh.X25519().NewPrivateKey(seed)
}

// privateKeyAAD binds a sealed private key to its owner. Its label is frozen: changing it makes every stored value unreadable (ADR-0020).
func privateKeyAAD(user string) []byte {
	return []byte("transferly/user-key/v1|" + user)
}

// SealPrivateKey seals the private key of a user with their DEK.
func SealPrivateKey(random io.Reader, dek []byte, user string, private []byte) ([]byte, error) {
	return seal(random, dek, private, privateKeyAAD(user))
}

// OpenPrivateKey opens a private key sealed by [SealPrivateKey], returning [ErrCorrupt] on any mismatch. The caller wipes it.
func OpenPrivateKey(dek []byte, user string, sealed []byte) ([]byte, error) {
	return open(dek, sealed, privateKeyAAD(user))
}

// GrantRef identifies a copy of a workspace key: which workspace, for whom and at which key version. It is authenticated
// with the copy, so a grant cannot be moved to another workspace, member or version without detection.
type GrantRef struct {
	// Workspace is the identifier of the workspace.
	Workspace string
	// User is the identifier of the member the copy is for.
	User string
	// Version is the version of the workspace key.
	Version int
}

// info renders the reference as the context of the key derivation and as associated data. Its label is frozen: changing it makes every
// stored value unreadable (ADR-0020).
func (r GrantRef) info() []byte {
	return []byte(fmt.Sprintf("transferly/workspace-key/v1|%s|%s|%d", r.Workspace, r.User, r.Version))
}

// grantKey derives the key that seals a grant from the shared secret of the exchange.
func grantKey(secret, ephemeral, recipient []byte, ref GrantRef) []byte {
	salt := append(append([]byte{}, ephemeral...), recipient...)
	key, _ := hkdf.Key(sha256.New, secret, salt, string(ref.info()), KeyLength)
	return key
}

// SealTo seals a workspace key for the owner of a public key. It generates an ephemeral X25519 key, derives a key with HKDF-SHA-256
// from the shared secret, the two public keys and ref, and encrypts with AES-256-GCM. The result starts with the ephemeral public key.
func SealTo(random io.Reader, recipient []byte, ref GrantRef, workspaceKey []byte) ([]byte, error) {
	public, err := ecdh.X25519().NewPublicKey(recipient)
	if err != nil {
		return nil, fmt.Errorf("reading the public key: %w", err)
	}
	ephemeral, err := newPrivateKey(random)
	if err != nil {
		return nil, err
	}
	secret, err := ephemeral.ECDH(public)
	if err != nil {
		return nil, fmt.Errorf("exchanging keys: %w", err)
	}
	defer Wipe(secret)
	key := grantKey(secret, ephemeral.PublicKey().Bytes(), recipient, ref)
	defer Wipe(key)
	sealed, err := seal(random, key, workspaceKey, ref.info())
	if err != nil {
		return nil, err
	}
	return append(ephemeral.PublicKey().Bytes(), sealed...), nil
}

// OpenGrant opens a grant sealed by [SealTo] with the private key of its recipient, returning [ErrCorrupt] on any mismatch. The
// caller wipes the result.
func OpenGrant(private []byte, ref GrantRef, sealed []byte) ([]byte, error) {
	if len(sealed) < KeyLength {
		return nil, ErrCorrupt
	}
	own, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, fmt.Errorf("reading the private key: %w", err)
	}
	ephemeral, _ := ecdh.X25519().NewPublicKey(sealed[:KeyLength])
	secret, err := own.ECDH(ephemeral)
	if err != nil {
		return nil, ErrCorrupt
	}
	defer Wipe(secret)
	key := grantKey(secret, sealed[:KeyLength], own.PublicKey().Bytes(), ref)
	defer Wipe(key)
	return open(key, sealed[KeyLength:], ref.info())
}
