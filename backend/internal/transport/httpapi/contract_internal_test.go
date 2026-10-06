package httpapi

import "testing"

// TestProtectedOperations verifies that the operations requiring a session are derived from the contract.
//
// Scenarios:
//   - the health, setup and login operations are public
//   - logout, the current user and preference updates are protected
func TestProtectedOperations(t *testing.T) {
	t.Parallel()
	spec, err := GetSpec()
	if err != nil {
		t.Fatalf("GetSpec() = %v", err)
	}

	protected := protectedOperations(spec)

	for _, public := range []string{"GetHealth", "GetReadiness", "GetSetupStatus", "Setup", "Login"} {
		if protected[public] {
			t.Errorf("%s is protected, want public", public)
		}
	}
	for _, name := range []string{"Logout", "GetMe", "UpdateMe"} {
		if !protected[name] {
			t.Errorf("%s is public, want protected", name)
		}
	}
}
