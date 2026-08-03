//go:build darwin

package tty

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

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
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		master.Close()
		return nil, "", fmt.Errorf("grant pty: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		master.Close()
		return nil, "", fmt.Errorf("unlock pty: %w", err)
	}
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		master.Close()
		return nil, "", fmt.Errorf("read pty name: %w", errno)
	}
	slave := string(name[:bytes.IndexByte(name[:], 0)])
	return master, slave, nil
}
