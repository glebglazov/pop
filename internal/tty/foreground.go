// Package tty owns the terminal job-control facts Pop depends on when it hands
// the terminal to an attended agent and then takes it back to prompt a human.
//
// One invariant explains everything here: the process group that reads a
// terminal must be that terminal's foreground group. A read from a background
// group draws SIGTTIN and the kernel stops the reader, so the human sees a
// fully rendered menu whose prompt never accepts input — a hang, not an error,
// because writes from a background group are not restricted the same way.
package tty

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ForegroundPgrp reports the process group that currently owns fd's terminal
// foreground.
func ForegroundPgrp(fd int) (int, error) {
	return unix.IoctlGetInt(fd, unix.TIOCGPGRP)
}

// SetForeground hands fd's terminal foreground to pgrp.
//
// tcsetpgrp issued from a background process group raises SIGTTOU, whose
// default action stops the whole group — the caller would be stopped by its own
// attempt to become runnable. SIGTTOU is therefore blocked on the calling
// thread for the duration: the tty layer consults that mask and completes the
// handover without generating the signal at all, and the thread's saved mask is
// put back afterwards. Blocking is used rather than ignoring the signal
// process-wide because a mask can be read back and restored exactly, while a
// disposition cannot — os/signal offers no way to learn what Pop inherited.
func SetForeground(fd, pgrp int) error {
	return withBlockedSignals(func() error {
		return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgrp)
	}, unix.SIGTTOU)
}

// Foreground records what ClaimForeground found and what it did about it.
type Foreground struct {
	// Owned reports whether this process's group owns the terminal foreground
	// now — the only field a caller must consult before reading.
	Owned bool
	// Holder is the process group that owned the foreground on entry, 0 when it
	// could not be read.
	Holder int
	// Taken reports that the foreground belonged to Holder and was taken.
	Taken bool
	// Err says why the claim failed; nil whenever Owned is true.
	Err error
}

// ClaimForeground makes this process's group the owner of fd's foreground,
// reporting whether it already was, had to take it, or could not.
//
// Callers prompt humans on terminals they do not exclusively control: an
// attended agent, or any descendant of one that outlived it, may have moved the
// foreground elsewhere. Asserting ownership at each prompt is cheap and turns a
// silent stop into either a working prompt or a named failure.
func ClaimForeground(fd int) Foreground {
	pgrp, err := unix.Getpgid(0)
	if err != nil {
		return Foreground{Err: fmt.Errorf("read own process group: %w", err)}
	}
	holder, err := ForegroundPgrp(fd)
	if err != nil {
		return Foreground{Err: fmt.Errorf("read terminal foreground process group: %w", err)}
	}
	if holder == pgrp {
		return Foreground{Owned: true, Holder: holder}
	}
	if err := SetForeground(fd, pgrp); err != nil {
		return Foreground{
			Holder: holder,
			Err:    fmt.Errorf("hand the terminal foreground from process group %d to %d: %w", holder, pgrp, err),
		}
	}
	return Foreground{Owned: true, Taken: true, Holder: holder}
}

// GuardRead runs fn — a read of the controlling terminal — with SIGTTIN and
// SIGTTOU blocked on the thread issuing it, restoring that thread's mask
// afterwards.
//
// This is the backstop behind ClaimForeground: if the claim did not hold after
// all, the kernel fails the blocked-signal read with EIO instead of stopping
// the process, so the caller can say what went wrong rather than freeze.
func GuardRead(fn func() error) error {
	return withBlockedSignals(fn, unix.SIGTTIN, unix.SIGTTOU)
}
