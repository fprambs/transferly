package fzimport

import (
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
)

// ErrCryptPassword means a FileZilla crypt password could not be decrypted.
var ErrCryptPassword = errors.New("filezilla crypt password could not be decrypted")

// DecryptCryptPassword reads a site password from FileZilla's encoding="crypt" form, using the master password
// of the site manager and the pubkey attribute on the Pass element.
func DecryptCryptPassword(masterPassword, xmlPubKey, xmlPass string) (string, error) {
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
	mySalt := storedPub[cryptKeySize:]
	storedPublicKey := storedPub[:cryptKeySize]
	myPriv := deriveCryptPrivateKey(masterPassword, mySalt)
	overhead := cryptKeySize + cryptSaltSize + cryptTagSize
	if len(cipherBlob) < overhead {
		return "", fmt.Errorf("%w: ciphertext too short", ErrCryptPassword)
	}
	ephemeralKey := cipherBlob[:cryptKeySize]
	ephemeralSalt := cipherBlob[cryptKeySize : cryptKeySize+cryptSaltSize]
	encrypted := cipherBlob[cryptKeySize+cryptSaltSize : len(cipherBlob)-cryptTagSize]
	tag := cipherBlob[len(cipherBlob)-cryptTagSize:]
	shared, err := curve25519.X25519(myPriv, ephemeralKey)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCryptPassword, err)
	}
	aesKey := cryptHashAccumulator(ephemeralSalt, 0, shared, ephemeralKey, storedPublicKey, mySalt)
	iv := cryptHashAccumulator(ephemeralSalt, 2, shared, ephemeralKey, storedPublicKey, mySalt)[:cryptIVSize]
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCryptPassword, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCryptPassword, err)
	}
	plain, err := gcm.Open(nil, iv, append(encrypted, tag...), nil)
	if err != nil {
		return "", fmt.Errorf("%w: wrong master password or corrupted data", ErrCryptPassword)
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
	out, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, err
	}
	return out, nil
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
