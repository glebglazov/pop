package deps

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Tmux defines operations for interacting with tmux
type Tmux interface {
	// Command runs tmux with the given arguments and returns the trimmed output.
	// This is the generic entry point — all tmux operations can go through here.
	Command(args ...string) (string, error)
	// HasSession checks if a session exists
	HasSession(name string) bool
	// NewSession creates a new detached session
	NewSession(name, dir string) error
	// SwitchClient switches to a session (when inside tmux)
	SwitchClient(name string) error
	// AttachSession attaches to a session (when outside tmux)
	AttachSession(name string) error
	// KillSession kills a session
	KillSession(name string) error
	// ListSessions returns session info in "name\tactivity" format per line.
	// Tab delimiter is used because session names may contain spaces.
	ListSessions() (string, error)
}

// RealTmux implements Tmux using actual tmux commands
type RealTmux struct{}

func NewRealTmux() *RealTmux {
	return &RealTmux{}
}

func (t *RealTmux) Command(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", tmuxError(err)
	}
	return strings.TrimSpace(string(out)), nil
}

// tmuxError enriches an exec error with tmux's stderr, which Cmd.Output()
// captures into ExitError.Stderr but the bare error string omits — leaving
// only an opaque "exit status 1". Surfacing stderr makes failures diagnosable.
func tmuxError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if msg := strings.TrimSpace(string(exit.Stderr)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
	}
	return err
}

func (t *RealTmux) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t="+name)
	return cmd.Run() == nil
}

func (t *RealTmux) NewSession(name, dir string) error {
	cmd := exec.Command("tmux", "new-session", "-ds", name, "-c", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func (t *RealTmux) SwitchClient(name string) error {
	cmd := exec.Command("tmux", "switch-client", "-t", name)
	return cmd.Run()
}

func (t *RealTmux) AttachSession(name string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (t *RealTmux) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	return cmd.Run()
}

func (t *RealTmux) ListSessions() (string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\t#{session_activity}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
