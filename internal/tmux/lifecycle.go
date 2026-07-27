package tmux

import (
	"fmt"
	"os"
)

// Session-lifecycle primitives. These mirror the tmux subcommands one-to-one;
// the switch-vs-attach and create-if-missing policies live in the composites
// below (Ensure, Attach, SwitchTarget).

func (t *realTmux) HasSession(name string) bool {
	// has-session exits non-zero when the session is absent; the runner maps
	// that into an error, which is all we need.
	_, err := t.run.output("has-session", "-t="+name)
	return err == nil
}

func (t *realTmux) NewSession(name, dir string) error {
	_, err := t.run.output("new-session", "-ds", name, "-c", dir)
	return err
}

func (t *realTmux) SwitchClient(target string) error {
	_, err := t.run.output("switch-client", "-t", target)
	return err
}

func (t *realTmux) AttachSession(target string) error {
	return t.run.attach("attach-session", "-t", target)
}

func (t *realTmux) KillSession(name string) error {
	_, err := t.run.output("kill-session", "-t", name)
	return err
}

func (t *realTmux) InTmux() bool {
	return os.Getenv("TMUX") != ""
}

// Ensure creates the session for name rooted at dir if it does not already
// exist. A no-op when the session is already live.
func Ensure(t Tmux, name, dir string) error {
	if t.HasSession(name) {
		return nil
	}
	if err := t.NewSession(name, dir); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}
	return nil
}

// SwitchTarget jumps to an existing session or pane id without creating
// anything: switch-client when already inside tmux, attach-session (stdio
// wired) when outside.
func SwitchTarget(t Tmux, target string) error {
	if t.InTmux() {
		return t.SwitchClient(target)
	}
	return t.AttachSession(target)
}

// Attach ensures the session for name at dir exists, then switches to or
// attaches to it depending on whether the caller is already inside tmux.
func Attach(t Tmux, name, dir string) error {
	if err := Ensure(t, name, dir); err != nil {
		return err
	}
	return SwitchTarget(t, name)
}

// FocusPane selects paneID and switches the attached client to it — the
// "jump to this pane" action shared by the dashboard/preview verbs.
func FocusPane(t Tmux, paneID string) error {
	if err := t.SelectPane(paneID); err != nil {
		return err
	}
	return t.SwitchClient(paneID)
}
