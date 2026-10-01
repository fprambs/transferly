package sshkey

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // PPK version 2 derives its keys and MAC with SHA-1; reading those files needs it.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

const (
	// ppkPrefix starts every PPK file, followed by the version.
	ppkPrefix = "PuTTY-User-Key-File-"
	// rfc4716Begin and rfc4716End frame an RFC 4716 public key.
	rfc4716Begin, rfc4716End = "---- BEGIN SSH2 PUBLIC KEY ----", "---- END SSH2 PUBLIC KEY ----"
	// maxArgon2Memory bounds the Argon2 memory of a PPK file, in KiB; PuTTYgen uses 8192.
	maxArgon2Memory = 256 * 1024
	// maxArgon2Work bounds memory times passes, in KiB, so large memory comes with few passes; PuTTYgen's defaults
	// (8192 KiB, 13 passes) use about 3 % of it.
	maxArgon2Work = 4 * 1024 * 1024
	// maxArgon2Parallelism bounds the Argon2 lanes of a PPK file; PuTTYgen uses 1.
	maxArgon2Parallelism = 16
)

// ppkAlgorithms are the key types read from PPK files. PuTTY also writes DSA and Ed448 keys, which Transferly does not
// connect with; anything else, such as a certificate, is not a PuTTY private key.
var ppkAlgorithms = []string{ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoED25519}

// readRFC4716 reads an RFC 4716 public key: header lines ("Name: value", continued with a trailing backslash) and
// then the base64 of the key, between the BEGIN and END lines.
func readRFC4716(data []byte) (Key, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	var body strings.Builder
	continued, ended := false, false
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		switch {
		case line == rfc4716End:
			ended = true
		case ended:
		case continued || strings.Contains(line, ":"):
			continued = strings.HasSuffix(line, "\\")
		default:
			body.WriteString(line)
		}
	}
	blob, err := base64.StdEncoding.DecodeString(body.String())
	if !ended || err != nil {
		return Key{}, ErrUnreadable
	}
	public, err := ssh.ParsePublicKey(blob)
	if err != nil {
		return Key{}, ErrUnreadable
	}
	return publicKey(FormatRFC4716, public)
}

// ppkFile is the outer layer of a PPK file.
type ppkFile struct {
	version    int
	algorithm  string
	encryption string
	comment    string
	public     []byte
	private    []byte
	mac        []byte
	fields     map[string]string
}

// parsePPK reads the text layer of a PPK file (PuTTY manual, appendix C.2): the header fields, the public and private
// base64 sections with their line counts, the key derivation fields and the MAC.
func parsePPK(data []byte) (ppkFile, error) {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), "\n")
	f := ppkFile{fields: map[string]string{}}
	for i := 0; i < len(lines); i++ {
		name, value, ok := strings.Cut(lines[i], ": ")
		if !ok {
			return ppkFile{}, ErrUnreadable
		}
		if name != "Public-Lines" && name != "Private-Lines" {
			f.fields[name] = value
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || i+n >= len(lines) {
			return ppkFile{}, ErrUnreadable
		}
		f.fields[name] = strings.Join(lines[i+1:i+1+n], "")
		i += n
	}
	first, algorithm, _ := strings.Cut(lines[0], ": ")
	switch strings.TrimPrefix(first, ppkPrefix) {
	case "2":
		f.version = 2
	case "3":
		f.version = 3
	default:
		return ppkFile{}, unsupported(ReasonPuTTYVersion)
	}
	var err error
	f.algorithm, f.encryption, f.comment = algorithm, f.fields["Encryption"], f.fields["Comment"]
	if f.public, err = base64.StdEncoding.DecodeString(f.fields["Public-Lines"]); err != nil {
		return ppkFile{}, ErrUnreadable
	}
	if f.private, err = base64.StdEncoding.DecodeString(f.fields["Private-Lines"]); err != nil {
		return ppkFile{}, ErrUnreadable
	}
	if f.mac, err = hex.DecodeString(f.fields["Private-MAC"]); err != nil || len(f.mac) == 0 {
		return ppkFile{}, ErrUnreadable
	}
	return f, nil
}

