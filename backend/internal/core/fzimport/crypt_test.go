package fzimport_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/pbkdf2"

	"github.com/fprambs/transferly/internal/core/fzimport"
)

const cryptVectorPubKey = "Uh7F4NQk+KMEVO7RwEtpGxNxQICL3+7XZSHL8NwEOxY02GXqpPVEJUbOrjlSi7Vl6R5u7I81x7uTGOrdY0BD8A=="
const cryptVectorPass = "YrYsSF4x6gsDNnLlknBWKlC6ZXJk2pfVf2ScQ04UDTeq+2qWAU/DbM95OhRmU7NvSwfhr+KGO9Hswd094Prm+S/JEXs14bxUkPS7C/sEnaj9kp2BqBgyXHz1QG7f8A=="

// TestDecryptCryptPassword verifies FileZilla's encoding="crypt" passwords.
//
// Scenarios:
//   - the master password of the site manager opens a known test vector
//   - a password FileZilla padded with zero bytes to 16 bytes opens without them; passwords of 16 bytes or more open as they are
//   - a wrong master password is refused
func TestDecryptCryptPassword(t *testing.T) {
	got, err := fzimport.DecryptCryptPassword("e2e-master", cryptVectorPubKey, cryptVectorPass)
	if err != nil || got != "site-secret-42" {
		t.Fatalf("DecryptCryptPassword() = %q, %v; want site-secret-42", got, err)
	}
	for _, tc := range []struct{ plain, want string }{
		{"short\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00", "short"},
		{"exactly-16-bytes", "exactly-16-bytes"},
		{"a password longer than sixteen", "a password longer than sixteen"},
	} {
		pub, pass := sealCrypt(t, "master", filled(32, 7), filled(32, 8), []byte(tc.plain))
		if got, err := fzimport.DecryptCryptPassword("master", pub, pass); err != nil || got != tc.want {
			t.Errorf("padded %q = %q, %v; want %q", tc.plain, got, err, tc.want)
		}
	}
	if _, err := fzimport.DecryptCryptPassword("wrong", cryptVectorPubKey, cryptVectorPass); err == nil {
		t.Fatal("wrong master password should fail")
	}
}

// TestParse_Crypt verifies crypt passwords in a site manager file.
//
// Scenarios:
//   - without a master password the password is skipped
//   - with the master password the password is read
func TestParse_Crypt(t *testing.T) {
	const doc = `<FileZilla3><Servers><Server>
		<Host>h.example.com</Host><Protocol>0</Protocol><Logontype>1</Logontype><User>u</User>
		<Pass encoding="crypt" pubkey="` + cryptVectorPubKey + `">` + cryptVectorPass + `</Pass><Name>Crypt</Name>
	</Server></Servers></FileZilla3>`
	without, _, err := fzimport.Parse(strings.NewReader(doc))
	if err != nil || len(without) != 1 || !without[0].PasswordSkipped || without[0].Password != "" {
		t.Fatalf("without master = %+v, %v", without, err)
	}
	with, _, err := fzimport.ParseWithOptions(strings.NewReader(doc), fzimport.ParseOptions{MasterPassword: "e2e-master"})
	if err != nil || len(with) != 1 || with[0].Password != "site-secret-42" || with[0].PasswordSkipped {
		t.Fatalf("with master = %+v, %v", with, err)
	}
}

// sealCrypt seals a password as FileZilla does for a master password, with a key salt and an ephemeral key of the test's choice, and
// returns the pubkey attribute and the text of the Pass element.
func sealCrypt(t *testing.T, master string, salt, ephemeral, plain []byte) (string, string) {
	t.Helper()
	priv := pbkdf2.Key([]byte(master), salt, 100_000, 32, sha256.New)
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, _ := curve25519.X25519(priv, curve25519.Basepoint)
	ephemeralPub, _ := curve25519.X25519(ephemeral, curve25519.Basepoint)
	shared, _ := curve25519.X25519(ephemeral, pub)
	ephemeralSalt := make([]byte, 32)
	hash := func(kind byte) []byte {
		h := sha256.New()
		for _, part := range [][]byte{ephemeralSalt, {kind}, shared, ephemeralPub, pub, salt} {
			h.Write(part)
		}
		return h.Sum(nil)
	}
	block, _ := aes.NewCipher(hash(0))
	gcm, _ := cipher.NewGCM(block)
	sealed := gcm.Seal(nil, hash(2)[:12], plain, nil)
	blob := append(append(append([]byte{}, ephemeralPub...), ephemeralSalt...), sealed...)
	return base64.StdEncoding.EncodeToString(append(pub, salt...)), base64.RawStdEncoding.EncodeToString(blob)
}

