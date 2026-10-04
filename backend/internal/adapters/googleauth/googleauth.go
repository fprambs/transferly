// Package googleauth signs Google accounts in for the Google Drive connector (ADR-0024): the authorization code flow with PKCE
// against the OAuth client the installation registered, and HTTP clients authorized with the refresh token it obtained.
package googleauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// The endpoints of Google.
const (
	DefaultAuthURL  = "https://accounts.google.com/o/oauth2/auth"
	DefaultTokenURL = "https://oauth2.googleapis.com/token"
	DefaultAPIURL   = "https://www.googleapis.com"
)

// CallbackPath is where Google sends the browser back, under the origin the sign-in started from.
const CallbackPath = "/api/v1/oauth/google/callback"

// Scopes are the permissions asked for: every file of Drive, and the email of the account to show which one is connected.
var Scopes = []string{"https://www.googleapis.com/auth/drive", "openid", "email"}

// pendingFor is how long a sign-in may take between its start and Google sending the browser back.
const pendingFor = 10 * time.Minute

// Config is the OAuth client of the installation and the endpoints of Google, which tests replace with a fake.
type Config struct {
	// ClientID and ClientSecret are those of the OAuth client the installation registered with Google.
	ClientID, ClientSecret string
	// AuthURL, TokenURL and APIURL are the consent page, the token endpoint and the origin of the APIs.
	AuthURL, TokenURL, APIURL string
}

// Enabled reports whether an OAuth client is configured.
func (c Config) Enabled() bool { return c.ClientID != "" && c.ClientSecret != "" }

// oauth returns the OAuth configuration that sends the browser back to redirect.
func (c Config) oauth(redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: redirect, Scopes: Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: c.AuthURL, TokenURL: c.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// Errors of the sign-in.
var (
	// ErrNotConfigured means the installation registered no OAuth client with Google.
	ErrNotConfigured = errors.New("no Google OAuth client is configured")
	// ErrUnknownState means the browser came back with a sign-in that was not started, already finished or too old.
	ErrUnknownState = errors.New("unknown or expired Google sign-in")
	// ErrDenied means the person declined, or Google refused the sign-in.
	ErrDenied = errors.New("access was not granted by Google")
	// ErrNoRefreshToken means Google signed the account in without a refresh token, which a site needs to connect later.
	ErrNoRefreshToken = errors.New("no refresh token was given by Google")
)

// pending is a sign-in waiting for Google to send the browser back.
type pending struct {
	user               uuid.UUID
	verifier, redirect string
	expires            time.Time
}

// Result is what a finished sign-in gives: the refresh token, the email of the account, and the origin the sign-in started from.
type Result struct {
	RefreshToken, Email, Origin string
	// User is who started the sign-in.
	User uuid.UUID
}

// Service runs the sign-ins of an installation. It is safe for concurrent use.
type Service struct {
	cfg    Config
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	open   map[string]pending
}

// New returns the sign-in service of cfg; client sends the requests to Google, now tells the time.
func New(cfg Config, client *http.Client, now func() time.Time) *Service {
	return &Service{cfg: cfg, client: client, now: now, open: map[string]pending{}}
}

// Config returns the OAuth client and the endpoints of the service.
func (s *Service) Config() Config { return s.cfg }

// Authorize starts a sign-in for user from origin and returns the URL of Google's consent page and the state that ties the answer
// to this sign-in; the browser must show the result to that state only.
func (s *Service) Authorize(user uuid.UUID, origin string) (string, string, error) {
	if !s.cfg.Enabled() {
		return "", "", ErrNotConfigured
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	state := base64.RawURLEncoding.EncodeToString(raw)
	verifier := oauth2.GenerateVerifier()
	redirect := strings.TrimSuffix(origin, "/") + CallbackPath
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, p := range s.open {
		if now.After(p.expires) {
			delete(s.open, key)
		}
	}
	s.open[state] = pending{user: user, verifier: verifier, redirect: redirect, expires: now.Add(pendingFor)}
	url := s.cfg.oauth(redirect).AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "consent"))
	return url, state, nil
}

// Complete finishes the sign-in of state with the code Google sent, or with the error it sent instead: it exchanges the code for
// tokens and reads the email of the account. A state is finished once, whatever the outcome.
func (s *Service) Complete(ctx context.Context, state, code, refused string) (Result, error) {
	s.mu.Lock()
	p, ok := s.open[state]
	delete(s.open, state)
	s.mu.Unlock()
	if !ok || s.now().After(p.expires) {
		return Result{}, ErrUnknownState
	}
	origin := strings.TrimSuffix(p.redirect, CallbackPath)
	if refused != "" || code == "" {
		return Result{Origin: origin, User: p.user}, fmt.Errorf("%w: %s", ErrDenied, refused)
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.client)
	token, err := s.cfg.oauth(p.redirect).Exchange(ctx, code, oauth2.VerifierOption(p.verifier))
	if err != nil {
		return Result{Origin: origin, User: p.user}, fmt.Errorf("%w: %w", ErrDenied, err)
	}
	if token.RefreshToken == "" {
		return Result{Origin: origin, User: p.user}, ErrNoRefreshToken
	}
	email, err := s.email(ctx, token)
	if err != nil {
		return Result{Origin: origin, User: p.user}, err
	}
	return Result{RefreshToken: token.RefreshToken, Email: email, Origin: origin, User: p.user}, nil
}

// email reads the email of the account a token belongs to.
func (s *Service) email(ctx context.Context, token *oauth2.Token) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.APIURL+"/oauth2/v3/userinfo", nil)
	token.SetAuthHeader(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reading the Google account: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var info struct {
		Email string `json:"email"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reading the Google account: answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("reading the Google account: %w", err)
	}
	return info.Email, nil
}

// Client returns an HTTP client authorized as the account of a refresh token, which refreshes its access token as it expires;
// base sends the requests, the token refreshes included, and outlives the context of the call that made the client.
func (s *Service) Client(base *http.Client, refreshToken string) *http.Client {
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, base)
	return s.cfg.oauth("").Client(ctx, &oauth2.Token{RefreshToken: refreshToken})
}