// readPPK reads a PuTTY private key file of version 2 or 3: its public half describes the key before any passphrase,
// the private half is decrypted and checked against the MAC, and the key is converted to OpenSSH's format.
func readPPK(data, passphrase []byte) (Key, error) {
	f, err := parsePPK(data)
	if err != nil {
		return Key{}, err
	}
	info := Info{Private: true, Format: FormatPuTTY3, Encrypted: f.encryption != "none"}
	if f.version == 2 {
		info.Format = FormatPuTTY2
	}
	public, err := ssh.ParsePublicKey(f.public)
	switch {
	case !slices.Contains(ppkAlgorithms, f.algorithm):
		return Key{}, unsupported(ReasonKeyType)
	case err != nil || public.Type() != f.algorithm:
		return Key{}, ErrUnreadable
	case f.encryption != "none" && f.encryption != "aes256-cbc":
		return Key{}, unsupported(ReasonEncryption)
	}
	describe(&info, public)
	if info.Encrypted && len(passphrase) == 0 {
		return Key{Info: info}, ErrPassphraseRequired
	}
	if !info.Encrypted {
		passphrase = nil
	}
	keys, err := f.keys(passphrase)
	if err != nil {
		return Key{}, err
	}
	plain, err := keys.decrypt(f.private)
	if err != nil {
		return Key{}, err
	}
	if !f.valid(keys, plain) {
		if info.Encrypted {
			return Key{Info: info}, ErrWrongPassphrase
		}
		return Key{}, ErrUnreadable
	}
	raw, err := ppkPrivate(public, plain)
	if err != nil {
		return Key{}, err
	}
	return converted(info, raw, f.comment, passphrase)
}

// ppkKeys is the key material of a PPK file: the AES key and IV (none when unencrypted) and the MAC key and hash.
type ppkKeys struct {
	cipher, iv, mac []byte
	hash            func() hash.Hash
}

// keys derives the key material from the passphrase (appendix C.4 for version 3, C.5.1 for version 2); an unencrypted
// file of version 3 has empty keys, one of version 2 a MAC key from the empty passphrase.
func (f ppkFile) keys(passphrase []byte) (ppkKeys, error) {
	if f.version == 2 {
		return ppk2Keys(passphrase, f.encryption != "none"), nil
	}
	if f.encryption == "none" {
		return ppkKeys{hash: sha256.New}, nil
	}
	flavor := f.fields["Key-Derivation"]
	memory, errMemory := strconv.ParseUint(f.fields["Argon2-Memory"], 10, 32)
	passes, errPasses := strconv.ParseUint(f.fields["Argon2-Passes"], 10, 32)
	lanes, errLanes := strconv.ParseUint(f.fields["Argon2-Parallelism"], 10, 8)
	salt, errSalt := hex.DecodeString(f.fields["Argon2-Salt"])
	switch {
	case errMemory != nil || errPasses != nil || errLanes != nil || errSalt != nil || passes == 0 || lanes == 0:
		return ppkKeys{}, ErrUnreadable
	case flavor == "Argon2d":
		return ppkKeys{}, unsupported(ReasonArgon2d)
	case flavor != "Argon2id" && flavor != "Argon2i":
		return ppkKeys{}, ErrUnreadable
	case memory > maxArgon2Memory || memory*passes > maxArgon2Work || lanes > maxArgon2Parallelism:
		return ppkKeys{}, unsupported(ReasonCost)
	}
	kdf := argon2.IDKey
	if flavor == "Argon2i" {
		kdf = argon2.Key
	}
	out := derive(func() []byte {
		return kdf(passphrase, salt, uint32(passes), uint32(memory), uint8(lanes), 32+16+32)
	})
	return ppkKeys{cipher: out[:32], iv: out[32:48], mac: out[48:], hash: sha256.New}, nil
}

