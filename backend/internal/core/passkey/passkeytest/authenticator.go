// Package passkeytest is a software authenticator for tests: it answers WebAuthn registrations and sign-ins the way a browser with a
// hardware key would, with ES256 keys and no attestation.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator holds one credential.
type Authenticator struct {
	key      *ecdsa.PrivateKey
	id       []byte
	counter  uint32
	origin   string
	rpID     string
	Presence bool
}

// New returns an authenticator that answers for a relying party and an origin.
func New(rpID, origin string) *Authenticator {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &Authenticator{key: key, id: id, origin: origin, rpID: rpID, Presence: true}
}

// SetOrigin changes the origin the authenticator claims to answer from, to test answers from the wrong place.
func (a *Authenticator) SetOrigin(origin string) { a.origin = origin }

// SetCounter changes the signature counter the next answer carries.
func (a *Authenticator) SetCounter(n uint32) { a.counter = n }

// b64 encodes in the unpadded URL alphabet the protocol uses.
func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// challengeOf reads the challenge out of the options a server sent, in either shape (creation or request).
func challengeOf(options []byte) string {
	var wrapper struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	_ = json.Unmarshal(options, &wrapper)
	return wrapper.PublicKey.Challenge
}

// clientData builds the client data of a ceremony.
func (a *Authenticator) clientData(kind, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return data
}

// flags returns the flags of the authenticator data: user present, user verified, and attested data when registering.
func (a *Authenticator) flags(attested bool) byte {
	var f byte
	if a.Presence {
		f |= 0x01 | 0x04
	}
	if attested {
		f |= 0x40
	}
	return f
}

// header builds the first 37 bytes of authenticator data.
func (a *Authenticator) header(attested bool) []byte {
	hash := sha256.Sum256([]byte(a.rpID))
	out := append([]byte{}, hash[:]...)
	out = append(out, a.flags(attested))
	var counter [4]byte
	binary.BigEndian.PutUint32(counter[:], a.counter)
	return append(out, counter[:]...)
}

// Register answers the options of a registration with the JSON of the credential a browser would return.
func (a *Authenticator) Register(options []byte) []byte {
	point, _ := a.key.PublicKey.Bytes()
	x, y := point[1:33], point[33:65]
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	authData := append(a.header(true), make([]byte, 16)...)
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(a.id)))
	authData = append(append(append(authData, length[:]...), a.id...), cose...)
	attestation, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	response, _ := json.Marshal(map[string]any{
		"id": b64(a.id), "rawId": b64(a.id), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64(a.clientData("webauthn.create", challengeOf(options))), "attestationObject": b64(attestation)},
		"clientExtensionResults": map[string]any{},
	})
	return response
}

// Assert answers the options of a sign-in with the JSON of the assertion a browser would return, and moves the counter forward.
func (a *Authenticator) Assert(options []byte) []byte {
	a.counter++
	authData := a.header(false)
	clientData := a.clientData("webauthn.get", challengeOf(options))
	hash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), hash[:]...))
	signature, _ := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	response, _ := json.Marshal(map[string]any{
		"id": b64(a.id), "rawId": b64(a.id), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(signature), "userHandle": ""},
		"clientExtensionResults": map[string]any{},
	})
	return response
}
