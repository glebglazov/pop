//go:build !linux && !darwin

package tasks

import (
	"strings"
	"testing"
)

// TestProcStartUnsupportedOnThisPlatform pins the documented boundary on
// platforms that cannot read process start times: PID-reuse defense is
// unavailable and startup surfaces an explicit warning rather than degrading
// silently.
func TestProcStartUnsupportedOnThisPlatform(t *testing.T) {
	if ProcStartSupported() {
		t.Fatal("expected no proc-start support outside linux/darwin")
	}
	var b strings.Builder
	WarnProcStartUnsupported(&b)
	if !strings.Contains(b.String(), "PID reuse") {
		t.Fatalf("unsupported platform must warn about PID reuse, got %q", b.String())
	}
	// Nil writer is tolerated.
	WarnProcStartUnsupported(nil)
}