// filled returns n bytes of one value.
func filled(n int, value byte) []byte {
	return []byte(strings.Repeat(string([]byte{value}), n))
}

// TestDecryptCryptPassword_Refusals verifies that damaged or hostile crypt passwords are refused, never trusted.
//
// Use cases:
//   - a password sealed by the test with a padding-free text opens, as FileZilla writes some fields
//   - empty and invalid base64 in the pubkey or the password, a pubkey of the wrong length and a password too short are refused
//   - an ephemeral key of low order is refused before deriving the AES key
//   - a password that decrypts to bytes that are not UTF-8 is refused
func TestDecryptCryptPassword_Refusals(t *testing.T) {
	salt := filled(32, 7)
	pub, pass := sealCrypt(t, "master", salt, filled(32, 9), []byte("ok"))
	if got, err := fzimport.DecryptCryptPassword("master", pub, pass); err != nil || got != "ok" {
		t.Fatalf("sealed by the test = %q, %v", got, err)
	}
	cases := map[string][2]string{
		"empty pubkey":     {"  ", pass},
		"invalid pubkey":   {"!!!!", pass},
		"invalid password": {pub, "!!!!"},
		"short pubkey":     {base64.StdEncoding.EncodeToString(filled(10, 1)), pass},
		"short password":   {pub, base64.StdEncoding.EncodeToString(filled(40, 1))},
		"low order key":    {pub, base64.StdEncoding.EncodeToString(append(filled(64, 0), filled(16, 1)...))},
	}
	for name, c := range cases {
		if _, err := fzimport.DecryptCryptPassword("master", c[0], c[1]); !errors.Is(err, fzimport.ErrCryptPassword) {
			t.Errorf("%s = %v", name, err)
		}
	}
	pub, pass = sealCrypt(t, "master", salt, filled(32, 9), []byte{0xff, 0xfe})
	if _, err := fzimport.DecryptCryptPassword("master", pub, pass); !errors.Is(err, fzimport.ErrCryptPassword) {
		t.Errorf("not UTF-8 = %v", err)
	}
}

// TestParse_CryptKeys verifies that the crypt passwords of a file cost one key derivation per key, and that a file with too many keys
// keeps its passwords sealed.
//
// Use cases:
//   - several sites sealed with the same key all open
//   - a password that does not open is skipped and the site is still read
//   - the sites of a fifth key are read with their passwords skipped
func TestParse_CryptKeys(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("<FileZilla3><Servers>")
	server := func(name, pub, pass string) {
		doc.WriteString(`<Server><Host>h.example.com</Host><Protocol>0</Protocol><Logontype>1</Logontype><User>u</User><Pass encoding="crypt" pubkey="` +
			pub + `">` + pass + `</Pass><Name>` + name + `</Name></Server>`)
	}
	for key := range 5 {
		pub, pass := sealCrypt(t, "master", filled(32, byte(key+1)), filled(32, 9), []byte("secret"))
		server("key"+string(rune('0'+key)), pub, pass)
		if key == 0 {
			server("same key", pub, pass)
			server("broken", pub, base64.StdEncoding.EncodeToString(filled(90, 3)))
		}
	}
	doc.WriteString("</Servers></FileZilla3>")
	servers, _, err := fzimport.ParseWithOptions(strings.NewReader(doc.String()), fzimport.ParseOptions{MasterPassword: "master"})
	if err != nil || len(servers) != 7 {
		t.Fatalf("Parse() = %d servers, %v", len(servers), err)
	}
	got := map[string]string{}
	for _, s := range servers {
		got[s.Name] = s.Password
		if s.PasswordSkipped {
			got[s.Name] = "skipped"
		}
	}
	want := map[string]string{"key0": "secret", "same key": "secret", "broken": "skipped", "key1": "secret", "key2": "secret", "key3": "secret", "key4": "skipped"}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q; want %q", name, got[name], value)
		}
	}
}
