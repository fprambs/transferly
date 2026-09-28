package sshkey_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/sentinel/internal/core/sshkey"
)

// passphrase protects every encrypted fixture in testdata. The fixtures were made for these tests with ssh-keygen and
// PuTTYgen and are used nowhere else.
const passphrase = "correct horse"

// fixture reads a file of testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fingerprintOf returns the fingerprint of the public key a fixture's tool wrote next to it.
func fingerprintOf(t *testing.T, name string) string {
	t.Helper()
	public, _, _, _, err := ssh.ParseAuthorizedKey(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return ssh.FingerprintSHA256(public)
}

// TestRead_PrivateKeys verifies that private keys of every supported format are recognized by their content.
//
// Use cases:
//   - OpenSSH, PEM (PKCS#1 and SEC 1), PKCS#8 and PuTTY versions 2 and 3 are read, plain and encrypted, with the type,
//     size and fingerprint the tools that made them report
//   - PuTTY version 3 files are read with both Argon2id and Argon2i, and with every NIST curve
//   - the key comes out in a form the SSH library opens with the same passphrase, holding the same key
//   - a passphrase given for a key that is not encrypted is ignored
func TestRead_PrivateKeys(t *testing.T) {
	cases := []struct {
		file, public string
		format       sshkey.Format
		algorithm    string
		bits         int
		encrypted    bool
	}{
		{"openssh-ed25519", "openssh-ed25519.pub", sshkey.FormatOpenSSH, ssh.KeyAlgoED25519, 256, false},
		{"openssh-ed25519-encrypted", "", sshkey.FormatOpenSSH, ssh.KeyAlgoED25519, 256, true},
		{"pem-rsa", "", sshkey.FormatPEM, ssh.KeyAlgoRSA, 2048, false},
		{"pem-rsa-encrypted", "", sshkey.FormatPEM, ssh.KeyAlgoRSA, 2048, true},
		{"pkcs8-ecdsa", "", sshkey.FormatPKCS8, ssh.KeyAlgoECDSA256, 256, false},
		{"pkcs8-ecdsa-encrypted", "", sshkey.FormatPKCS8, ssh.KeyAlgoECDSA384, 384, true},
		{"ppk3-ed25519.ppk", "ppk3-ed25519.ppk.pub", sshkey.FormatPuTTY3, ssh.KeyAlgoED25519, 256, false},
		{"ppk3-rsa-argon2id.ppk", "ppk3-rsa-argon2id.ppk.pub", sshkey.FormatPuTTY3, ssh.KeyAlgoRSA, 2048, true},
		{"ppk3-ecdsa-argon2i.ppk", "ppk3-ecdsa-argon2i.ppk.pub", sshkey.FormatPuTTY3, ssh.KeyAlgoECDSA256, 256, true},
		{"ppk3-ecdsa384.ppk", "ppk3-ecdsa384.ppk.pub", sshkey.FormatPuTTY3, ssh.KeyAlgoECDSA384, 384, false},
		{"ppk3-ecdsa521.ppk", "ppk3-ecdsa521.ppk.pub", sshkey.FormatPuTTY3, ssh.KeyAlgoECDSA521, 521, false},
		{"ppk2-rsa-encrypted.ppk", "ppk2-rsa-encrypted.ppk.pub", sshkey.FormatPuTTY2, ssh.KeyAlgoRSA, 2048, true},
		{"ppk2-ed25519.ppk", "ppk2-ed25519.ppk.pub", sshkey.FormatPuTTY2, ssh.KeyAlgoED25519, 256, false},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			key, err := sshkey.Read(fixture(t, c.file), []byte(passphrase))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			if !key.Private || key.Format != c.format || key.Algorithm != c.algorithm || key.Bits != c.bits || key.Encrypted != c.encrypted {
				t.Errorf("Read() = %+v; want a private %s %s key of %d bits, encrypted %v", key.Info, c.format, c.algorithm, c.bits, c.encrypted)
			}
			if c.public != "" && key.Fingerprint != fingerprintOf(t, c.public) {
				t.Errorf("fingerprint = %s; want %s", key.Fingerprint, fingerprintOf(t, c.public))
			}
			var signer ssh.Signer
			if c.encrypted {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(key.PEM, []byte(passphrase))
			} else {
				signer, err = ssh.ParsePrivateKey(key.PEM)
			}
			if err != nil || ssh.FingerprintSHA256(signer.PublicKey()) != key.Fingerprint {
				t.Errorf("the key read does not open as the same key: %v", err)
			}
		})
	}
}

