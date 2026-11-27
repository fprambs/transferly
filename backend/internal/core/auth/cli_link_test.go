package auth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fprambs/transferly/internal/core/auth"
)

// TestCLILinks verifies the short links to the approval page of the terminal client (ADR-0029).
//
// Scenarios:
//   - a link holds the trimmed request, the state and the address of the client, lasts 10 minutes, and its code has 8 characters
//     without lookalikes; it is shown in two groups of four
//   - the request is found by its code whatever the case, dashes and spaces, until it expires; malformed codes are not looked up
//   - a link without a redirect or a state keeps them empty
//   - requests an approval would refuse, and states with spaces, control characters or over 128 characters, are refused
//   - with the sign-in of the terminal turned off no link is made
//   - the purge removes expired links with expired approvals
func TestCLILinks(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	link, err := h.svc.CreateCLILink(ctx, cliRequest(), "st-1", terminal)
	if err != nil {
		t.Fatalf("CreateCLILink() = %v", err)
	}
	if len(link.Code) != 8 || strings.ContainsAny(link.Code, "01ILO") || strings.ToUpper(link.Code) != link.Code {
		t.Errorf("code = %q, want 8 capitals and digits without lookalikes", link.Code)
	}
	if link.Request.ClientName != "transferly-tui" || link.State != "st-1" || link.IP != terminal.IP ||
		!link.ExpiresAt.Equal(h.clock.Now().Add(10*time.Minute)) {
		t.Errorf("link = %+v, want the trimmed name, the state, the address and 10 minutes", link)
	}
	shown := auth.FormatLinkCode(link.Code)
	if shown != link.Code[:4]+"-"+link.Code[4:] || auth.FormatLinkCode("ABC") != "ABC" {
		t.Errorf("FormatLinkCode() = %q", shown)
	}

	for _, typed := range []string{link.Code, shown, strings.ToLower(shown), " " + shown[:4] + " " + shown[5:]} {
		got, err := h.svc.CLILinkRequest(ctx, typed)
		if err != nil || got.Request != link.Request || got.State != "st-1" {
			t.Errorf("CLILinkRequest(%q) = %+v, %v", typed, got, err)
		}
	}
	for _, typed := range []string{"", "ABCD-EFG", "ABCD-EFGH0", "ABCD-EF0H", "ABCD-EFG!"} {
		if _, err := h.svc.CLILinkRequest(ctx, typed); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("CLILinkRequest(%q) = %v, want ErrNotFound", typed, err)
		}
	}

	pasted := cliRequest()
	pasted.RedirectURI = ""
	plain, err := h.svc.CreateCLILink(ctx, pasted, "", terminal)
	if err != nil || plain.Request.RedirectURI != "" || plain.State != "" {
		t.Errorf("CreateCLILink(no redirect, no state) = %+v, %v", plain, err)
	}

	badRedirect := cliRequest()
	badRedirect.RedirectURI = "https://evil.example.com/callback"
	badChallenge := cliRequest()
	badChallenge.CodeChallenge = "short"
	for _, c := range []struct {
		req   auth.CLIRequest
		state string
		want  error
	}{
		{badRedirect, "", auth.ErrInvalidRedirect},
		{badChallenge, "", auth.ErrInvalidCLIRequest},
		{cliRequest(), "a b", auth.ErrInvalidCLIRequest},
		{cliRequest(), "a\nb", auth.ErrInvalidCLIRequest},
		{cliRequest(), strings.Repeat("s", 129), auth.ErrInvalidCLIRequest},
	} {
		if _, err := h.svc.CreateCLILink(ctx, c.req, c.state, terminal); !errors.Is(err, c.want) {
			t.Errorf("CreateCLILink(%q) = %v, want %v", c.state, err, c.want)
		}
	}

	store := newFakeStore()
	off := auth.NewService(store, store, store, h.hasher, h.clock, rand.Reader, auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour})
	if _, err := off.CreateCLILink(ctx, cliRequest(), "", terminal); !errors.Is(err, auth.ErrCLILoginDisabled) {
		t.Errorf("CreateCLILink(turned off) = %v", err)
	}

	h.clock.Advance(10 * time.Minute)
	if _, err := h.svc.CLILinkRequest(ctx, link.Code); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("CLILinkRequest(expired) = %v, want ErrNotFound", err)
	}
	if n, err := h.svc.PurgeCLIAuthorizations(ctx); err != nil || n != 2 {
		t.Errorf("PurgeCLIAuthorizations() = %d, %v, want the 2 links", n, err)
	}
}

// TestCLILinkCodes verifies that codes draw on the alphabet without bias, skipping bytes past its largest multiple, and that failures
// of the entropy source and of the store reach the caller.
func TestCLILinkCodes(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	config := auth.Config{IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour, CLILogin: true}
	entropy := bytes.NewReader([]byte{255, 248, 0, 1, 2, 3, 30, 31, 61, 247})
	fixed := auth.NewService(h.store, h.store, h.store, h.hasher, h.clock, entropy, config)
	link, err := fixed.CreateCLILink(ctx, cliRequest(), "", terminal)
	if err != nil || link.Code != "2345Z2ZZ" {
		t.Errorf("CreateCLILink() code = %q, %v, want 2345Z2ZZ from the bytes under 248", link.Code, err)
	}
	if _, err := fixed.CreateCLILink(ctx, cliRequest(), "", terminal); err == nil {
		t.Error("CreateCLILink() without entropy succeeded")
	}
	h.store.failing("CreateCLILink")
	if _, err := h.svc.CreateCLILink(ctx, cliRequest(), "", terminal); !errors.Is(err, errBoom) {
		t.Errorf("CreateCLILink() with a failing store = %v", err)
	}
	h.store.failing("DeleteExpiredCLILinks")
	if _, err := h.svc.PurgeCLIAuthorizations(ctx); !errors.Is(err, errBoom) {
		t.Errorf("PurgeCLIAuthorizations() with a failing store of links = %v", err)
	}
}
