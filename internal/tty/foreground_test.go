package tty

import (
	"os"
	"testing"
)

// A caller may hand this package any reader's fd; one with no foreground to own
// must come back as a named refusal rather than a claim silently believed.
func TestClaimForegroundRefusesANonTerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer f.Close()

	claim := ClaimForeground(int(f.Fd()))
	if claim.Owned {
		t.Fatalf("claim owned %s: %+v", os.DevNull, claim)
	}
	if claim.Err == nil {
		t.Fatal("claim failed without saying why")
	}
}

func TestGuardReadRunsTheRead(t *testing.T) {
	ran := false
	if err := GuardRead(func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("guard: %v", err)
	}
	if !ran {
		t.Fatal("guarded read never ran")
	}
}
