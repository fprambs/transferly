package mfa_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/mfa"
	"github.com/fprambs/transferly/internal/testutil"
)

// errBoom is the failure injected by the fake store.
var errBoom = errors.New("boom")

// fakeStore keeps second factors in memory.
type fakeStore struct {
	factors  map[uuid.UUID]mfa.Factor
	recovery map[uuid.UUID]map[string]bool
	fail     map[string]error
	raced    bool
}

// failure returns the error injected for an operation.
func (f *fakeStore) failure(op string) error { return f.fail[op] }

// Factor returns the factor of a user.
func (f *fakeStore) Factor(_ context.Context, id uuid.UUID) (mfa.Factor, error) {
	if err := f.failure("factor"); err != nil {
		return mfa.Factor{}, err
	}
	factor, ok := f.factors[id]
	if !ok {
		return mfa.Factor{}, mfa.ErrNotEnrolled
	}
	return factor, nil
}

// SaveFactor stores an unconfirmed secret and forgets the recovery codes.
func (f *fakeStore) SaveFactor(_ context.Context, id uuid.UUID, sealed []byte) error {
	if err := f.failure("save"); err != nil {
		return err
	}
	f.factors[id] = mfa.Factor{Sealed: sealed}
	return nil
}

// ConfirmFactor confirms the secret and stores the recovery codes.
func (f *fakeStore) ConfirmFactor(_ context.Context, id uuid.UUID, step int64, recovery [][]byte) error {
	if err := f.failure("confirm"); err != nil {
		return err
	}
	factor := f.factors[id]
	factor.Confirmed, factor.LastStep = true, step
	f.factors[id] = factor
	f.setRecovery(id, recovery)
	return nil
}

// setRecovery replaces the recovery codes of a user.
func (f *fakeStore) setRecovery(id uuid.UUID, hashes [][]byte) {
	codes := map[string]bool{}
	for _, h := range hashes {
		codes[string(h)] = false
	}
	f.recovery[id] = codes
}

// AdvanceStep records a newer step; with raced set, another request wins.
func (f *fakeStore) AdvanceStep(_ context.Context, id uuid.UUID, step int64) (bool, error) {
	if err := f.failure("advance"); err != nil {
		return false, err
	}
	factor := f.factors[id]
	if f.raced || step <= factor.LastStep {
		return false, nil
	}
	factor.LastStep = step
	f.factors[id] = factor
	return true, nil
}

// DeleteFactor removes the factor and the recovery codes.
func (f *fakeStore) DeleteFactor(_ context.Context, id uuid.UUID) error {
	if err := f.failure("delete"); err != nil {
		return err
	}
	delete(f.factors, id)
	return nil
}

// ReplaceRecovery stores a new set of recovery codes.
func (f *fakeStore) ReplaceRecovery(_ context.Context, id uuid.UUID, hashes [][]byte) error {
	if err := f.failure("replace"); err != nil {
		return err
	}
	f.setRecovery(id, hashes)
	return nil
}

// UseRecovery marks a code used.
func (f *fakeStore) UseRecovery(_ context.Context, id uuid.UUID, hash []byte) (bool, error) {
	if err := f.failure("use"); err != nil {
		return false, err
	}
	used, ok := f.recovery[id][string(hash)]
	if !ok || used {
		return false, nil
	}
	f.recovery[id][string(hash)] = true
	return true, nil
}

// RecoveryLeft counts the unused codes.
func (f *fakeStore) RecoveryLeft(_ context.Context, id uuid.UUID) (int, error) {
	if err := f.failure("left"); err != nil {
		return 0, err
	}
	n := 0
	for _, used := range f.recovery[id] {
		if !used {
			n++
		}
	}
	return n, nil
}

// env is a service over the fake store with a controllable clock.
type env struct {
	svc   *mfa.Service
	store *fakeStore
	clock *testutil.FakeClock
	user  uuid.UUID
}

