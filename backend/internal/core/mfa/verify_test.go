package mfa_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/mfa"
	"github.com/fprambs/sentinel/internal/testutil"
)

// TestService_VerifyCode verifies the second step of signing in.
//
// Scenarios:
//   - a code of the app works once: the same code again, and an older one, are refused, and the next step works
//   - a wrong code, a user without a factor and one whose setup was never confirmed are refused
//   - a recovery code works once, is accepted in lower case and with or without the dash, and the count of unused codes falls
//   - a code accepted by a request that lost the race is refused
//   - failures of the store are returned
func TestService_VerifyCode(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	secret, recovery := e.enroll(t)

	e.clock.Advance(mfa.Period * time.Second)
	code := mfa.Code(secret, e.clock.Now())
	if err := e.svc.VerifyCode(ctx, e.user, code); err != nil {
		t.Fatalf("VerifyCode() = %v", err)
	}
	if err := e.svc.VerifyCode(ctx, e.user, code); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("the same code twice = %v; want ErrInvalidCode", err)
	}
	e.clock.Advance(mfa.Period * time.Second)
	if err := e.svc.VerifyCode(ctx, e.user, mfa.Code(secret, e.clock.Now())); err != nil {
		t.Errorf("the next code = %v", err)
	}
	if err := e.svc.VerifyCode(ctx, e.user, "000000"); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("a wrong code = %v; want ErrInvalidCode", err)
	}
	if err := e.svc.VerifyCode(ctx, uuid.New(), "123456"); !errors.Is(err, mfa.ErrNotEnrolled) {
		t.Errorf("no factor = %v; want ErrNotEnrolled", err)
	}
	pending := newEnv()
	pending.begin(t)
	if err := pending.svc.VerifyCode(ctx, pending.user, "123456"); !errors.Is(err, mfa.ErrNotEnrolled) {
		t.Errorf("an unconfirmed setup = %v; want ErrNotEnrolled", err)
	}

	if err := e.svc.VerifyCode(ctx, e.user, strings.ToLower(recovery[0])); err != nil {
		t.Errorf("a recovery code in lower case = %v", err)
	}
	if err := e.svc.VerifyCode(ctx, e.user, recovery[0]); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("a recovery code twice = %v; want ErrInvalidCode", err)
	}
	if err := e.svc.VerifyCode(ctx, e.user, strings.ReplaceAll(recovery[1], "-", "")); err != nil {
		t.Errorf("a recovery code without the dash = %v", err)
	}
	if status, _ := e.svc.Status(ctx, e.user); status.RecoveryLeft != 8 {
		t.Errorf("recovery codes left = %d; want 8", status.RecoveryLeft)
	}

	e.clock.Advance(mfa.Period * time.Second)
	e.store.raced = true
	if err := e.svc.VerifyCode(ctx, e.user, mfa.Code(secret, e.clock.Now())); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("a request that lost the race = %v; want ErrInvalidCode", err)
	}
	e.store.raced = false

	for _, op := range []string{"factor", "advance", "use"} {
		failing := newEnv()
		s, _ := failing.enroll(t)
		failing.clock.Advance(mfa.Period * time.Second)
		failing.store.fail[op] = errBoom
		given := mfa.Code(s, failing.clock.Now())
		if op == "use" {
			given = "ABCDE-FGHIJ"
		}
		if err := failing.svc.VerifyCode(ctx, failing.user, given); !errors.Is(err, errBoom) {
			t.Errorf("%s failure = %v", op, err)
		}
	}
}

