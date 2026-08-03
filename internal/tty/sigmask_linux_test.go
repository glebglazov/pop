//go:build linux

package tty

import (
	"testing"

	"golang.org/x/sys/unix"
)

// The whole reason this package masks signals per thread rather than changing
// the process-wide disposition is that a mask can be put back exactly. Pin that:
// the mask a caller had on entry is the mask it has on return.
func TestWithBlockedSignalsRestoresTheThreadMask(t *testing.T) {
	before := threadSigmask(t)

	var inside unix.Sigset_t
	if err := withBlockedSignals(func() error {
		inside = threadSigmask(t)
		return nil
	}, unix.SIGTTIN, unix.SIGTTOU); err != nil {
		t.Fatalf("withBlockedSignals: %v", err)
	}

	var want unix.Sigset_t
	sigaddset(&want, unix.SIGTTIN)
	sigaddset(&want, unix.SIGTTOU)
	for i := range want.Val {
		if inside.Val[i]&want.Val[i] != want.Val[i] {
			t.Fatalf("mask inside = %v, want SIGTTIN and SIGTTOU blocked", inside)
		}
	}
	if got := threadSigmask(t); got != before {
		t.Fatalf("mask after = %v, want the entry mask %v", got, before)
	}
}

func threadSigmask(t *testing.T) unix.Sigset_t {
	t.Helper()
	var current unix.Sigset_t
	if err := unix.PthreadSigmask(unix.SIG_BLOCK, nil, &current); err != nil {
		t.Fatalf("read sigmask: %v", err)
	}
	return current
}
