//go:build darwin

package tty

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Darwin's <signal.h> values; x/sys/unix exports neither these nor a
// pthread_sigmask wrapper for this platform.
const (
	sigBlock   = 1
	sigSetmask = 3
)

// withBlockedSignals runs fn on a thread whose signal mask has sigs added, then
// restores the mask that thread had on entry.
//
// The mask must be per-thread and it must be the mask of the thread issuing the
// tty syscall: XNU's tty layer asks whether the calling thread blocks
// SIGTTIN/SIGTTOU (uu_sigmask) before generating it, so blocking here suppresses
// the signal for the whole process group without touching the process-wide
// disposition. Hence the LockOSThread: the goroutine must not migrate between
// the mask change and the syscall.
func withBlockedSignals(fn func() error, sigs ...unix.Signal) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var block, saved uint32
	for _, sig := range sigs {
		block |= 1 << (uint32(sig) - 1)
	}
	if _, _, errno := unix.Syscall(unix.SYS_SIGPROCMASK, sigBlock, uintptr(unsafe.Pointer(&block)), uintptr(unsafe.Pointer(&saved))); errno != 0 {
		return fmt.Errorf("block %v: %w", sigs, errno)
	}
	defer unix.Syscall(unix.SYS_SIGPROCMASK, sigSetmask, uintptr(unsafe.Pointer(&saved)), 0)
	return fn()
}
