//go:build linux || darwin

package tasks

import (
	"strings"
	"testing"
)

// TestProcStartSupportedOnThisPlatform pins the documented boundary on the
// platforms that can read process start times: PID-reuse defense is available
// and no startup warning is emitted.
func TestProcStartSupportedOnThisPlatform(t *testing.T) {
	if !ProcStartSupported() {
		t.Fatal("expected proc-start support on linux/darwin")
	}
	var b strings.Builder
	WarnProcStartUnsupported(&b)
	if b.Len() != 0 {
		t.Fatalf("supported platform must emit no warning, got %q", b.String())
	}
}
