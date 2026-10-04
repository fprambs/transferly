package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/fprambs/transferly/internal/adapters/googleauth"
)

// AuthorizeGoogle starts signing a Google account in for the person, coming back to the origin of the request (ADR-0024).
func (s *server) AuthorizeGoogle(ctx context.Context, _ AuthorizeGoogleRequestObject) (AuthorizeGoogleResponseObject, error) {
	p, _ := principalFrom(ctx)
	google := s.deps.Google
	if google == nil {
		google = googleauth.New(googleauth.Config{}, nil, nil)
	}
	url, state, err := google.Authorize(p.User.ID, originFrom(ctx, s.deps.AllowedOrigins[0]))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	return AuthorizeGoogle201JSONResponse{Url: url, State: state}, nil
}

// callbackPage is the page Google sends the browser back to: it publishes the outcome of the sign-in to the pages of Transferly of
// the same origin, which keep the one whose state they asked for, and closes. The cross-origin opener policy of Transferly cuts the
// popup from the page that opened it once it visits Google, so a broadcast channel carries the outcome instead of the opener.
const callbackPage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>Transferly</title>
<p>You can close this window.</p>
<script nonce="%s">
const outcome = %s;
const channel = new BroadcastChannel("transferly-oauth");
channel.postMessage(outcome);
channel.close();
window.close();
</script>
</html>`

// googleCallback answers the page Google sends the browser back to after the consent: it finishes the sign-in its state names and
// publishes the refresh token and the email of the account, or why it failed.
func googleCallback(google *googleauth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		outcome := map[string]string{"source": "transferly-oauth", "provider": "google", "state": q.Get("state")}
		if google == nil {
			outcome["error"] = "not_configured"
		} else if result, err := google.Complete(r.Context(), q.Get("state"), q.Get("code"), q.Get("error")); err != nil {
			outcome["error"] = callbackReason(err)
		} else {
			outcome["refreshToken"], outcome["email"] = result.RefreshToken, result.Email
		}
		raw := make([]byte, 18)
		_, _ = rand.Read(raw)
		nonce := base64.StdEncoding.EncodeToString(raw)
		payload, _ := json.Marshal(outcome)
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		_, _ = fmt.Fprintf(w, callbackPage, nonce, payload)
	}
}

// callbackReason names why a sign-in failed, as the page of the callback tells the form.
func callbackReason(err error) string {
	switch {
	case errors.Is(err, googleauth.ErrUnknownState):
		return "expired"
	case errors.Is(err, googleauth.ErrDenied):
		return "denied"
	case errors.Is(err, googleauth.ErrNoRefreshToken):
		return "no_refresh_token"
	}
	return "failed"
}
