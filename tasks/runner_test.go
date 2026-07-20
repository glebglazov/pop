package tasks

import (
	"context"
	"os"
	"testing"
)

// TestRunAttendedNonTerminalStdinPlainExec checks that a non-tty stdin skips
// foreground process-group handover and execs plainly without error.
func TestRunAttendedNonTerminalStdinPlainExec(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	code, err := RealCommandRunner{}.RunAttended(context.Background(), ".", r, os.Stdout, os.Stderr, "true")
	if err != nil {
		t.Fatalf("RunAttended: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

// TestRunAttendedTTYNotStdinFd checks that foreground handover uses the tty fd
// wired to the child, not pop's fd 0, so a caller with redirected stdin can
// still spawn an attended child on a separately-opened terminal.
func TestRunAttendedTTYNotStdinFd(t *testing.T) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		t.Skip("no /dev/tty")
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skipf("cannot open /dev/tty: %v", err)
	}
	defer tty.Close()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	oldStdin := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = oldStdin }()

	if tty.Fd() == 0 {
		t.Skip("tty unexpectedly got fd 0")
	}

	code, err := RealCommandRunner{}.RunAttended(context.Background(), ".", tty, os.Stdout, os.Stderr, "true")
	if err != nil {
		t.Fatalf("RunAttended with tty not on fd 0: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}