// newEnv builds the environment.
func newEnv() *env {
	store := &fakeStore{factors: map[uuid.UUID]mfa.Factor{}, recovery: map[uuid.UUID]map[string]bool{}, fail: map[string]error{}}
	clock := testutil.NewFakeClock(time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
	return &env{svc: mfa.NewService(store, bytes.Repeat([]byte{7}, 32), rand.Reader, clock), store: store, clock: clock, user: uuid.New()}
}

// enroll runs the whole enrolment and returns the secret and the recovery codes.
func (e *env) enroll(t *testing.T) ([]byte, []string) {
	t.Helper()
	secret := e.begin(t)
	codes, err := e.svc.ConfirmTOTP(context.Background(), e.user, mfa.Code(secret, e.clock.Now()))
	if err != nil {
		t.Fatalf("ConfirmTOTP() = %v", err)
	}
	return secret, codes
}

// decode reads a base32 secret.
func decode(encoded string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
}

// begin starts the enrolment and returns the raw secret read back from the store.
func (e *env) begin(t *testing.T) []byte {
	t.Helper()
	encoded, _, err := e.svc.BeginTOTP(context.Background(), e.user, "ana@example.com")
	if err != nil {
		t.Fatalf("BeginTOTP() = %v", err)
	}
	secret, err := decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

// TestCode verifies the codes against the test vectors of RFC 6238.
//
// Scenarios:
//   - the six digit codes of the RFC 6238 appendix B vectors for HMAC-SHA-1 are produced
//   - the current step and one step either side are accepted, two steps away are not, and the step of the match is returned
//   - codes with spaces or dashes are accepted, and codes that are not six digits are refused
func TestCode(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		if got := mfa.Code(secret, time.Unix(unix, 0)); got != want {
			t.Errorf("Code(%d) = %s; want %s", unix, got, want)
		}
	}
	now := time.Unix(1111111109, 0)
	current := mfa.Step(now)
	for offset, ok := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		given := mfa.Code(secret, time.Unix((current+offset)*mfa.Period, 0))
		step, matched := mfa.Verify(secret, given, now)
		if matched != ok || (ok && step != current+offset) {
			t.Errorf("offset %d = %d, %v; want %v", offset, step, matched, ok)
		}
	}
	if _, ok := mfa.Verify(secret, "081 804", now); !ok {
		t.Error("a code typed with a space was refused")
	}
	if _, ok := mfa.Verify(secret, "081-804", now); !ok {
		t.Error("a code typed with a dash was refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := mfa.Verify(secret, bad, now); ok {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestURI verifies the address an authenticator app scans.
//
// Scenarios:
//   - the address names the issuer and the account, carries the base32 secret and the parameters, and escapes what needs it
func TestURI(t *testing.T) {
	got := mfa.URI("Transferly", "ana+x@example.com", []byte("12345678901234567890"))
	for _, part := range []string{"otpauth://totp/Transferly:ana+x@example.com?", "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "issuer=Transferly", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(got, part) {
			t.Errorf("URI() = %s; want it to contain %s", got, part)
		}
	}
}

// TestService_Enrolment verifies enrolling, checking and removing the authenticator app.
//
// Scenarios:
//   - nothing is enrolled at first; beginning gives a base32 secret and an address but does not enable the factor
//   - confirming with a right code enables it and gives ten recovery codes; a wrong code, an unstarted setup and a repeated confirmation are refused
//   - the status reports the factor and the unused recovery codes, disabling removes the app and keeps the recovery codes until they are cleared
//   - the stored secret is sealed: it is not the raw secret, and another user's copy cannot be opened
//   - failures of the store are returned
func TestService_Enrolment(t *testing.T) {
	ctx := context.Background()
	e := newEnv()
	if enabled, err := e.svc.Enabled(ctx, e.user); err != nil || enabled {
		t.Fatalf("Enabled() = %v, %v; want off", enabled, err)
	}
	if _, err := e.svc.ConfirmTOTP(ctx, e.user, "123456"); !errors.Is(err, mfa.ErrNoSetup) {
		t.Errorf("confirming before beginning = %v; want ErrNoSetup", err)
	}
	secret := e.begin(t)
	if bytes.Contains(e.store.factors[e.user].Sealed, secret) {
		t.Error("the secret is stored in clear text")
	}
	if enabled, _ := e.svc.Enabled(ctx, e.user); enabled {
		t.Error("beginning enabled the factor")
	}
	if _, err := e.svc.ConfirmTOTP(ctx, e.user, "000000"); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("a wrong code = %v; want ErrInvalidCode", err)
	}
	codes, err := e.svc.ConfirmTOTP(ctx, e.user, mfa.Code(secret, e.clock.Now()))
	if err != nil || len(codes) != mfa.RecoveryCodes || len(codes[0]) != 11 || codes[0][5] != '-' {
		t.Fatalf("ConfirmTOTP() = %v, %v; want ten codes of the form xxxxx-xxxxx", codes, err)
	}
	if _, err := e.svc.ConfirmTOTP(ctx, e.user, mfa.Code(secret, e.clock.Now())); !errors.Is(err, mfa.ErrNoSetup) {
		t.Errorf("confirming twice = %v; want ErrNoSetup", err)
	}
	if status, err := e.svc.Status(ctx, e.user); err != nil || !status.TOTP || status.RecoveryLeft != 10 {
		t.Errorf("Status() = %+v, %v", status, err)
	}
	if status, _ := e.svc.Status(ctx, uuid.New()); status.TOTP || status.RecoveryLeft != 0 {
		t.Error("a user without a factor reports one")
	}

	other := uuid.New()
	e.store.factors[other] = mfa.Factor{Sealed: e.store.factors[e.user].Sealed, Confirmed: true}
	if err := e.svc.VerifyCode(ctx, other, mfa.Code(secret, e.clock.Now())); err == nil {
		t.Error("a copy of the sealed secret opened for another user")
	}
	e.store.factors[other] = mfa.Factor{Sealed: []byte("x"), Confirmed: true}
	if err := e.svc.VerifyCode(ctx, other, "123456"); !errors.Is(err, mfa.ErrInvalidCode) {
		t.Errorf("a truncated secret = %v; want ErrInvalidCode", err)
	}
	e.store.factors[other] = mfa.Factor{Sealed: []byte("x")}
	if _, err := e.svc.ConfirmTOTP(ctx, other, "123456"); err == nil {
		t.Error("a corrupt secret was confirmed")
	}

	if err := e.svc.Disable(ctx, e.user); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := e.svc.Enabled(ctx, e.user); enabled || len(e.store.recovery[e.user]) != 10 {
		t.Error("disabling left the factor, or removed the recovery codes, which belong to the account")
	}
	if err := e.svc.ClearRecovery(ctx, e.user); err != nil || len(e.store.recovery[e.user]) != 0 {
		t.Errorf("ClearRecovery() = %v with %d codes left", err, len(e.store.recovery[e.user]))
	}

	for _, op := range []string{"save", "confirm", "factor", "left", "delete"} {
		failing := newEnv()
		failing.store.fail[op] = errBoom
		var got error
		switch op {
		case "save":
			_, _, got = failing.svc.BeginTOTP(ctx, failing.user, "a@example.com")
		case "confirm":
			failing.store.fail[op] = nil
			s := failing.begin(t)
			failing.store.fail[op] = errBoom
			_, got = failing.svc.ConfirmTOTP(ctx, failing.user, mfa.Code(s, failing.clock.Now()))
		case "factor":
			_, got = failing.svc.Enabled(ctx, failing.user)
			if _, err := failing.svc.Status(ctx, failing.user); !errors.Is(err, errBoom) {
				t.Errorf("status with a failing lookup = %v", err)
			}
			if _, err := failing.svc.ConfirmTOTP(ctx, failing.user, "1"); !errors.Is(err, errBoom) {
				t.Errorf("confirm with a failing lookup = %v", err)
			}
		case "left":
			failing.store.fail[op] = nil
			failing.enroll(t)
			failing.store.fail[op] = errBoom
			_, got = failing.svc.Status(ctx, failing.user)
		case "delete":
			got = failing.svc.Disable(ctx, failing.user)
		}
		if !errors.Is(got, errBoom) {
			t.Errorf("%s failure = %v", op, got)
		}
	}
}

// zeroThen serves zero bytes for a budget and then fails.
type zeroThen struct{ left int }

// Read serves zeros until the budget is spent.
func (z *zeroThen) Read(p []byte) (int, error) {
	if z.left <= 0 {
		return 0, errBoom
	}
	n := min(len(p), z.left)
	z.left -= n
	clear(p[:n])
	return n, nil
}
