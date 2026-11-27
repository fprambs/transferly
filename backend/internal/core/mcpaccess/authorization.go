package mcpaccess

import (
	"errors"
	"time"

	"github.com/fprambs/transferly/internal/core/auth"
)

// ClaudeRedirectURI is the callback for Claude's preregistered OAuth client.
const ClaudeRedirectURI = "https://claude.ai/api/mcp/auth_callback"

// ErrInvalidAuthorization means a request does not match the registered client or its approval.
var ErrInvalidAuthorization = errors.New("invalid MCP authorization")

// OAuthClient describes the one preregistered MCP client and the server resource.
type OAuthClient struct {
	// ID is the configured client identifier.
	ID string
	// Resource is the canonical HTTPS MCP endpoint.
	Resource string
}

// AuthorizationRequest holds the security-sensitive settings of a browser approval.
type AuthorizationRequest struct {
	// ClientID identifies the OAuth client.
	ClientID string
	// RedirectURI is where the browser returns the approval code.
	RedirectURI string
	// Resource is the MCP endpoint being authorized.
	Resource string
	// ResponseType is the OAuth authorization response type.
	ResponseType string
	// Challenge is the PKCE challenge.
	Challenge string
	// ChallengeMethod names the challenge algorithm.
	ChallengeMethod string
	// State is the request correlation value supplied by the client.
	State string
}

// Validate checks the request before consent or any browser redirect.
func (c OAuthClient) Validate(request AuthorizationRequest) error {
	if c.ID == "" || c.Resource == "" || request.ClientID != c.ID ||
		request.Resource != c.Resource || request.RedirectURI != ClaudeRedirectURI ||
		request.ResponseType != "code" || request.ChallengeMethod != "S256" ||
		!auth.ValidS256Challenge(request.Challenge) || request.State == "" || len(request.State) > 1024 {
		return ErrInvalidAuthorization
	}
	return nil
}

// CodeExchange holds the OAuth parameters bound to the approved code.
type CodeExchange struct {
	// ClientID is the registered client redeeming the code.
	ClientID string
	// RedirectURI is the callback used during approval.
	RedirectURI string
	// Resource is the authorized MCP endpoint.
	Resource string
	// Verifier proves possession of the approved PKCE secret.
	Verifier string
}

// MatchesExchange checks the approved request against an exchange before a store consumes the code atomically.
// The store must enforce single use and expiration in the same transaction that grants access.
func (c OAuthClient) MatchesExchange(approved AuthorizationRequest, exchange CodeExchange, expires, now time.Time) bool {
	return c.Validate(approved) == nil && now.Before(expires) &&
		exchange.ClientID == approved.ClientID && exchange.RedirectURI == approved.RedirectURI &&
		exchange.Resource == approved.Resource && auth.MatchesS256Verifier(exchange.Verifier, approved.Challenge)
}
