package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/glebglazov/pop/internal/tty"
)

// signalGracePeriod is how long a SIGTERMed agent gets to exit before the
// process group is SIGKILLed. A variable so tests can shorten the escalation.
var signalGracePeriod = 5 * time.Second

// CommandRunner executes external commands.
type CommandRunner interface {
	Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (exitCode int, err error)
	Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error)
}

// AttendedCommandRunner executes commands attached to a caller-provided stdin.
type AttendedCommandRunner interface {
	RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (exitCode int, err error)
}

// EnvCommandRunner starts a command with extra KEY=VALUE entries layered over
// pop's own environment. It is separate from CommandRunner because only an
// Agent invocation that carries env (ADR-0164) needs it; a runner that never
// spawns one is unaffected, and one that is handed such an invocation without
// implementing this fails loudly rather than silently dropping the variable.
type EnvCommandRunner interface {
	StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error)
}

// ManagedProcess is a command running in its own process group.
type ManagedProcess struct {
	cmd  *exec.Cmd
	done chan waitResult
}

type waitResult struct {
	exitCode int
	err      error
}

// RealCommandRunner runs commands via os/exec.
type RealCommandRunner struct{}

func (RealCommandRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (RealCommandRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	if stdin == nil {
		stdin = os.Stdin
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// An attended agent usually launches an interactive TUI that reads the
	// controlling terminal. Such a child MUST run in the terminal's foreground
	// process group: a read from a background group draws SIGTTIN and the kernel
	// stops the child, which surfaces as a silent hang on a blank screen. This is
	// the opposite of the headless Run/Start paths, where Setpgid deliberately
	// isolates the agent in its own group so Pop can signal it as a unit. So we
	// only take over the foreground when stdin is a real terminal; otherwise we
	// exec plainly with no job control.
	ttyFd, isTTY := tty.TerminalFd(stdin)
	var savedPgrp int
	var savedPgrpErr error
	if isTTY {
		// Foreground:true makes the child its own process group and hands it the
		// terminal foreground via tcsetpgrp(Ctty). Ctty must be the resolved tty
		// fd in this process — not fd 0, which may be a different (non-tty) stream
		// when the caller redirected stdin.
		cmd.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: ttyFd}
		savedPgrp, savedPgrpErr = tty.ForegroundPgrp(ttyFd)
	}

	if err := cmd.Start(); err != nil {
		return 1, err
	}
	proc := &ManagedProcess{
		cmd:  cmd,
		done: make(chan waitResult, 1),
	}
	go func() {
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}
		proc.done <- waitResult{exitCode: exitCode, err: err}
	}()
	exitCode, waitErr := proc.Wait()

	// The child's group owned the terminal foreground and is now gone, leaving
	// Pop a background process: the next tty read (the gate re-prompt) would draw
	// SIGTTIN and the kernel would stop Pop in turn. Hand the foreground back to
	// the group that held it before the launch.
	//
	// This hand-back is a courtesy, not the guarantee the prompts rely on — a
	// descendant the agent left behind can take the foreground again a moment
	// later, which is why each prompt re-asserts ownership (see promptReader).
	// What matters here is that a hand-back which could not happen is said out
	// loud, since its consequence surfaces far from its cause.
	if isTTY {
		switch {
		case savedPgrpErr != nil:
			fmt.Fprintf(stderr, "Could not read the terminal foreground process group before launching %s (%v); the terminal was not handed back.\n", name, savedPgrpErr)
		case savedPgrp == 0:
			fmt.Fprintf(stderr, "The terminal reported no foreground process group before launching %s; the terminal was not handed back.\n", name)
		default:
			if err := tty.SetForeground(ttyFd, savedPgrp); err != nil {
				fmt.Fprintf(stderr, "Could not hand the terminal foreground back to process group %d: %v\n", savedPgrp, err)
			}
		}
	}
	return exitCode, waitErr
}

func (RealCommandRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return RealCommandRunner{}.StartWithEnv(ctx, dir, nil, stdout, stderr, name, args...)
}

func (RealCommandRunner) StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	if len(env) > 0 {
		// Appending after the inherited environment makes these entries win:
		// exec keeps the last value for a duplicated key.
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	proc := &ManagedProcess{
		cmd:  cmd,
		done: make(chan waitResult, 1),
	}
	go func() {
		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}
		proc.done <- waitResult{exitCode: exitCode, err: err}
	}()
	return proc, nil
}

func (p *ManagedProcess) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *ManagedProcess) PGID() int {
	pid := p.PID()
	if pid == 0 {
		return 0
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return pid
	}
	return pgid
}

func (p *ManagedProcess) SignalGroup(sig syscall.Signal) error {
	pgid := p.PGID()
	if pgid == 0 {
		return nil
	}
	return syscall.Kill(-pgid, sig)
}

func (p *ManagedProcess) Wait() (int, error) {
	if p == nil {
		return 1, nil
	}
	r := <-p.done
	if r.err != nil {
		if exitErr, ok := r.err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return r.exitCode, r.err
	}
	return r.exitCode, nil
}

func terminateProcessGroup(proc *ManagedProcess, sig syscall.Signal) {
	if proc == nil {
		return
	}
	_ = proc.SignalGroup(sig)
}
