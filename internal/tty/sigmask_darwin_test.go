//go:build darwin

package tty

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The whole reason this package masks signals per thread rather than changing
// the process-wide disposition is that a mask can be put back exactly. Pin that:
// the mask a caller had on entry is the mask it has on return.
func TestWithBlockedSignalsRestoresTheThreadMask(t *testing.T) {
	before := threadSigmask(t)

	var inside uint32
	if err := withBlockedSignals(func() error {
		inside = threadSigmask(t)
		return nil
	}, unix.SIGTTIN, unix.SIGTTOU); err != nil {
		t.Fatalf("withBlockedSignals: %v", err)
	}

	ttin := uint32(1) << (uint32(unix.SIGTTIN) - 1)
	ttou := uint32(1) << (uint32(unix.SIGTTOU) - 1)
	if inside&ttin == 0 || inside&ttou == 0 {
		t.Fatalf("mask inside = %#x, want SIGTTIN and SIGTTOU blocked", inside)
	}
	if got := threadSigmask(t); got != before {
		t.Fatalf("mask after = %#x, want the entry mask %#x", got, before)
	}
}

func threadSigmask(t *testing.T) uint32 {
	t.Helper()
	var empty, current uint32
	if _, _, errno := unix.Syscall(unix.SYS_SIGPROCMASK, sigBlock, uintptr(unsafe.Pointer(&empty)), uintptr(unsafe.Pointer(&current))); errno != 0 {
		t.Fatalf("read sigmask: %v", errno)
	}
	return current
}
