package sftpfs

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// safeHostAlgorithms intersects the secure policy with trusted identities. An empty
// intersection must never become nil: nil would enable the library defaults.
func safeHostAlgorithms(trusted []string) []string {
	safe := ssh.SupportedAlgorithms().HostKeys
	if len(trusted) == 0 {
		return safe
	}
	out := []string{"transferly-no-compatible-trusted-key"}
	for _, algorithm := range trusted {
		if slices.Contains(safe, algorithm) {
			out = append(out, algorithm)
		}
	}
	return out
}

// safeSigner prevents the client's RSA signer falling back to SHA-1 authentication.
func safeSigner(signer ssh.Signer) (ssh.Signer, error) {
	algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("signer does not support algorithm restrictions")
	}
	var allowed []string
	for _, algorithm := range ssh.SupportedAlgorithms().PublicKeyAuths {
		if signer.PublicKey().Type() == algorithm || (signer.PublicKey().Type() == ssh.KeyAlgoRSA && (algorithm == ssh.KeyAlgoRSASHA256 || algorithm == ssh.KeyAlgoRSASHA512)) {
			allowed = append(allowed, algorithm)
		}
	}
	return ssh.NewSignerWithAlgorithms(algorithmSigner, allowed)
}

// negotiationError bounds untrusted advertisements before passing them to logs and the UI.
func negotiationError(e *ssh.AlgorithmNegotiationError) error {
	category := "key_exchange"
	switch {
	case e.What == "public key signature algorithm":
		category = "authentication_signature"
	case e.What == "host key":
		category = "host_signature"
	case strings.Contains(e.What, "cipher"):
		category = "cipher"
	case strings.Contains(e.What, "MAC"):
		category = "mac"
	}
	var algorithms []string
	for _, algorithm := range e.RequestedAlgorithms {
		if len(algorithms) == 64 {
			break
		}
		if len(algorithm) > 128 {
			algorithm = algorithm[:128]
		}
		algorithm = strings.Map(func(r rune) rune {
			if r < 33 || r > 126 {
				return -1
			}
			return r
		}, algorithm)
		algorithms = append(algorithms, algorithm)
	}
	return &remotefs.NegotiationError{Category: category, ServerAlgorithms: algorithms}
}

// The library's pre-extension RSA fallback predates its typed negotiation error.
// Match only its fixed diagnostic, never classify arbitrary authentication text.
var signatureFallback = regexp.MustCompile(`ssh: no common public key signature algorithm, server only supports "([A-Za-z0-9@._+-]{1,128})" for key type `)

// authenticationNegotiation classifies the fixed diagnostic used by the SSH RSA fallback.
func authenticationNegotiation(err error) error {
	match := signatureFallback.FindStringSubmatch(err.Error())
	if match == nil {
		return nil
	}
	return &remotefs.NegotiationError{Category: "authentication_signature", ServerAlgorithms: []string{match[1]}}
}
