package sshkey

import (
	"encoding/binary"
	"encoding/pem"
	"math/big"

	"golang.org/x/crypto/ssh"
)

const (
	// opensshMagic starts the body of an "OPENSSH PRIVATE KEY" block.
	opensshMagic = "openssh-key-v1\x00"
	// maxBcryptRounds bounds the bcrypt rounds of an encrypted OpenSSH key; ssh-keygen uses 16 unless told otherwise
	// with -a, and each round costs about as much as the last.
	maxBcryptRounds = 1024
	// concurrentDerivations is how many passphrase key derivations may run at once.
	concurrentDerivations = 2
)

// derivations admits passphrase key derivations a few at a time.
var derivations = make(chan struct{}, concurrentDerivations)

// derive runs a passphrase key derivation once one of the few slots is free.
func derive[T any](f func() T) T {
	derivations <- struct{}{}
	defer func() { <-derivations }()
	return f()
}

// readOpenSSH reads an "OPENSSH PRIVATE KEY" block. Its header is checked first: the public key it carries in clear
// text describes the key before any passphrase, and a bcrypt round count beyond [maxBcryptRounds] is refused before
// the library would spend it.
func readOpenSSH(data []byte, block *pem.Block, passphrase []byte) (Key, error) {
	if len(block.Bytes) < len(opensshMagic) || string(block.Bytes[:len(opensshMagic)]) != opensshMagic {
		return Key{}, ErrUnreadable
	}
	w := &wire{b: block.Bytes[len(opensshMagic):]}
	w.bytes()
	kdf := string(w.bytes())
	options := &wire{b: w.bytes()}
	if w.uint32() != 1 {
		return Key{}, ErrUnreadable
	}
	public, err := ssh.ParsePublicKey(w.bytes())
	if err != nil || w.bad {
		return Key{}, ErrUnreadable
	}
	if kdf == "bcrypt" {
		options.bytes()
		rounds := options.uint32()
		switch {
		case options.bad:
			return Key{}, ErrUnreadable
		case rounds > maxBcryptRounds:
			return Key{}, unsupported(ReasonCost)
		}
	}
	read := derive(func() result {
		key, err := readNative(FormatOpenSSH, data, passphrase, public)
		return result{key, err}
	})
	return read.key, read.err
}

// result carries a key and an error out of [derive].
type result struct {
	key Key
	err error
}

// wire reads the SSH wire encoding of RFC 4251: uint32, string and mpint. A read past the end marks it bad and
// returns zero values, so a sequence of reads is checked once at the end.
type wire struct {
	b   []byte
	bad bool
}

// uint32 reads a big-endian 32-bit integer.
func (w *wire) uint32() uint32 {
	if len(w.b) < 4 {
		w.bad = true
		return 0
	}
	v := binary.BigEndian.Uint32(w.b)
	w.b = w.b[4:]
	return v
}

// bytes reads a string: a 32-bit length and that many bytes.
func (w *wire) bytes() []byte {
	n := w.uint32()
	if w.bad || uint64(n) > uint64(len(w.b)) {
		w.bad = true
		return nil
	}
	v := w.b[:n]
	w.b = w.b[n:]
	return v
}

// mpint reads a multiple precision integer; the keys this package reads hold only positive ones.
func (w *wire) mpint() *big.Int {
	return new(big.Int).SetBytes(w.bytes())
}
