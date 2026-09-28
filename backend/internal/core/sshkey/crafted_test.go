package sshkey_test

import (
	"bytes"
	"crypto/dsa" //nolint:staticcheck // A DSA public key is built to check how its size is reported.
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/sentinel/internal/core/sshkey"
)

// sshString encodes values as SSH wire strings, one after the other.
func sshString(values ...[]byte) []byte {
	var out []byte
	for _, v := range values {
		out = binary.BigEndian.AppendUint32(out, uint32(len(v)))
		out = append(out, v...)
	}
	return out
}

// ppk3 writes an unencrypted PPK version 3 file with a valid MAC, which for an unencrypted file is keyed with nothing,
// so tests can hand the reader any private section.
func ppk3(algorithm string, public, private []byte) string {
	mac := hmac.New(sha256.New, nil)
	mac.Write(sshString([]byte(algorithm), []byte("none"), []byte("test"), public, private))
	return fmt.Sprintf("PuTTY-User-Key-File-3: %s\nEncryption: none\nComment: test\nPublic-Lines: 1\n%s\nPrivate-Lines: 1\n%s\nPrivate-MAC: %s\n",
		algorithm, base64.StdEncoding.EncodeToString(public), base64.StdEncoding.EncodeToString(private), hex.EncodeToString(mac.Sum(nil)))
}

// opensshBlock wraps an OpenSSH private key body in its PEM block.
func opensshBlock(body []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
}

// TestRead_CraftedPuTTY verifies that damaged or crafted PuTTY files are refused, never trusted.
//
// Use cases:
//   - a malformed text layer, unknown versions, bad base64 or hex, and a public key that does not match the named
//     algorithm are unreadable or unsupported
//   - unknown encryptions and Argon2 flavors, and Argon2 costs beyond the bounds, are refused before deriving anything
//   - a private section whose MAC is valid but whose key does not match its public half is unreadable, for every type
//   - an unencrypted file whose MAC does not match is unreadable
func TestRead_CraftedPuTTY(t *testing.T) {
	plain := string(fixture(t, "ppk3-ed25519.ppk"))
	encrypted := string(fixture(t, "ppk3-rsa-argon2id.ppk"))
	unreadable := map[string]string{
		"no colon":             strings.Replace(plain, "Comment: test", "Comment", 1),
		"line count":           strings.Replace(plain, "Private-Lines: 1", "Private-Lines: 9", 1),
		"public base64":        strings.Replace(plain, "Public-Lines: 2\n", "Public-Lines: 2\n!", 1),
		"private base64":       strings.Replace(plain, "Private-Lines: 1\n", "Private-Lines: 1\n!", 1),
		"mac hex":              strings.Replace(plain, "Private-MAC: ", "Private-MAC: zz", 1),
		"algorithm mismatch":   strings.Replace(plain, "File-3: ssh-ed25519", "File-3: ssh-rsa", 1),
		"mac mismatch":         strings.Replace(plain, "Comment: test", "Comment: changed", 1),
		"argon2 numbers":       strings.Replace(encrypted, "Argon2-Passes: 13", "Argon2-Passes: x", 1),
		"argon2 flavor":        strings.Replace(encrypted, "Key-Derivation: Argon2id", "Key-Derivation: Scrypt", 1),
		"private length":       ppkWithPrivate(encrypted, []byte("short")),
		"rsa private":          ppk3(ssh.KeyAlgoRSA, rsaPublic(t), sshString([]byte{1})),
		"ecdsa too large":      ppk3(ssh.KeyAlgoECDSA256, ecdsaPublic(t), sshString(bytes.Repeat([]byte{0xff}, 40))),
		"ecdsa mismatch":       ppk3(ssh.KeyAlgoECDSA256, ecdsaPublic(t), sshString([]byte{7})),
		"ed25519 seed size":    ppk3(ssh.KeyAlgoED25519, ed25519Public(t), sshString([]byte{1, 2})),
		"ed25519 mismatch":     ppk3(ssh.KeyAlgoED25519, ed25519Public(t), sshString(make([]byte, ed25519.SeedSize))),
		"rfc4716 base64":       "---- BEGIN SSH2 PUBLIC KEY ----\n!!!\n---- END SSH2 PUBLIC KEY ----",
		"rfc4716 unterminated": "---- BEGIN SSH2 PUBLIC KEY ----\nAAAA",
		"rfc4716 not a key":    "---- BEGIN SSH2 PUBLIC KEY ----\nAAAA\n---- END SSH2 PUBLIC KEY ----",
	}
	for name, data := range unreadable {
		if _, err := sshkey.Read([]byte(data), []byte(passphrase)); !errors.Is(err, sshkey.ErrUnreadable) {
			t.Errorf("Read(%s) = %v; want ErrUnreadable", name, err)
		}
	}
	unsupported := map[string]string{
		"version 1":     strings.Replace(plain, "File-3", "File-1", 1),
		"encryption":    strings.Replace(encrypted, "Encryption: aes256-cbc", "Encryption: aes128-ctr", 1),
		"argon2 memory": strings.Replace(encrypted, "Argon2-Memory: 8192", "Argon2-Memory: 1048576", 1),
		"argon2 work":   strings.Replace(encrypted, "Argon2-Passes: 13", "Argon2-Passes: 1000", 1),
	}
	for name, data := range unsupported {
		if _, err := sshkey.Read([]byte(data), []byte(passphrase)); !errors.Is(err, sshkey.ErrUnsupported) {
			t.Errorf("Read(%s) = %v; want ErrUnsupported", name, err)
		}
	}
	trailing := string(fixture(t, "rfc4716-ed25519.pub")) + "\nafter the end"
	if _, err := sshkey.Read([]byte(trailing), nil); !errors.Is(err, sshkey.ErrPublicKey) {
		t.Errorf("Read(RFC 4716 with text after the end) = %v; want ErrPublicKey", err)
	}
}