// TestRead_Passphrases verifies how encrypted keys answer without their passphrase or with a wrong one.
//
// Use cases:
//   - with no passphrase an encrypted key needs one, and is still described as far as its file tells in clear text: the
//     type and fingerprint of OpenSSH and PuTTY keys, nothing more for PEM and PKCS#8 ones
//   - a wrong passphrase is told apart from a damaged file, in every format
//   - Argon2d PuTTY files are refused as unsupported, with that reason
func TestRead_Passphrases(t *testing.T) {
	for _, file := range []string{"openssh-ed25519-encrypted", "pem-rsa-encrypted", "pkcs8-ecdsa-encrypted", "ppk3-rsa-argon2id.ppk", "ppk2-rsa-encrypted.ppk"} {
		t.Run(file, func(t *testing.T) {
			key, err := sshkey.Read(fixture(t, file), nil)
			if !errors.Is(err, sshkey.ErrPassphraseRequired) || !key.Private || !key.Encrypted {
				t.Errorf("Read() with no passphrase = %+v, %v; want an encrypted private key that needs its passphrase", key.Info, err)
			}
			clear := strings.HasPrefix(file, "openssh") || strings.HasPrefix(file, "ppk")
			if clear != (key.Fingerprint != "") {
				t.Errorf("fingerprint before the passphrase = %q; want one only from OpenSSH and PuTTY files", key.Fingerprint)
			}
			if _, err := sshkey.Read(fixture(t, file), []byte("not it")); !errors.Is(err, sshkey.ErrWrongPassphrase) {
				t.Errorf("Read() with a wrong passphrase = %v; want ErrWrongPassphrase", err)
			}
		})
	}
	_, err := sshkey.Read(fixture(t, "ppk3-ed25519-argon2d.ppk"), []byte(passphrase))
	var reason *sshkey.UnsupportedError
	if !errors.Is(err, sshkey.ErrUnsupported) || !errors.As(err, &reason) || reason.Reason != sshkey.ReasonArgon2d || err.Error() != "unsupported key: argon2d" {
		t.Errorf("Read() of an Argon2d file = %v; want ErrUnsupported for Argon2d", err)
	}
}

// TestRead_PublicKeys verifies that public keys are recognized and described, so the user is told to pick the private one.
//
// Use cases:
//   - authorized_keys lines, RFC 4716 files, PKIX and PKCS#1 PEM public keys and PuTTY's OpenSSH export are all
//     ErrPublicKey, with the fingerprint of the key
func TestRead_PublicKeys(t *testing.T) {
	want := fingerprintOf(t, "openssh-ed25519.pub")
	cases := []struct {
		file   string
		format sshkey.Format
		same   bool
	}{
		{"openssh-ed25519.pub", sshkey.FormatOpenSSH, true},
		{"rfc4716-ed25519.pub", sshkey.FormatRFC4716, true},
		{"pkcs8-rsa-public.pem", sshkey.FormatPEM, false},
		{"ppk3-ed25519.ppk.pub", sshkey.FormatOpenSSH, false},
	}
	for _, c := range cases {
		key, err := sshkey.Read(fixture(t, c.file), nil)
		if !errors.Is(err, sshkey.ErrPublicKey) || key.Private || key.Format != c.format || key.Fingerprint == "" {
			t.Errorf("Read(%s) = %+v, %v; want a %s public key with its fingerprint", c.file, key.Info, err, c.format)
		}
		if c.same && key.Fingerprint != want {
			t.Errorf("Read(%s) fingerprint = %s; want %s", c.file, key.Fingerprint, want)
		}
	}
}

// TestRead_Refusals verifies the keys and files that are refused.
//
// Use cases:
//   - DSA and Ed448 keys are unsupported, from PEM and PuTTY files alike
//   - text that is no key, and damaged key files, are unreadable
func TestRead_Refusals(t *testing.T) {
	for _, file := range []string{"ppk3-dsa.ppk", "ppk3-ed448.ppk"} {
		if _, err := sshkey.Read(fixture(t, file), nil); !errors.Is(err, sshkey.ErrUnsupported) {
			t.Errorf("Read(%s) = %v; want ErrUnsupported", file, err)
		}
	}
	dsa := "-----BEGIN DSA PRIVATE KEY-----\nAAAA\n-----END DSA PRIVATE KEY-----\n"
	if _, err := sshkey.Read([]byte(dsa), nil); !errors.Is(err, sshkey.ErrUnsupported) {
		t.Errorf("Read(DSA PEM) = %v; want ErrUnsupported", err)
	}
	for name, data := range map[string]string{
		"text":            "hello",
		"unknown PEM":     "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n",
		"damaged PKCS#1":  "-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----\n",
		"damaged public":  "-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n",
		"damaged OpenSSH": "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n",
	} {
		if _, err := sshkey.Read([]byte(data), nil); !errors.Is(err, sshkey.ErrUnreadable) {
			t.Errorf("Read(%s) = %v; want ErrUnreadable", name, err)
		}
	}
}
