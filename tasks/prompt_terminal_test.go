package tasks

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/tty"
	"golang.org/x/sys/unix"
)

// The terminal facts these tests pin — foreground process groups, SIGTTIN,
// SIGTTOU — only exist on a real terminal, and only for a process that owns a
// session. So each test allocates a pty, launches this same test binary into a
// new session with that pty as its controlling terminal, and drives it through
// the master side. helperEnv selects which act the child performs.
const helperEnv = "POP_PROMPT_TERMINAL_HELPER"

// TestGatePromptReadsWithForegroundHeldByAnotherGroup drives a gate prompt in a
// terminal whose foreground belongs to a different process group — the state an
// attended agent's surviving descendant leaves behind — and asserts the read
// completes. Before prompts asserted ownership this read drew SIGTTIN and the
// kernel stopped the process mid-menu, which this test would see as a timeout.
func TestGatePromptReadsWithForegroundHeldByAnotherGroup(t *testing.T) {
	out := runTerminalHelper(t, "prompt", "2\n")
	if !strings.Contains(out, `GOT="2"`) {
		t.Fatalf("prompt did not read the answer; helper output:\n%s", out)
	}
	if !strings.Contains(out, "took it back to prompt") {
		t.Fatalf("prompt did not report taking the foreground back; helper output:\n%s", out)
	}
	if strings.Contains(out, "HELPER-FAIL") {
		t.Fatalf("helper refused to set up the case:\n%s", out)
	}
}

// TestAttendedChildRunsInTheTerminalForeground pins the other half of the
// handover: an attended agent must be the terminal's foreground group while it
// runs (a TTY-requiring agent like codex fails outright otherwise), and the
// foreground must be back with Pop's group once it exits.
func TestAttendedChildRunsInTheTerminalForeground(t *testing.T) {
	out := runTerminalHelper(t, "attended", "")
	if !strings.Contains(out, "CHILD-IS-FOREGROUND") {
		t.Fatalf("attended child did not get the terminal foreground; helper output:\n%s", out)
	}
	if !strings.Contains(out, "HANDED-BACK") {
		t.Fatalf("foreground was not handed back after the child exited; helper output:\n%s", out)
	}
}

// runTerminalHelper runs this test binary as a session leader on a fresh pty in
// the named helper mode, types answer into it, and returns everything the
// helper wrote before it exited.
func runTerminalHelper(t *testing.T, mode, answer string) string {
	t.Helper()
	master, slavePath, err := tty.OpenPTY()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	defer master.Close()
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", slavePath, err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestPromptTerminalHelper", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Setsid + Setctty make the helper a session leader owning this pty, which is
	// what gives it a foreground process group to lose in the first place.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	slave.Close()
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	lines := make(chan string, 256)
	go func() {
		scanner := bufio.NewScanner(master)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	var seen strings.Builder
	deadline := time.After(30 * time.Second)
	answered := answer == ""
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return seen.String()
			}
			seen.WriteString(line + "\n")
			if !answered && strings.Contains(line, "READY") {
				if _, err := master.WriteString(answer); err != nil {
					t.Fatalf("write answer: %v", err)
				}
				answered = true
			}
			if strings.Contains(line, "HELPER-DONE") {
				return seen.String()
			}
		case <-deadline:
			return seen.String() + "\n(timed out waiting for the helper)\n"
		}
	}
}

// TestPromptTerminalHelper is the child half of the two tests above: a no-op
// unless the parent selected a mode. It runs as a session leader whose stdin is
// the pty the parent drives.
func TestPromptTerminalHelper(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("helper process only; driven by the terminal foreground tests")
	}
	defer fmt.Println("HELPER-DONE")

	own, err := unix.Getpgid(0)
	if err != nil {
		fmt.Printf("HELPER-FAIL own process group: %v\n", err)
		return
	}

	switch mode {
	case "prompt":
		// A process group that outlives an attended agent and holds the terminal:
		// exactly the state that used to stop Pop at its next prompt.
		holder := exec.Command("/bin/sh", "-c", "sleep 30")
		holder.Stdin = os.Stdin
		holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: 0}
		if err := holder.Start(); err != nil {
			fmt.Printf("HELPER-FAIL start holder: %v\n", err)
			return
		}
		defer func() {
			_ = holder.Process.Kill()
			_, _ = holder.Process.Wait()
		}()
		holding, err := tty.ForegroundPgrp(0)
		if err != nil {
			fmt.Printf("HELPER-FAIL read foreground: %v\n", err)
			return
		}
		if holding == own {
			fmt.Printf("HELPER-FAIL foreground still ours (%d)\n", own)
			return
		}
		fmt.Printf("READY holder=%d own=%d\n", holding, own)

		answer, err := readPromptLine(newPromptReader(os.Stdin), os.Stdout, "0")
		fmt.Printf("GOT=%q err=%v\n", answer, err)

	case "attended":
		// The child compares its own process group against the terminal's foreground
		// group; the two agreeing is what a TTY-requiring agent depends on.
		// `ps` pads each column to its header width, so the two ids must be stripped
		// before they are compared as strings — " 893" and "893" are the same group.
		script := `p=$(ps -o pgid= -p $$ | tr -d ' '); t=$(ps -o tpgid= -p $$ | tr -d ' ')
if [ "$p" = "$t" ]; then echo CHILD-IS-FOREGROUND; else echo "HELPER-FAIL child pgid=$p tpgid=$t"; fi`
		if _, err := (RealCommandRunner{}).RunAttended(context.Background(), "", os.Stdin, os.Stdout, os.Stderr, "/bin/sh", "-c", script); err != nil {
			fmt.Printf("HELPER-FAIL attended run: %v\n", err)
			return
		}
		after, err := tty.ForegroundPgrp(0)
		if err != nil {
			fmt.Printf("HELPER-FAIL read foreground: %v\n", err)
			return
		}
		if after == own {
			fmt.Println("HANDED-BACK")
		} else {
			fmt.Printf("HELPER-FAIL foreground left with %d, own %d\n", after, own)
		}

	default:
		fmt.Printf("HELPER-FAIL unknown mode %q\n", mode)
	}
}