// ppkWithPrivate replaces the private section of a PPK file.
func ppkWithPrivate(file string, private []byte) string {
	start := strings.Index(file, "Private-Lines: ")
	end := strings.Index(file, "Private-MAC: ")
	return file[:start] + "Private-Lines: 1\n" + base64.StdEncoding.EncodeToString(private) + "\n" + file[end:]
}

// publicBlob returns the wire form of the public key of a fixture.
func publicBlob(t *testing.T, name string) []byte {
	t.Helper()
	public, _, _, _, err := ssh.ParseAuthorizedKey(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return public.Marshal()
}

// rsaPublic returns an RSA public key, from the fixtures.
func rsaPublic(t *testing.T) []byte { return publicBlob(t, "ppk3-rsa-argon2id.ppk.pub") }

// ecdsaPublic returns an ECDSA P-256 public key, from the fixtures.
func ecdsaPublic(t *testing.T) []byte { return publicBlob(t, "ppk3-ecdsa-argon2i.ppk.pub") }

// ed25519Public returns an Ed25519 public key, from the fixtures.
func ed25519Public(t *testing.T) []byte { return publicBlob(t, "ppk3-ed25519.ppk.pub") }

// TestRead_CraftedOpenSSH verifies the checks made on an OpenSSH private key before the library reads it.
//
// Use cases:
//   - a body without the magic, with other than one key, with a damaged public key or damaged bcrypt options is unreadable
//   - more bcrypt rounds than the bound is unsupported, refused before spending them
func TestRead_CraftedOpenSSH(t *testing.T) {
	public := ed25519Public(t)
	body := func(kdf string, options []byte, keys uint32, pub []byte) []byte {
		out := append([]byte("openssh-key-v1\x00"), sshString([]byte("aes256-ctr"), []byte(kdf), options)...)
		out = binary.BigEndian.AppendUint32(out, keys)
		return append(out, sshString(pub)...)
	}
	rounds := func(n uint32) []byte { return binary.BigEndian.AppendUint32(sshString([]byte("salt")), n) }
	unreadable := map[string][]byte{
		"magic":          opensshBlock([]byte("openssh-key-v0\x00")),
		"short":          opensshBlock([]byte("openssh")),
		"two keys":       opensshBlock(body("none", nil, 2, public)),
		"public key":     opensshBlock(body("none", nil, 1, []byte("junk"))),
		"string length":  opensshBlock(append([]byte("openssh-key-v1\x00"), 0, 0, 0, 9)),
		"bcrypt options": opensshBlock(body("bcrypt", sshString([]byte("salt")), 1, public)),
	}
	for name, data := range unreadable {
		if _, err := sshkey.Read(data, nil); !errors.Is(err, sshkey.ErrUnreadable) {
			t.Errorf("Read(%s) = %v; want ErrUnreadable", name, err)
		}
	}
	if _, err := sshkey.Read(opensshBlock(body("bcrypt", rounds(1<<20), 1, public)), []byte(passphrase)); !errors.Is(err, sshkey.ErrUnsupported) {
		t.Errorf("Read(too many bcrypt rounds) = %v; want ErrUnsupported", err)
	}
}

// encryptedInfo mirrors PKCS#8's EncryptedPrivateKeyInfo, so tests can rewrite a real file.
type encryptedInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	Data      []byte
}