// ppk2Keys derives the key material of a version 2 file: an AES key from two SHA-1 hashes of a sequence number and
// the passphrase, a zero IV, and a MAC key hashed from a fixed string and the passphrase.
func ppk2Keys(passphrase []byte, encrypted bool) ppkKeys {
	mac := sha1.Sum(append([]byte("putty-private-key-file-mac-key"), passphrase...)) //nolint:gosec // PPK version 2 is defined with SHA-1.
	keys := ppkKeys{mac: mac[:], hash: sha1.New}
	if !encrypted {
		return keys
	}
	var material []byte
	for seq := range uint32(2) {
		sum := sha1.Sum(append(binary.BigEndian.AppendUint32(nil, seq), passphrase...)) //nolint:gosec // PPK version 2 is defined with SHA-1.
		material = append(material, sum[:]...)
	}
	keys.cipher, keys.iv = material[:32], make([]byte, aes.BlockSize)
	return keys
}

// decrypt decrypts the private section with AES-256-CBC, or returns it as it is when the file is not encrypted.
func (k ppkKeys) decrypt(private []byte) ([]byte, error) {
	if k.cipher == nil {
		return private, nil
	}
	if len(private) == 0 || len(private)%aes.BlockSize != 0 {
		return nil, ErrUnreadable
	}
	block, _ := aes.NewCipher(k.cipher)
	plain := make([]byte, len(private))
	cipher.NewCBCDecrypter(block, k.iv).CryptBlocks(plain, private)
	return plain, nil
}

// valid checks the MAC of the file over the algorithm, encryption, comment, public key and decrypted private key.
func (f ppkFile) valid(k ppkKeys, plain []byte) bool {
	mac := hmac.New(k.hash, k.mac)
	for _, field := range [][]byte{[]byte(f.algorithm), []byte(f.encryption), []byte(f.comment), f.public, plain} {
		_, _ = mac.Write(binary.BigEndian.AppendUint32(nil, uint32(len(field))))
		_, _ = mac.Write(field)
	}
	return subtle.ConstantTimeCompare(mac.Sum(nil), f.mac) == 1
}

// ppkPrivate builds the private key from the public key, one of [ppkAlgorithms], and the decrypted private section
// (appendix C.3), and checks that both halves belong together.
func ppkPrivate(public ssh.PublicKey, plain []byte) (any, error) {
	w := &wire{b: plain}
	switch key := public.(ssh.CryptoPublicKey).CryptoPublicKey().(type) {
	case *rsa.PublicKey:
		d, p, q := w.mpint(), w.mpint(), w.mpint()
		private := &rsa.PrivateKey{PublicKey: *key, D: d, Primes: []*big.Int{p, q}}
		if w.bad || private.Validate() != nil {
			return nil, ErrUnreadable
		}
		private.Precompute()
		return private, nil
	case *ecdsa.PublicKey:
		size := (key.Curve.Params().BitSize + 7) / 8
		d := w.mpint()
		if w.bad || d.BitLen() > size*8 {
			return nil, ErrUnreadable
		}
		private, err := ecdsa.ParseRawPrivateKey(key.Curve, d.FillBytes(make([]byte, size)))
		if err != nil || !private.PublicKey.Equal(key) {
			return nil, ErrUnreadable
		}
		return private, nil
	default:
		seed := w.bytes()
		if w.bad || len(seed) != ed25519.SeedSize {
			return nil, ErrUnreadable
		}
		private := ed25519.NewKeyFromSeed(seed)
		if !bytes.Equal(private.Public().(ed25519.PublicKey), public.(ssh.CryptoPublicKey).CryptoPublicKey().(ed25519.PublicKey)) {
			return nil, ErrUnreadable
		}
		return private, nil
	}
}
