package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// cliCodeLifetime is how long an approved code can be exchanged.
	cliCodeLifetime = 5 * time.Minute
	// cliCallbackPath is the only path of a loopback redirect address.
	cliCallbackPath = "/callback"
	// challengeLength is the length of an S256 challenge: a SHA-256 in base64url without padding.
	challengeLength = 43
	// minVerifierLength and maxVerifierLength bound a PKCE code verifier (RFC 7636, section 4.1).
	minVerifierLength, maxVerifierLength = 43, 128
	// maxClientNameRunes and maxClientHostRunes bound what the client declares about itself.
	maxClientNameRunes, maxClientHostRunes = 64, 253
	// cliLinkLifetime is how long a short link opens the approval page.
	cliLinkLifetime = 10 * time.Minute
	// cliLinkAlphabet holds the characters of a short link: digits and capitals without the lookalikes 0, 1, I, L and O.
	cliLinkAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	// cliLinkLength is how many characters a short link has; 31^8 is about 8.5 * 10^11.
	cliLinkLength = 8
	// maxStateLength bounds the state the terminal client checks on its callback.
	maxStateLength = 128
)

// CLIRequest is what the terminal client asks for when the person approves it in the browser (ADR-0029).
type CLIRequest struct {
	// CodeChallenge is the PKCE S256 challenge: base64url of the SHA-256 of the verifier, without padding.
	CodeChallenge string
	// RedirectURI is the loopback address the code goes to, http://127.0.0.1:<port>/callback or http://[::1]:<port>/callback, or
	// empty when the person pastes the code into the terminal.
	RedirectURI string
	// ClientName and ClientHost are the name of the client and the host it runs on, as it declares them.
	ClientName, ClientHost string
}

// CLIExchange is what the terminal client presents to turn an approved code into a session.
type CLIExchange struct {
	// Code is the single-use code of the approval.
	Code string
	// CodeVerifier is the PKCE verifier whose S256 challenge was approved.
	CodeVerifier string
	// RedirectURI must equal the address of the approval, empty included.
	RedirectURI string
}

