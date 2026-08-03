//go:build linux

package tty

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// OpenPTY allocates a pseudo-terminal pair, returning the master and the path of
// the slave device.
//
// Job control is only observable on a real terminal: foreground process groups,
// SIGTTIN and SIGTTOU do not exist for a pipe. Tests that need to prove Pop's
// prompts survive losing the foreground therefore need a terminal of their own,
// which is what this hands them.
func OpenPTY() (*os.File, string, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, "", fmt.Errorf("unlock pty: %w", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, "", fmt.Errorf("read pty number: %w", err)
	}
	return master, fmt.Sprintf("/dev/pts/%d", n), nil
}
