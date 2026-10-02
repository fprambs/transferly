package fzimport_test

import (
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/core/fzimport"
)

const cryptVectorPubKey = "Uh7F4NQk+KMEVO7RwEtpGxNxQICL3+7XZSHL8NwEOxY02GXqpPVEJUbOrjlSi7Vl6R5u7I81x7uTGOrdY0BD8A=="
const cryptVectorPass = "YrYsSF4x6gsDNnLlknBWKlC6ZXJk2pfVf2ScQ04UDTeq+2qWAU/DbM95OhRmU7NvSwfhr+KGO9Hswd094Prm+S/JEXs14bxUkPS7C/sEnaj9kp2BqBgyXHz1QG7f8A=="

// TestDecryptCryptPassword verifies FileZilla's encoding="crypt" passwords.
//
// Scenarios:
//   - the master password of the site manager opens a known test vector
//   - a wrong master password is refused
func TestDecryptCryptPassword(t *testing.T) {
	got, err := fzimport.DecryptCryptPassword("e2e-master", cryptVectorPubKey, cryptVectorPass)
	if err != nil || got != "site-secret-42" {
		t.Fatalf("DecryptCryptPassword() = %q, %v; want site-secret-42", got, err)
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
