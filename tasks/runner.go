package tasks

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	signalGracePeriod = 5 * time.Second
)

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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
	return proc.Wait()
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