// ApproveCLI records the approval of a sign-in of the terminal client by user, who confirms it with their password, and returns
// the single-use code to hand to the client. client is the browser that approved.
func (s *Service) ApproveCLI(ctx context.Context, user User, password string, req CLIRequest, client ClientInfo) (string, error) {
	if !s.config.CLILogin {
		return "", ErrCLILoginDisabled
	}
	if err := validateCLIRequest(req); err != nil {
		return "", err
	}
	if err := s.VerifyPassword(ctx, user, password); err != nil {
		return "", err
	}
	code, err := s.randomToken()
	if err != nil {
		return "", fmt.Errorf("reading approval code: %w", err)
	}
	id, err := s.newID()
	if err != nil {
		return "", fmt.Errorf("generating approval id: %w", err)
	}
	now := s.clock.Now()
	err = s.cli.CreateCLIAuthorization(ctx, CLIAuthorization{
		ID: id, UserID: user.ID, CodeHash: hashToken(code), CodeChallenge: req.CodeChallenge, RedirectURI: req.RedirectURI,
		ClientName: strings.TrimSpace(req.ClientName), ClientHost: strings.TrimSpace(req.ClientHost), IP: client.IP,
		CreatedAt: now, ExpiresAt: now.Add(cliCodeLifetime),
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// CreateCLILink registers a request of the terminal client under a short code for /cli/a/<code>, valid for 10 minutes. client is
// the terminal client. The request is checked as an approval checks it, and the state must be printable without spaces.
func (s *Service) CreateCLILink(ctx context.Context, req CLIRequest, state string, client ClientInfo) (CLILink, error) {
	if !s.config.CLILogin {
		return CLILink{}, ErrCLILoginDisabled
	}
	if err := validateCLIRequest(req); err != nil {
		return CLILink{}, err
	}
	if len(state) > maxStateLength || strings.IndexFunc(state, func(r rune) bool { return !unicode.IsPrint(r) || unicode.IsSpace(r) }) >= 0 {
		return CLILink{}, ErrInvalidCLIRequest
	}
	code, err := s.linkCode()
	if err != nil {
		return CLILink{}, fmt.Errorf("reading link code: %w", err)
	}
	now := s.clock.Now()
	req.ClientName, req.ClientHost = strings.TrimSpace(req.ClientName), strings.TrimSpace(req.ClientHost)
	link := CLILink{Code: code, Request: req, State: state, IP: client.IP, CreatedAt: now, ExpiresAt: now.Add(cliLinkLifetime)}
	if err := s.cli.CreateCLILink(ctx, link); err != nil {
		return CLILink{}, err
	}
	return link, nil
}

// CLILinkRequest returns the request behind a short code, as typed: case, dashes and spaces do not matter. An unknown or expired
// code is [ErrNotFound].
func (s *Service) CLILinkRequest(ctx context.Context, code string) (CLILink, error) {
	code = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	if len(code) != cliLinkLength || strings.Trim(code, cliLinkAlphabet) != "" {
		return CLILink{}, ErrNotFound
	}
	return s.cli.CLILink(ctx, code, s.clock.Now())
}

// FormatLinkCode shows a short code in two groups of four, such as K7P2-9QXM.
func FormatLinkCode(code string) string {
	if len(code) != cliLinkLength {
		return code
	}
	return code[:cliLinkLength/2] + "-" + code[cliLinkLength/2:]
}

// linkCode returns cliLinkLength random characters of cliLinkAlphabet, without bias: bytes past the largest multiple of the alphabet
// are drawn again.
func (s *Service) linkCode() (string, error) {
	limit := byte(256 / len(cliLinkAlphabet) * len(cliLinkAlphabet))
	code := make([]byte, 0, cliLinkLength)
	raw := make([]byte, 1)
	for len(code) < cliLinkLength {
		if _, err := io.ReadFull(s.random, raw); err != nil {
			return "", err
		}
		if raw[0] < limit {
			code = append(code, cliLinkAlphabet[int(raw[0])%len(cliLinkAlphabet)])
		}
	}
	return string(code), nil
}

// ExchangeCLI turns an approved code into a session of the terminal client and returns its token and principal. client is the
// terminal client. Every reason to refuse is [ErrInvalidGrant], and a code is spent by the first exchange, valid or not.
func (s *Service) ExchangeCLI(ctx context.Context, ex CLIExchange, client ClientInfo) (string, Principal, error) {
	if !s.config.CLILogin {
		return "", Principal{}, ErrCLILoginDisabled
	}
	now := s.clock.Now()
	approval, err := s.cli.UseCLIAuthorization(ctx, hashToken(ex.Code), now)
	if errors.Is(err, ErrNotFound) {
		return "", Principal{}, ErrInvalidGrant
	}
	if err != nil {
		return "", Principal{}, err
	}
	if !now.Before(approval.ExpiresAt) || ex.RedirectURI != approval.RedirectURI || !MatchesS256Verifier(ex.CodeVerifier, approval.CodeChallenge) {
		return "", Principal{}, ErrInvalidGrant
	}
	user, err := s.users.UserByID(ctx, approval.UserID)
	if errors.Is(err, ErrNotFound) {
		return "", Principal{}, ErrInvalidGrant
	}
	if err != nil {
		return "", Principal{}, err
	}
	if user.Disabled {
		return "", Principal{}, ErrInvalidGrant
	}
	template := Session{Kind: SessionCLI, Name: approval.ClientName, Host: approval.ClientHost}
	return s.startSession(ctx, user, client, template, s.config.CLIAbsoluteTimeout)
}

// CLISessions returns the sessions of the terminal client of a user, most recently used first.
func (s *Service) CLISessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	return s.sessions.ListUserSessions(ctx, userID, SessionCLI)
}

// RevokeSession ends one session of a user; the session of another user, or one that no longer exists, is [ErrNotFound].
func (s *Service) RevokeSession(ctx context.Context, userID, id uuid.UUID) error {
	deleted, err := s.sessions.DeleteUserSession(ctx, userID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

// PurgeCLIAuthorizations deletes approvals whose code expired and short links that expired, and returns how many were removed.
func (s *Service) PurgeCLIAuthorizations(ctx context.Context) (int, error) {
	now := s.clock.Now()
	approvals, err := s.cli.DeleteExpiredCLIAuthorizations(ctx, now)
	if err != nil {
		return approvals, err
	}
	links, err := s.cli.DeleteExpiredCLILinks(ctx, now)
	return approvals + links, err
}

// randomToken returns tokenLength random bytes in base64url without padding.
func (s *Service) randomToken() (string, error) {
	raw := make([]byte, tokenLength)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// validateCLIRequest checks the challenge, the redirect address and what the client declares about itself.
func validateCLIRequest(req CLIRequest) error {
	if !ValidS256Challenge(req.CodeChallenge) || !validLabel(req.ClientName, 1, maxClientNameRunes) || !validLabel(req.ClientHost, 0, maxClientHostRunes) {
		return ErrInvalidCLIRequest
	}
	if req.RedirectURI != "" && !validRedirect(req.RedirectURI) {
		return ErrInvalidRedirect
	}
	return nil
}

// ValidS256Challenge reports whether c is an S256 challenge: 43 characters of base64url that decode to 32 bytes.
func ValidS256Challenge(c string) bool {
	if len(c) != challengeLength {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	return err == nil && len(raw) == sha256.Size
}

// validLabel reports whether s, trimmed, has between minRunes and maxRunes printable characters and no control characters.
func validLabel(s string, minRunes, maxRunes int) bool {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if !utf8.ValidString(s) || n < minRunes || n > maxRunes {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) < 0
}

// validRedirect reports whether raw is http://127.0.0.1:<port>/callback or http://[::1]:<port>/callback with a port from 1 to
// 65535, and nothing else: no other host, user, query or fragment (RFC 8252, section 7.3).
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != cliCallbackPath || u.RawPath != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil || (addr != netip.MustParseAddr("127.0.0.1") && addr != netip.IPv6Loopback()) {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port >= 1 && port <= 65535 && strconv.Itoa(port) == u.Port()
}

// MatchesS256Verifier reports whether verifier is a well-formed PKCE verifier whose S256 challenge is challenge, in constant time.
func MatchesS256Verifier(verifier, challenge string) bool {
	if len(verifier) < minVerifierLength || len(verifier) > maxVerifierLength {
		return false
	}
	for _, r := range verifier {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~", r) {
			return false
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}
