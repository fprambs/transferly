// Package sshkey reads SSH key files by their content, whatever their name: OpenSSH, PEM (PKCS#1 and SEC 1, plain or
// with the legacy OpenSSL encryption), PKCS#8 (plain or encrypted with PBES2) and PuTTY's PPK versions 2 and 3. It tells
// a private key from a public one, reports the type and the SHA-256 fingerprint, checks the passphrase, and turns every
// private key into a form [golang.org/x/crypto/ssh] parses, so connecting needs no knowledge of the file it came from.
//
// The costly key derivations a file names (bcrypt rounds, Argon2 memory and passes, PBKDF2 iterations) are bounded and
// run a few at a time: a crafted file must not be able to hold the server's processor or memory.
package sshkey

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"

	"golang.org/x/crypto/ssh"
)

// Format is the file format a key was read from.
type Format string

const (
	// FormatOpenSSH is OpenSSH's own format: "OPENSSH PRIVATE KEY" blocks and authorized_keys lines.
	FormatOpenSSH Format = "openssh"
	// FormatPEM is the traditional PEM format: PKCS#1 RSA, SEC 1 elliptic curve and PKCS#1 or PKIX public keys.
	FormatPEM Format = "pem"
	// FormatPKCS8 is a PKCS#8 private key, plain or encrypted.
	FormatPKCS8 Format = "pkcs8"
	// FormatRFC4716 is the SSH2 public key format of RFC 4716 ("---- BEGIN SSH2 PUBLIC KEY ----").
	FormatRFC4716 Format = "rfc4716"
	// FormatPuTTY2 is PuTTY's PPK version 2 (PuTTY 0.52 to 0.74).
	FormatPuTTY2 Format = "ppk2"
	// FormatPuTTY3 is PuTTY's PPK version 3.
	FormatPuTTY3 Format = "ppk3"
)

// Info describes a key file.
type Info struct {
	// Private says the file holds a private key; false means a public key.
	Private bool
	// Format is the format of the file.
	Format Format
	// Algorithm is the SSH name of the key type, such as "ssh-ed25519"; empty when it cannot be known before decrypting.
	Algorithm string
	// Bits is the size of the key; zero when unknown.
	Bits int
	// Fingerprint is the SHA-256 fingerprint in OpenSSH's form ("SHA256:…"); empty when it cannot be known before decrypting.
	Fingerprint string
	// Encrypted says the private key is protected by a passphrase.
	Encrypted bool
}

// Key is a private key read from a file.
type Key struct {
	// Info describes the file.
	Info
	// PEM is the key as [ssh.ParsePrivateKey] or [ssh.ParsePrivateKeyWithPassphrase] reads it, protected by the same
	// passphrase as the file: the file itself when the library reads its format, otherwise the key in OpenSSH's format.
	PEM []byte
}

var (
	// ErrUnreadable means the data is not a key file of a known format, or is damaged.
	ErrUnreadable = errors.New("not a readable key file")
	// ErrPublicKey means the file holds a public key where a private key is needed.
	ErrPublicKey = errors.New("the file holds a public key, not a private key")
	// ErrPassphraseRequired means the private key is encrypted and no passphrase was given.
	ErrPassphraseRequired = errors.New("the private key needs its passphrase")
	// ErrWrongPassphrase means the passphrase does not open the private key.
	ErrWrongPassphrase = errors.New("wrong passphrase for the private key")
	// ErrUnsupported means the key is readable but of a type or protection Transferly does not use; the error is an
	// [*UnsupportedError] that says which.
	ErrUnsupported = errors.New("unsupported key")
)

// Reasons an [UnsupportedError] gives, stable so clients can explain them.
const (
	// ReasonKeyType is a key type Transferly does not connect with: DSA, Ed448, X25519 or an elliptic curve SSH does not define.
	ReasonKeyType = "key_type"
	// ReasonPuTTYVersion is a PuTTY key file of a version other than 2 and 3.
	ReasonPuTTYVersion = "ppk_version"
	// ReasonEncryption is a way of encrypting the key other than the ones read: AES-256-CBC for PuTTY, PBES2 with
	// PBKDF2 and AES-CBC for PKCS#8.
	ReasonEncryption = "encryption"
	// ReasonArgon2d is a PuTTY key file derived with Argon2d, which the Argon2 library does not offer; saving the key
	// again with PuTTYgen's default Argon2id fixes it.
	ReasonArgon2d = "argon2d"
	// ReasonCost is a key derivation costlier than the bounds on bcrypt rounds, Argon2 memory and passes, or PBKDF2
	// iterations.
	ReasonCost = "cost"
)

// UnsupportedError is a key that is readable but not usable, and why. It matches [ErrUnsupported].
type UnsupportedError struct {
	// Reason is one of the Reason constants.
	Reason string
}

// Error implements [error].
func (e *UnsupportedError) Error() string { return "unsupported key: " + e.Reason }

// Is reports whether target is [ErrUnsupported].
func (e *UnsupportedError) Is(target error) bool { return target == ErrUnsupported }

// unsupported returns an [UnsupportedError] with the reason.
func unsupported(reason string) error { return &UnsupportedError{Reason: reason} }

