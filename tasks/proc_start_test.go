//go:build linux || darwin

package tasks

import (
	"os"
	"testing"
)

// TestDefaultProcStartTokenReadsLivePID checks the platform default reads a
// stable, non-empty token for a known-live process (this test binary) and
// reports failure for an impossible PID.
func TestDefaultProcStartTokenReadsLivePID(t *testing.T) {
	tok, ok := defaultProcStartToken(os.Getpid())
	if !ok || tok == "" {
		t.Fatalf("token for self = (%q, %v), want a non-empty token", tok, ok)
	}
	// Stable across calls for the same live process.
	if tok2, ok2 := defaultProcStartToken(os.Getpid()); !ok2 || tok2 != tok {
		t.Fatalf("token not stable: %q then %q", tok, tok2)
	}
	if _, ok := defaultProcStartToken(-1); ok {
		t.Fatalf("negative PID reported a token")
	}
}