// TestService_Recovery verifies replacing the recovery codes.
//
// Scenarios:
//   - new codes replace the old ones, which stop working, and a user without a factor cannot ask for them
//   - failures of the store are returned
func TestService_Recovery(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	_, old := e.enroll(t)
	fresh, err := e.svc.RegenerateRecovery(ctx, e.user)
	if err != nil || len(fresh) != mfa.RecoveryCodes {
		t.Fatalf("RegenerateRecovery() = %v, %v", fresh, err)
	}
	if err := e.svc.VerifyCode(ctx, e.user, old[0]); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("an old code = %v; want ErrInvalidCode", err)
	}
	if err := e.svc.VerifyCode(ctx, e.user, fresh[0]); err != nil {
		t.Errorf("a new code = %v", err)
	}
	if _, err := e.svc.RegenerateRecovery(ctx, uuid.New()); !errors.Is(err, mfa.ErrNotEnrolled) {
		t.Errorf("without a factor = %v; want ErrNotEnrolled", err)
	}
	e.store.fail["replace"] = errBoom
	if _, err := e.svc.RegenerateRecovery(ctx, e.user); !errors.Is(err, errBoom) {
		t.Errorf("replace failure = %v", err)
	}
	e.store.fail = map[string]error{"factor": errBoom}
	if _, err := e.svc.RegenerateRecovery(ctx, e.user); !errors.Is(err, errBoom) {
		t.Errorf("lookup failure = %v", err)
	}
}

// TestService_RandomFailures verifies that a source of randomness that fails stops each operation that needs it.
//
// Scenarios:
//   - the secret, the nonce that seals it and each recovery code need random bytes, and none of them is made without
func TestService_RandomFailures(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{7}, 32)
	clock := testutil.NewFakeClock(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
	for name, budget := range map[string]int{"secret": 0, "nonce": mfa.SecretLength} {
		svc := mfa.NewService(newEnv().store, key, &zeroThen{left: budget}, clock)
		if _, _, err := svc.BeginTOTP(ctx, uuid.New(), "a@example.com"); err == nil {
			t.Errorf("no random bytes for the %s: the setup worked", name)
		}
	}
	e := newEnv()
	secret := e.begin(t)
	given := mfa.Code(secret, e.clock.Now())
	broken := mfa.NewService(e.store, key, &zeroThen{left: 0}, clock)
	if _, err := broken.ConfirmTOTP(ctx, e.user, given); err == nil {
		t.Error("no random bytes for the recovery codes: the confirmation worked")
	}
	e.enroll(t)
	if _, err := broken.RegenerateRecovery(ctx, e.user); err == nil {
		t.Error("no random bytes for the recovery codes: they were regenerated")
	}
}

// TestChallenge verifies the token that carries the password step.
//
// Scenarios:
//   - a challenge opens to its user, and to nobody else
//   - it expires after five minutes and not before
//   - garbage, a changed byte, a truncated token and a challenge signed under another instance key are refused
func TestChallenge(t *testing.T) {
	e := newEnv()
	token := e.svc.Challenge(e.user)
	if id, err := e.svc.OpenChallenge(token); err != nil || id != e.user {
		t.Fatalf("OpenChallenge() = %s, %v; want %s", id, err, e.user)
	}
	e.clock.Advance(mfa.ChallengeTTL - time.Second)
	if _, err := e.svc.OpenChallenge(token); err != nil {
		t.Errorf("a challenge just before its end = %v", err)
	}
	e.clock.Advance(time.Second)
	if _, err := e.svc.OpenChallenge(token); !errors.Is(err, mfa.ErrInvalidChallenge) {
		t.Errorf("an expired challenge = %v; want ErrInvalidChallenge", err)
	}

	fresh := e.svc.Challenge(e.user)
	flipped := []byte(fresh)
	flipped[3] ^= 1
	foreign := mfa.NewService(e.store, bytes.Repeat([]byte{9}, 32), nil, e.clock).Challenge(e.user)
	for name, bad := range map[string]string{"garbage": "not a token!", "empty": "", "flipped": string(flipped), "truncated": fresh[:20], "another key": foreign} {
		if _, err := e.svc.OpenChallenge(bad); !errors.Is(err, mfa.ErrInvalidChallenge) {
			t.Errorf("%s = %v; want ErrInvalidChallenge", name, err)
		}
	}
}
