package fzimport

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/pbkdf2"
)

const (
	cryptKeySize    = 32
	cryptSaltSize   = 32
	cryptIVSize     = 12
	cryptTagSize    = 16
	cryptPBKDF2Iter = 100_000
	// maxCryptKeys is the most different keys of one site manager file whose passwords are opened. FileZilla seals every password of a
	// file with one key; a file with many keys would cost a key derivation each.
	maxCryptKeys = 4
)

// ErrCryptPassword means a FileZilla crypt password could not be decrypted.
var ErrCryptPassword = errors.New("filezilla crypt password could not be decrypted")

// cryptOpener opens the crypt passwords of one site manager file with its master password, deriving the private key once per key of
// the file, since the derivation is slow on purpose, and refusing a file with more than [maxCryptKeys] keys.
type cryptOpener struct {
	master string
	keys   map[string][]byte
}

// newCryptOpener returns an opener for the master password of a site manager file.
func newCryptOpener(master string) *cryptOpener {
	return &cryptOpener{master: master, keys: map[string][]byte{}}
}

// DecryptCryptPassword reads a site password from FileZilla's encoding="crypt" form, using the master password
// of the site manager and the pubkey attribute on the Pass element.
func DecryptCryptPassword(masterPassword, xmlPubKey, xmlPass string) (string, error) {
	return newCryptOpener(masterPassword).open(xmlPubKey, xmlPass)
}

// open reads one crypt password: X25519 between the private key derived from the master password and the ephemeral key of the
// password, SHA-256 of the key material for the AES key and the nonce, and AES-256-GCM, as libfilezilla seals it. FileZilla pads
// passwords shorter than 16 bytes with zero bytes before sealing them, so they do not tell their length; the password ends at the
// first zero byte, which no password holds.
func (o *cryptOpener) open(xmlPubKey, xmlPass string) (string, error) {
	storedPub, err := decodeCryptBase64(xmlPubKey)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCryptPassword, err)
	}
	cipherBlob, err := decodeCryptBase64(xmlPass)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCryptPassword, err)
	}
	if len(storedPub) != cryptKeySize+cryptSaltSize {
		return "", fmt.Errorf("%w: invalid pubkey length", ErrCryptPassword)
	}
	if len(cipherBlob) < cryptKeySize+cryptSaltSize+cryptTagSize {
		return "", fmt.Errorf("%w: ciphertext too short", ErrCryptPassword)
	}
	mySalt := storedPub[cryptKeySize:]
	storedPublicKey := storedPub[:cryptKeySize]
	myPriv, known := o.keys[string(storedPub)]
	if !known {
		if len(o.keys) >= maxCryptKeys {
			return "", fmt.Errorf("%w: the file uses too many keys", ErrCryptPassword)
		}
		myPriv = deriveCryptPrivateKey(o.master, mySalt)
		o.keys[string(storedPub)] = myPriv
	}
	ephemeralKey := cipherBlob[:cryptKeySize]
	ephemeralSalt := cipherBlob[cryptKeySize : cryptKeySize+cryptSaltSize]
	sealed := cipherBlob[cryptKeySize+cryptSaltSize:]
	shared, err := curve25519.X25519(myPriv, ephemeralKey)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCryptPassword, err)
	}
	aesKey := cryptHashAccumulator(ephemeralSalt, 0, shared, ephemeralKey, storedPublicKey, mySalt)
	iv := cryptHashAccumulator(ephemeralSalt, 2, shared, ephemeralKey, storedPublicKey, mySalt)[:cryptIVSize]
	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("%w: wrong master password or corrupted data", ErrCryptPassword)
	}
	if end := bytes.IndexByte(plain, 0); end >= 0 {
		plain = plain[:end]
	}
	if !utf8.Valid(plain) {
		return "", fmt.Errorf("%w: password is not valid UTF-8", ErrCryptPassword)
	}
	return string(plain), nil
}

// decodeCryptBase64 reads FileZilla crypt fields that may carry whitespace or missing padding.
func decodeCryptBase64(raw string) ([]byte, error) {
	clean := strings.NewReplacer(" ", "", "\n", "", "\r", "").Replace(strings.TrimSpace(raw))
	if clean == "" {
		return nil, errors.New("empty base64")
	}
	if pad := len(clean) % 4; pad != 0 {
		clean += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(clean)
}

// deriveCryptPrivateKey turns the master password and salt into a clamped Curve25519 private key.
func deriveCryptPrivateKey(password string, salt []byte) []byte {
	key := pbkdf2.Key([]byte(password), salt, cryptPBKDF2Iter, cryptKeySize, sha256.New)
	key[0] &= 248
	key[31] &= 127
	key[31] |= 64
	return key
}

// cryptHashAccumulator mixes the ephemeral salt, kind byte and key material into a 32-byte digest.
func cryptHashAccumulator(ephemeralSalt []byte, kind byte, shared, ephemeralKey, storedPublicKey, mySalt []byte) []byte {
	h := sha256.New()
	for _, part := range [][]byte{ephemeralSalt, {kind}, shared, ephemeralKey, storedPublicKey, mySalt} {
		h.Write(part)
	}
	return h.Sum(nil)
}
