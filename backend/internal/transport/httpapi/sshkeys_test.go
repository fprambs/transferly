package httpapi_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/fprambs/transferly/internal/testutil"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// keyFixture reads a key file made for the tests of the sshkey package.
func keyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../core/sshkey/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// inspect asks the API to describe a key file.
func (a *api) inspect(body map[string]any, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.send(http.MethodPost, "/api/v1/ssh-keys/inspect", body, cookie, csrf)
}

// info decodes the reply as a key description.
func (r reply) info(t *testing.T) httpapi.SshKeyInfo {
	t.Helper()
	var info httpapi.SshKeyInfo
	mustDecode(t, r.Body, &info)
	return info
}

// TestInspectSshKey covers describing key files through the API (4.8g).
//
// Use cases:
//   - a PuTTY key with its passphrase is usable, with its format, type, size and fingerprint
//   - an encrypted key without its passphrase, or with a wrong one, says so while still describing what it can
//   - a public key is described and answered as a public key
//   - a file that is no key and an Argon2d PuTTY file are refused as saving a site would refuse them, the latter with
//     its reason
//   - a key or passphrase larger than any real one is refused, and a caller who is not signed in is refused
func TestInspectSshKey(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ppk := keyFixture(t, "ppk3-rsa-argon2id.ppk")

	usable := a.inspect(map[string]any{"key": ppk, "passphrase": "correct horse"}, cookie, csrf)
	if usable.Status != http.StatusOK {
		t.Fatalf("inspect = %d %s", usable.Status, usable.Body)
	}
	info := usable.info(t)
	if !info.Private || info.Format != httpapi.Ppk3 || info.Status != httpapi.Usable || !info.Encrypted || *info.Algorithm != "ssh-rsa" || *info.Bits != 2048 || !strings.HasPrefix(*info.Fingerprint, "SHA256:") {
		t.Errorf("inspect(PuTTY key) = %+v", info)
	}
	if got := a.inspect(map[string]any{"key": ppk}, cookie, csrf).info(t); got.Status != httpapi.PassphraseRequired || got.Fingerprint == nil {
		t.Errorf("inspect without the passphrase = %+v; want passphrase_required with the fingerprint", got)
	}
	if got := a.inspect(map[string]any{"key": ppk, "passphrase": "nope"}, cookie, csrf).info(t); got.Status != httpapi.WrongPassphrase {
		t.Errorf("inspect with a wrong passphrase = %+v; want wrong_passphrase", got)
	}
	if got := a.inspect(map[string]any{"key": keyFixture(t, "openssh-ed25519.pub")}, cookie, csrf).info(t); got.Private || got.Status != httpapi.PublicKey || got.Bits == nil {
		t.Errorf("inspect(public key) = %+v; want a described public key", got)
	}
	if got := a.inspect(map[string]any{"key": keyFixture(t, "pem-rsa-encrypted")}, cookie, csrf).info(t); got.Fingerprint != nil || got.Algorithm != nil || got.Bits != nil {
		t.Errorf("inspect(encrypted PEM) = %+v; want nothing told before the passphrase", got)
	}

	expectProblem(t, a.inspect(map[string]any{"key": "hello"}, cookie, csrf), http.StatusUnprocessableEntity, "private_key_unreadable")
	argon2d := a.inspect(map[string]any{"key": keyFixture(t, "ppk3-ed25519-argon2d.ppk"), "passphrase": "correct horse"}, cookie, csrf)
	expectProblem(t, argon2d, http.StatusUnprocessableEntity, "private_key_unsupported")
	if params := argon2d.problem(t).Params; params == nil || (*params)["reason"] != "argon2d" {
		t.Errorf("unsupported params = %v; want the argon2d reason", params)
	}
	expectProblem(t, a.inspect(map[string]any{"key": strings.Repeat("A", 64*1024+1)}, cookie, csrf), http.StatusUnprocessableEntity, "secret_too_large")
	expectProblem(t, a.inspect(map[string]any{"key": ppk, "passphrase": strings.Repeat("p", 1025)}, cookie, csrf), http.StatusUnprocessableEntity, "secret_too_large")
	expectProblem(t, a.inspect(map[string]any{"key": ppk}, nil, csrf), http.StatusUnauthorized, "unauthenticated")
}

// TestSites_KeyRefusals covers the problems saving a site answers for a private key it cannot use (4.8g).
//
// Use cases:
//   - a public key given as the site's key is private_key_is_public for the site's key
//   - an encrypted key without its passphrase is passphrase_required
//   - a jump host key with a wrong passphrase is wrong_passphrase for the jump host's key
func TestSites_KeyRefusals(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	encrypted := testutil.PrivateKey(t, "phrase")

	public := secret(set(siteBody("Public key"), "auth", "key"), "private_key", keyFixture(t, "openssh-ed25519.pub"))
	refused := a.send(http.MethodPost, sitesPath(ws), public, cookie, csrf)
	expectProblem(t, refused, http.StatusUnprocessableEntity, "private_key_is_public")
	if params := refused.problem(t).Params; params == nil || (*params)["field"] != "private_key" {
		t.Errorf("params = %v; want the site's key", params)
	}

	locked := secret(set(siteBody("No passphrase"), "auth", "key"), "private_key", encrypted)
	expectProblem(t, a.send(http.MethodPost, sitesPath(ws), locked, cookie, csrf), http.StatusUnprocessableEntity, "passphrase_required")

	jump := set(siteBody("Jump"), "auth", "key", "jump_host", "1.1.1.1", "jump_username", "bastion", "jump_auth", "key")
	secret(jump, "private_key", encrypted, "passphrase", "phrase", "jump_private_key", encrypted, "jump_passphrase", "wrong")
	wrong := a.send(http.MethodPost, sitesPath(ws), jump, cookie, csrf)
	expectProblem(t, wrong, http.StatusUnprocessableEntity, "wrong_passphrase")
	if params := wrong.problem(t).Params; params == nil || (*params)["field"] != "jump_private_key" {
		t.Errorf("params = %v; want the jump host's key", params)
	}
}
