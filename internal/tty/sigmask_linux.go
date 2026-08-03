//go:build linux

package tty

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// withBlockedSignals runs fn on a thread whose signal mask has sigs added, then
// restores the mask that thread had on entry.
//
// The mask must be per-thread and it must be the mask of the thread issuing the
// tty syscall: Linux's __tty_check_change asks whether the calling thread blocks
// SIGTTIN/SIGTTOU before generating it, so blocking here suppresses the signal
// for the whole process group without touching the process-wide disposition.
// Hence the LockOSThread: the goroutine must not migrate between the mask change
// and the syscall.
func withBlockedSignals(fn func() error, sigs ...unix.Signal) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var block, saved unix.Sigset_t
	for _, sig := range sigs {
		sigaddset(&block, sig)
	}
	if err := unix.PthreadSigmask(unix.SIG_BLOCK, &block, &saved); err != nil {
		return fmt.Errorf("block %v: %w", sigs, err)
	}
	defer unix.PthreadSigmask(unix.SIG_SETMASK, &saved, nil)
	return fn()
}

func sigaddset(set *unix.Sigset_t, sig unix.Signal) {
	bits := uint(unsafe.Sizeof(set.Val[0])) * 8
	n := uint(sig) - 1
	set.Val[n/bits] |= 1 << (n % bits)
}