// Read reads a private key file with its passphrase, which may be empty. The returned key describes the file even when
// the error is [ErrPublicKey], [ErrPassphraseRequired] or [ErrWrongPassphrase], as far as the file tells without the
// passphrase. A passphrase given for a key that is not encrypted is ignored: [Info.Encrypted] says whether it is needed.
func Read(data, passphrase []byte) (Key, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	switch {
	case bytes.HasPrefix(data, []byte(ppkPrefix)):
		return readPPK(data, passphrase)
	case bytes.HasPrefix(data, []byte(rfc4716Begin)):
		return readRFC4716(data)
	}
	if public, _, _, _, err := ssh.ParseAuthorizedKey(data); err == nil {
		return publicKey(FormatOpenSSH, public)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return Key{}, ErrUnreadable
	}
	switch block.Type {
	case "PUBLIC KEY", "RSA PUBLIC KEY":
		return readPEMPublic(block)
	case "OPENSSH PRIVATE KEY":
		return readOpenSSH(data, block, passphrase)
	case "RSA PRIVATE KEY", "EC PRIVATE KEY":
		return readNative(FormatPEM, data, passphrase, nil)
	case "PRIVATE KEY":
		return readNative(FormatPKCS8, data, passphrase, nil)
	case "ENCRYPTED PRIVATE KEY":
		return readEncryptedPKCS8(block, passphrase)
	case "DSA PRIVATE KEY":
		return Key{}, unsupported(ReasonKeyType)
	}
	return Key{}, ErrUnreadable
}

// readNative reads a private key whose format the SSH library parses. known is the public key the file carries in
// clear text, when it does.
func readNative(format Format, data, passphrase []byte, known ssh.PublicKey) (Key, error) {
	info := Info{Private: true, Format: format}
	describe(&info, known)
	raw, err := ssh.ParseRawPrivateKey(data)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		info.Encrypted = true
		if len(passphrase) == 0 {
			return Key{Info: info}, ErrPassphraseRequired
		}
		raw, err = ssh.ParseRawPrivateKeyWithPassphrase(data, passphrase)
		if errors.Is(err, x509.IncorrectPasswordError) {
			return Key{Info: info}, ErrWrongPassphrase
		}
	}
	if err != nil {
		return Key{}, ErrUnreadable
	}
	signer, err := supported(raw)
	if err != nil {
		return Key{}, err
	}
	describe(&info, signer.PublicKey())
	return Key{Info: info, PEM: data}, nil
}

// converted describes a private key decoded by this package and encodes it in OpenSSH's format, encrypted with the
// passphrase when the file was. Encoding cannot fail for the key types [supported] accepts.
func converted(info Info, raw crypto.PrivateKey, comment string, passphrase []byte) (Key, error) {
	signer, err := supported(raw)
	if err != nil {
		return Key{}, err
	}
	describe(&info, signer.PublicKey())
	var block *pem.Block
	if info.Encrypted {
		block, _ = ssh.MarshalPrivateKeyWithPassphrase(raw, comment, passphrase)
	} else {
		block, _ = ssh.MarshalPrivateKey(raw, comment)
	}
	return Key{Info: info, PEM: pem.EncodeToMemory(block)}, nil
}

// supported returns a signer for the private key types Transferly connects with: RSA, ECDSA on the curves SSH defines,
// and Ed25519.
func supported(raw any) (ssh.Signer, error) {
	switch raw.(type) {
	case *rsa.PrivateKey, *ecdsa.PrivateKey, ed25519.PrivateKey, *ed25519.PrivateKey:
		signer, err := ssh.NewSignerFromKey(raw)
		if err != nil {
			return nil, unsupported(ReasonKeyType)
		}
		return signer, nil
	}
	return nil, unsupported(ReasonKeyType)
}

// publicKey describes a public key file.
func publicKey(format Format, key ssh.PublicKey) (Key, error) {
	info := Info{Format: format}
	describe(&info, key)
	return Key{Info: info}, ErrPublicKey
}

// readPEMPublic reads a PEM public key: PKIX ("PUBLIC KEY") or PKCS#1 ("RSA PUBLIC KEY").
func readPEMPublic(block *pem.Block) (Key, error) {
	var parsed any
	var err error
	if block.Type == "PUBLIC KEY" {
		parsed, err = x509.ParsePKIXPublicKey(block.Bytes)
	} else {
		parsed, err = x509.ParsePKCS1PublicKey(block.Bytes)
	}
	if err != nil {
		return Key{}, ErrUnreadable
	}
	public, err := ssh.NewPublicKey(parsed)
	if err != nil {
		return Key{}, unsupported(ReasonKeyType)
	}
	return publicKey(FormatPEM, public)
}

// describe fills the type, size and fingerprint of a key from its public half; nil leaves them as they are.
func describe(info *Info, public ssh.PublicKey) {
	if public == nil {
		return
	}
	info.Algorithm, info.Fingerprint, info.Bits = public.Type(), ssh.FingerprintSHA256(public), bits(public)
}

// bits returns the size of a public key, or zero for types without one.
func bits(public ssh.PublicKey) int {
	if holder, ok := public.(ssh.CryptoPublicKey); ok {
		switch key := holder.CryptoPublicKey().(type) {
		case *rsa.PublicKey:
			return key.N.BitLen()
		case *ecdsa.PublicKey:
			return key.Curve.Params().BitSize
		case ed25519.PublicKey:
			return 256
		}
	}
	return 0
}
