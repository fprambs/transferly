package sshkey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1" //nolint:gosec // PBKDF2 with HMAC-SHA-1 is the PKCS#5 default and still how many PKCS#8 files are encrypted.
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"hash"
)

// maxPBKDF2Iterations bounds the PBKDF2 iterations of an encrypted PKCS#8 key; OpenSSL and ssh-keygen use 2048.
const maxPBKDF2Iterations = 1_000_000

var (
	// oidPBES2 and oidPBKDF2 identify PBES2 encryption and its PBKDF2 key derivation (RFC 8018).
	oidPBES2  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	// pbkdf2Hashes are the HMAC pseudorandom functions PBKDF2 may use, by identifier.
	pbkdf2Hashes = map[string]func() hash.Hash{
		"1.2.840.113549.2.7":  sha1.New,
		"1.2.840.113549.2.9":  sha256.New,
		"1.2.840.113549.2.10": sha512.New384,
		"1.2.840.113549.2.11": sha512.New,
	}
	// aesKeySizes are the key sizes of the AES-CBC ciphers PBES2 may use, by identifier.
	aesKeySizes = map[string]int{
		"2.16.840.1.101.3.4.1.2":  16,
		"2.16.840.1.101.3.4.1.22": 24,
		"2.16.840.1.101.3.4.1.42": 32,
	}
)

// encryptedPrivateKeyInfo is the PKCS#8 EncryptedPrivateKeyInfo structure (RFC 5958).
type encryptedPrivateKeyInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	Data      []byte
}

// pbes2Params are the parameters of PBES2: the key derivation and the encryption scheme.
type pbes2Params struct {
	KeyDerivation pkix.AlgorithmIdentifier
	Encryption    pkix.AlgorithmIdentifier
}

// pbkdf2Params are the parameters of PBKDF2; the pseudorandom function defaults to HMAC-SHA-1.
type pbkdf2Params struct {
	Salt       []byte
	Iterations int
	KeyLength  int                      `asn1:"optional"`
	PRF        pkix.AlgorithmIdentifier `asn1:"optional"`
}

// readEncryptedPKCS8 reads an "ENCRYPTED PRIVATE KEY" block protected with PBES2 (PBKDF2 and AES-CBC), the scheme
// OpenSSL and ssh-keygen write, and converts the key to OpenSSH's format under the same passphrase.
func readEncryptedPKCS8(block *pem.Block, passphrase []byte) (Key, error) {
	info := Info{Private: true, Format: FormatPKCS8, Encrypted: true}
	var outer encryptedPrivateKeyInfo
	var scheme pbes2Params
	var kdf pbkdf2Params
	var iv []byte
	switch {
	case !parseDER(block.Bytes, &outer):
		return Key{}, ErrUnreadable
	case !outer.Algorithm.Algorithm.Equal(oidPBES2):
		return Key{}, unsupported(ReasonEncryption)
	case !parseDER(outer.Algorithm.Parameters.FullBytes, &scheme) || !scheme.KeyDerivation.Algorithm.Equal(oidPBKDF2) ||
		!parseDER(scheme.KeyDerivation.Parameters.FullBytes, &kdf) || !parseDER(scheme.Encryption.Parameters.FullBytes, &iv):
		return Key{}, ErrUnreadable
	}
	prf, size := sha1.New, aesKeySizes[scheme.Encryption.Algorithm.String()]
	if kdf.PRF.Algorithm != nil {
		prf = pbkdf2Hashes[kdf.PRF.Algorithm.String()]
	}
	switch {
	case prf == nil || size == 0:
		return Key{}, unsupported(ReasonEncryption)
	case kdf.Iterations < 1 || kdf.Iterations > maxPBKDF2Iterations:
		return Key{}, unsupported(ReasonCost)
	case len(iv) != aes.BlockSize || len(outer.Data) == 0 || len(outer.Data)%aes.BlockSize != 0:
		return Key{}, ErrUnreadable
	case len(passphrase) == 0:
		return Key{Info: info}, ErrPassphraseRequired
	}
	secret := derive(func() []byte {
		key, _ := pbkdf2.Key(prf, string(passphrase), kdf.Salt, kdf.Iterations, size)
		return key
	})
	aesBlock, _ := aes.NewCipher(secret)
	plain := make([]byte, len(outer.Data))
	cipher.NewCBCDecrypter(aesBlock, iv).CryptBlocks(plain, outer.Data)
	var der asn1.RawValue
	_, err := asn1.Unmarshal(plain, &der)
	raw, errParse := x509.ParsePKCS8PrivateKey(der.FullBytes)
	if err != nil || errParse != nil {
		return Key{Info: info}, ErrWrongPassphrase
	}
	return converted(info, raw, "", passphrase)
}

// parseDER decodes one complete DER value into out.
func parseDER(der []byte, out any) bool {
	rest, err := asn1.Unmarshal(der, out)
	return err == nil && len(rest) == 0
}
