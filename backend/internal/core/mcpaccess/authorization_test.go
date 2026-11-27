package mcpaccess

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// TestOAuthBinding verifies that authorization and exchange use the registered resource, callback, and PKCE proof.
func TestOAuthBinding(t *testing.T) {
	client := OAuthClient{ID: "claude", Resource: "https://files.example.invalid/mcp"}
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	request := AuthorizationRequest{
		ClientID: client.ID, Resource: client.Resource, RedirectURI: ClaudeRedirectURI,
		ResponseType: "code", Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), ChallengeMethod: "S256", State: "state",
	}
	exchange := CodeExchange{ClientID: client.ID, Resource: client.Resource, RedirectURI: ClaudeRedirectURI, Verifier: verifier}
	now := time.Now()
	expires := now.Add(5 * time.Minute)
	if err := client.Validate(request); err != nil || !client.MatchesExchange(request, exchange, expires, now) {
		t.Fatal("valid request was rejected")
	}
	wrong := request
	wrong.RedirectURI += "/"
	if client.Validate(wrong) != ErrInvalidAuthorization {
		t.Fatal("unregistered redirect was accepted")
	}
	wrong = request
	wrong.ChallengeMethod = "plain"
	if client.Validate(wrong) != ErrInvalidAuthorization {
		t.Fatal("plain PKCE was accepted")
	}
	for _, invalid := range []CodeExchange{
		{ClientID: "other", Resource: exchange.Resource, RedirectURI: exchange.RedirectURI, Verifier: verifier},
		{ClientID: exchange.ClientID, Resource: exchange.Resource + "/other", RedirectURI: exchange.RedirectURI, Verifier: verifier},
		{ClientID: exchange.ClientID, Resource: exchange.Resource, RedirectURI: exchange.RedirectURI + "/", Verifier: verifier},
		{ClientID: exchange.ClientID, Resource: exchange.Resource, RedirectURI: exchange.RedirectURI, Verifier: strings.Repeat("b", 43)},
	} {
		if client.MatchesExchange(request, invalid, expires, now) {
			t.Fatal("mismatched exchange was accepted")
		}
	}
	if client.MatchesExchange(request, exchange, expires, expires) {
		t.Fatal("expired exchange was accepted")
	}
}
