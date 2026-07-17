package tasks

import (
	"errors"
	"testing"
)

// TestQuotaPausedExit pins the supervisor-facing exit-code contract (ADR-0100):
// a run that parked on an agent quota pause maps to the dedicated
// ExitQuotaPaused code, while any other completion maps to no error (clean
// exit). This is the deterministic replacement for the removed end-to-end cmd
// test, which hung in WaitForRecovery on real wall-clock quota resets.
func TestQuotaPausedExit(t *testing.T) {
	err := QuotaPausedExit(true)
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("QuotaPausedExit(true) = %v, want *ExitError", err)
	}
	if ee.Code != ExitQuotaPaused {
		t.Fatalf("QuotaPausedExit(true) code = %d, want ExitQuotaPaused (%d)", ee.Code, ExitQuotaPaused)
	}

	if err := QuotaPausedExit(false); err != nil {
		t.Fatalf("QuotaPausedExit(false) = %v, want nil", err)
	}
}
