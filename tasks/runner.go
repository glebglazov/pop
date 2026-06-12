package tasks

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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
	ttyFd, isTTY := terminalFd(stdin)
	var savedPgrp int
	if isTTY {
		// Foreground:true makes the child its own process group and hands it the
		// terminal foreground via tcsetpgrp(Ctty). Ctty:0 is the child's stdin,
		// which we wired to the tty above.
		cmd.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: 0}
		if pgrp, err := unix.IoctlGetInt(ttyFd, unix.TIOCGPGRP); err == nil {
			savedPgrp = pgrp
		}
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
	// Pop a background process: the next tty read/write (the gate re-prompt)
	// would draw SIGTTIN/SIGTTOU and stop Pop in turn. Reclaim the foreground for
	// Pop's saved group, ignoring SIGTTOU during the handover because tcsetpgrp
	// from a background group raises it.
	if isTTY && savedPgrp != 0 {
		signal.Ignore(syscall.SIGTTOU)
		_ = unix.IoctlSetPointerInt(ttyFd, unix.TIOCSPGRP, savedPgrp)
		signal.Reset(syscall.SIGTTOU)
	}
	return exitCode, waitErr
}

// terminalFd reports the file descriptor of r when r is a real terminal, so an
// attended child can be placed in that terminal's foreground process group.
func terminalFd(r io.Reader) (int, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return 0, false
	}
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return 0, false
	}
	return int(f.Fd()), true
}

func (RealCommandRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
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