// pbes2 mirrors the PBES2 parameters.
type pbes2 struct {
	KeyDerivation pkix.AlgorithmIdentifier
	Encryption    pkix.AlgorithmIdentifier
}

// pbkdf2 mirrors the PBKDF2 parameters.
type pbkdf2 struct {
	Salt       []byte
	Iterations int
	KeyLength  int                      `asn1:"optional"`
	PRF        pkix.AlgorithmIdentifier `asn1:"optional"`
}

// rewritePKCS8 decodes the encrypted PKCS#8 fixture, lets edit change it, and encodes it again.
func rewritePKCS8(t *testing.T, edit func(*encryptedInfo, *pbes2, *pbkdf2, *[]byte)) []byte {
	t.Helper()
	block, _ := pem.Decode(fixture(t, "pkcs8-ecdsa-encrypted"))
	var info encryptedInfo
	var scheme pbes2
	var kdf pbkdf2
	var iv []byte
	mustUnmarshal(t, block.Bytes, &info)
	mustUnmarshal(t, info.Algorithm.Parameters.FullBytes, &scheme)
	mustUnmarshal(t, scheme.KeyDerivation.Parameters.FullBytes, &kdf)
	mustUnmarshal(t, scheme.Encryption.Parameters.FullBytes, &iv)
	edit(&info, &scheme, &kdf, &iv)
	scheme.KeyDerivation.Parameters = raw(t, kdf)
	scheme.Encryption.Parameters = raw(t, iv)
	info.Algorithm.Parameters = raw(t, scheme)
	der, err := asn1.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: der})
}

// mustUnmarshal decodes DER or fails the test.
func mustUnmarshal(t *testing.T, der []byte, out any) {
	t.Helper()
	if _, err := asn1.Unmarshal(der, out); err != nil {
		t.Fatal(err)
	}
}

// raw encodes a value as an ASN.1 raw value.
func raw(t *testing.T, v any) asn1.RawValue {
	t.Helper()
	der, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return asn1.RawValue{FullBytes: der}
}

