package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/fprambs/transferly/internal/core/remotefs"
)

// TestSSHNegotiationProblem checks the actionable SSH negotiation problem and advertised algorithms.
func TestSSHNegotiationProblem(t *testing.T) {
	p, ok := asProblem(fmt.Errorf("connecting: %w", &remotefs.NegotiationError{Category: "cipher", ServerAlgorithms: []string{"aes128-cbc", "3des-cbc"}}))
	if !ok || p.status != http.StatusUnprocessableEntity || p.code != "ssh_algorithm_incompatible" || p.params["category"] != "cipher" || p.params["serverAlgorithms"] != "aes128-cbc, 3des-cbc" {
		t.Fatalf("problem = %+v, ok = %v", p, ok)
	}
}

// TestConnectionTimeoutProblem checks the HTTP status and stable code of a connection timeout.
func TestConnectionTimeoutProblem(t *testing.T) {
	p, ok := asProblem(remotefs.ErrConnectTimeout)
	if !ok || p.status != http.StatusGatewayTimeout || p.code != "remote_connection_timeout" {
		t.Fatalf("problem = %+v", p)
	}
}
