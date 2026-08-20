package ui

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/glebglazov/pop/internal/tty"
)

// resolveAppearanceHelperEnv marks the re-executed test binary as the child that
// does the resolving, and carries the terminal it resolves against.
const (
	resolveAppearanceHelperEnv = "POP_TEST_RESOLVE_APPEARANCE_TTY"
)

// A pop that does not hold the terminal foreground resolves plain, and neither
// stops its process group nor hangs doing it — the SIGTTOU that raw mode from a
// background group raises would otherwise stop pop and every command in its pane.
//
// The child runs in a process group of its own, so it is background for the
// terminal it is handed by definition. A stopped child never exits, so the
// deadline is the hang detector.
func TestResolveAppearanceFromABackgroundProcessGroup(t *testing.T) {
	if slave := os.Getenv(resolveAppearanceHelperEnv); slave != "" {
		resolveAppearanceInBackgroundGroup(t, slave)
		return
	}

	master, slave, err := tty.OpenPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	defer master.Close()
	// Drain the master, so a child that does write a query is never blocked by a
	// full terminal buffer.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := master.Read(buf); err != nil {
				return
			}
		}
	}()

	child := exec.Command(os.Args[0], "-test.run=TestResolveAppearanceFromABackgroundProcessGroup", "-test.v")
	child.Env = append(os.Environ(), resolveAppearanceHelperEnv+"="+slave)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output, err := runWithin(child, 30*time.Second)
	if err != nil {
		t.Fatalf("background resolve did not finish cleanly: %v\n%s", err, output)
	}
	if !strings.Contains(output, "PASS") {
		t.Fatalf("background resolve failed:\n%s", output)
	}
}

func resolveAppearanceInBackgroundGroup(t *testing.T, slave string) {
	f, err := os.OpenFile(slave, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", slave, err)
	}
	defer f.Close()

	if got := ResolveAppearance(f, f); got != AppearancePlain {
		t.Fatalf("ResolveAppearance from a background process group = %v, want plain", got)
	}

	// The guard is what makes the refusal safe rather than lucky: even the raw
	// mode the query would have entered fails here instead of stopping the group.
	_ = tty.GuardRead(func() error {
		state, err := term.MakeRaw(int(f.Fd()))
		if err == nil {
			return term.Restore(int(f.Fd()), state)
		}
		return err
	})
}

// runWithin runs cmd and kills it past the deadline, so a process the kernel
// stopped is reported as a failure rather than waited on forever.
func runWithin(cmd *exec.Cmd, limit time.Duration) (string, error) {
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.String(), err
	case <-time.After(limit):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return out.String(), os.ErrDeadlineExceeded
	}
}
