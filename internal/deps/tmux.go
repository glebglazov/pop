package deps

import (
	"os/exec"
	"strings"
)

// Tmux defines operations for interacting with tmux
type Tmux interface {
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
	// ListSessions returns session info in "name activity" format per line
	ListSessions() (string, error)
}

// RealTmux implements Tmux using actual tmux commands
type RealTmux struct{}

func NewRealTmux() *RealTmux {
	return &RealTmux{}
}

func (t *RealTmux) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t="+name)
	return cmd.Run() == nil
}

func (t *RealTmux) NewSession(name, dir string) error {
	cmd := exec.Command("tmux", "new-session", "-ds", name, "-c", dir)
	return cmd.Run()
}

func (t *RealTmux) SwitchClient(name string) error {
	cmd := exec.Command("tmux", "switch-client", "-t", name)
	return cmd.Run()
}

func (t *RealTmux) AttachSession(name string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", name)
	// Note: In real usage, stdin/stdout/stderr are connected
	// This basic implementation is for the interface contract
	return cmd.Run()
}

func (t *RealTmux) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	return cmd.Run()
}

func (t *RealTmux) ListSessions() (string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name} #{session_activity}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