// TestRead_CraftedPKCS8 verifies how encrypted PKCS#8 keys other than the ones OpenSSL writes by default are handled.
//
// Use cases:
//   - other encryption schemes, key derivations, ciphers and hashes are unsupported or unreadable, and so is a file
//     that is not DER, an IV of the wrong size or data that is not whole blocks
//   - more PBKDF2 iterations than the bound are refused before deriving
//   - an X25519 key, which SSH does not use, is unsupported once decrypted
func TestRead_CraftedPKCS8(t *testing.T) {
	other := asn1.ObjectIdentifier{1, 2, 3}
	unsupported := map[string][]byte{
		"scheme":     rewritePKCS8(t, func(i *encryptedInfo, _ *pbes2, _ *pbkdf2, _ *[]byte) { i.Algorithm.Algorithm = other }),
		"cipher":     rewritePKCS8(t, func(_ *encryptedInfo, s *pbes2, _ *pbkdf2, _ *[]byte) { s.Encryption.Algorithm = other }),
		"hash":       rewritePKCS8(t, func(_ *encryptedInfo, _ *pbes2, k *pbkdf2, _ *[]byte) { k.PRF.Algorithm = other }),
		"iterations": rewritePKCS8(t, func(_ *encryptedInfo, _ *pbes2, k *pbkdf2, _ *[]byte) { k.Iterations = 10_000_000 }),
		"x25519":     fixture(t, "pkcs8-x25519-encrypted"),
	}
	for name, data := range unsupported {
		if _, err := sshkey.Read(data, []byte(passphrase)); !errors.Is(err, sshkey.ErrUnsupported) {
			t.Errorf("Read(%s) = %v; want ErrUnsupported", name, err)
		}
	}
	unreadable := map[string][]byte{
		"not DER":    pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("junk")}),
		"derivation": rewritePKCS8(t, func(_ *encryptedInfo, s *pbes2, _ *pbkdf2, _ *[]byte) { s.KeyDerivation.Algorithm = other }),
		"iv":         rewritePKCS8(t, func(_ *encryptedInfo, _ *pbes2, _ *pbkdf2, iv *[]byte) { *iv = (*iv)[:8] }),
		"data":       rewritePKCS8(t, func(i *encryptedInfo, _ *pbes2, _ *pbkdf2, _ *[]byte) { i.Data = i.Data[:17] }),
	}
	for name, data := range unreadable {
		if _, err := sshkey.Read(data, []byte(passphrase)); !errors.Is(err, sshkey.ErrUnreadable) {
			t.Errorf("Read(%s) = %v; want ErrUnreadable", name, err)
		}
	}
}

// TestRead_OtherKeyTypes verifies keys that are readable but not used, and how the size of unusual public keys is told.
//
// Use cases:
//   - a PEM elliptic curve key on a curve SSH does not define, and a PEM X25519 public key, are unsupported
//   - a PKCS#1 RSA public key is recognized as a public key
//   - a DSA public key and a certificate are public keys with no size given
func TestRead_OtherKeyTypes(t *testing.T) {
	for _, file := range []string{"pem-ec-p224", "pkix-x25519-public.pem"} {
		if _, err := sshkey.Read(fixture(t, file), nil); !errors.Is(err, sshkey.ErrUnsupported) {
			t.Errorf("Read(%s) = %v; want ErrUnsupported", file, err)
		}
	}
	if key, err := sshkey.Read(fixture(t, "pkcs1-rsa-public.pem"), nil); !errors.Is(err, sshkey.ErrPublicKey) || key.Bits != 2048 {
		t.Errorf("Read(PKCS#1 public key) = %+v, %v; want a 2048-bit public key", key.Info, err)
	}

	var dsaKey dsa.PrivateKey
	if err := dsa.GenerateParameters(&dsaKey.Parameters, rand.Reader, dsa.L1024N160); err != nil {
		t.Fatal(err)
	}
	if err := dsa.GenerateKey(&dsaKey, rand.Reader); err != nil {
		t.Fatal(err)
	}
	dsaPublic, err := ssh.NewPublicKey(&dsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.UserCert, ValidBefore: ssh.CertTimeInfinity}
	if err := certificate.SignCert(rand.Reader, signer); err != nil {
		t.Fatal(err)
	}
	for name, public := range map[string]ssh.PublicKey{"dsa": dsaPublic, "certificate": certificate} {
		key, err := sshkey.Read(ssh.MarshalAuthorizedKey(public), nil)
		if !errors.Is(err, sshkey.ErrPublicKey) || key.Bits != 0 || key.Algorithm != public.Type() {
			t.Errorf("Read(%s) = %+v, %v; want a public key of unknown size", name, key.Info, err)
		}
	}
}
