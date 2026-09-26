package vault_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/fprambs/sentinel/internal/core/vault"
)

// TestKeyPair verifies generating and sealing key pairs.
//
// Scenarios:
//   - a key pair has 32 byte halves, and two pairs differ
//   - a failing random source fails generation
//   - a private key opens with the DEK and the user it was sealed for, and with nothing else
func TestKeyPair(t *testing.T) {
	a, err := vault.GenerateKeyPair(rand.Reader)
	if err != nil || len(a.Public) != 32 || len(a.Private) != 32 {
		t.Fatalf("GenerateKeyPair() = %+v, %v", a, err)
	}
	b, _ := vault.GenerateKeyPair(rand.Reader)
	if bytes.Equal(a.Public, b.Public) || bytes.Equal(a.Private, b.Private) {
		t.Error("two key pairs are equal")
	}
	if _, err := vault.GenerateKeyPair(failingReader{}); err == nil {
		t.Error("a failing random source generated a key pair")
	}

	dek := bytes.Repeat([]byte{7}, 32)
	sealed, err := vault.SealPrivateKey(rand.Reader, dek, "alice", a.Private)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := vault.OpenPrivateKey(dek, "alice", sealed); err != nil || !bytes.Equal(got, a.Private) {
		t.Fatalf("OpenPrivateKey() = %x, %v", got, err)
	}
	if _, err := vault.OpenPrivateKey(dek, "bob", sealed); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("another user = %v; want ErrCorrupt", err)
	}
	if _, err := vault.OpenPrivateKey(bytes.Repeat([]byte{8}, 32), "alice", sealed); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("another DEK = %v; want ErrCorrupt", err)
	}
	if _, err := vault.SealPrivateKey(rand.Reader, []byte("short"), "alice", a.Private); err == nil {
		t.Error("a short DEK sealed a private key")
	}
}

// TestGrant verifies sealing a workspace key for a member.
//
// Scenarios:
//   - the recipient opens the grant, and each sealing gives a different ciphertext
//   - another member's private key, another workspace, user or version, and any tampered byte fail with ErrCorrupt
//   - malformed public keys, private keys and grants are refused, including public keys that give an all-zero secret
//   - failing random sources fail sealing
func TestGrant(t *testing.T) {
	recipient, _ := vault.GenerateKeyPair(rand.Reader)
	other, _ := vault.GenerateKeyPair(rand.Reader)
	workspaceKey := bytes.Repeat([]byte{42}, 32)
	ref := vault.GrantRef{Workspace: "ws", User: "alice", Version: 3}

	sealed, err := vault.SealTo(rand.Reader, recipient.Public, ref, workspaceKey)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := vault.OpenGrant(recipient.Private, ref, sealed); err != nil || !bytes.Equal(got, workspaceKey) {
		t.Fatalf("OpenGrant() = %x, %v", got, err)
	}
	again, _ := vault.SealTo(rand.Reader, recipient.Public, ref, workspaceKey)
	if bytes.Equal(sealed, again) {
		t.Error("two sealings gave the same bytes")
	}

	for name, c := range map[string]struct {
		private []byte
		ref     vault.GrantRef
	}{
		"another member":    {other.Private, ref},
		"another workspace": {recipient.Private, vault.GrantRef{Workspace: "other", User: "alice", Version: 3}},
		"another user":      {recipient.Private, vault.GrantRef{Workspace: "ws", User: "bob", Version: 3}},
		"another version":   {recipient.Private, vault.GrantRef{Workspace: "ws", User: "alice", Version: 2}},
	} {
		if _, err := vault.OpenGrant(c.private, c.ref, sealed); !errors.Is(err, vault.ErrCorrupt) {
			t.Errorf("%s = %v; want ErrCorrupt", name, err)
		}
	}
	for i := range sealed {
		tampered := bytes.Clone(sealed)
		tampered[i] ^= 1
		if _, err := vault.OpenGrant(recipient.Private, ref, tampered); err == nil {
			t.Fatalf("a grant with byte %d changed still opened", i)
		}
	}

	if _, err := vault.OpenGrant(recipient.Private, ref, sealed[:10]); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("a short grant = %v; want ErrCorrupt", err)
	}
	if _, err := vault.OpenGrant([]byte("short"), ref, sealed); err == nil {
		t.Error("a short private key opened a grant")
	}
	lowOrder := bytes.Repeat([]byte{0}, 32)
	forged := append(bytes.Clone(lowOrder), sealed[32:]...)
	if _, err := vault.OpenGrant(recipient.Private, ref, forged); !errors.Is(err, vault.ErrCorrupt) {
		t.Errorf("a low order ephemeral key = %v; want ErrCorrupt", err)
	}
	if _, err := vault.SealTo(rand.Reader, lowOrder, ref, workspaceKey); err == nil {
		t.Error("sealing to a low order public key worked")
	}
	if _, err := vault.SealTo(rand.Reader, []byte("short"), ref, workspaceKey); err == nil {
		t.Error("sealing to a short public key worked")
	}
	if _, err := vault.SealTo(failingReader{}, recipient.Public, ref, workspaceKey); err == nil {
		t.Error("a failing random source sealed a grant")
	}
	headless := &limitedReader{left: 34}
	if _, err := vault.SealTo(headless, recipient.Public, ref, workspaceKey); err == nil {
		t.Error("a random source that runs out before the nonce sealed a grant")
	}
}
