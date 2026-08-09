package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// InTmux reports whether the caller is inside the configured tmux server
// (ADR-0199 decision 2). $TMUX is "<socket-path>,<pid>,<session>"; the
// predicate compares its first field to the configured socket as
// symlink-resolved paths. An unset socket keeps the pre-socket-key
// behaviour: any non-empty $TMUX counts as inside.
func (t *realTmux) InTmux() bool {
	return inConfiguredServer(os.Getenv("TMUX"), t.socket)
}

// inConfiguredServer is the pure half of InTmux: given $TMUX's value and the
// configured socket name, report whether the caller is inside that server.
func inConfiguredServer(tmuxEnv, socket string) bool {
	if tmuxEnv == "" {
		return false
	}
	if socket == "" {
		return true
	}
	envPath := tmuxEnvSocketPath(tmuxEnv)
	if envPath == "" {
		return false
	}
	return sameResolvedPath(envPath, configuredSocketPath(socket))
}

// tmuxEnvSocketPath returns the socket path field of a $TMUX value.
func tmuxEnvSocketPath(tmuxEnv string) string {
	path, _, _ := strings.Cut(tmuxEnv, ",")
	return path
}

// configuredSocketPath builds the filesystem path for a -L socket name using
// tmux's documented rule: $TMUX_TMPDIR or /tmp, then tmux-<uid>/<name>.
func configuredSocketPath(name string) string {
	base := os.Getenv("TMUX_TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	return filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()), name)
}

// sameResolvedPath reports whether a and b name the same path after symlink
// resolution. A naive string compare fails on macOS where constructing the
// configured path yields /tmp/... while $TMUX reports /private/tmp/... .
func sameResolvedPath(a, b string) bool {
	return resolvePath(a) == resolvePath(b)
}

// resolvePath returns path with as many leading symlinks evaluated as exist.
// The final socket file need not exist — peels missing trailing components
// so /tmp/tmux-UID/name still resolves through /tmp → /private/tmp on macOS.
func resolvePath(path string) string {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	var missing []string
	cur := path
	for {
		dir := filepath.Dir(cur)
		if dir == cur {
			break
		}
		missing = append([]string{filepath.Base(cur)}, missing...)
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...)
		}
		cur = dir
	}
	return path
}

// PaneIDFromEnv returns the id of the pane the caller is running in, empty when
// it is not running in one. It is a package function rather than a Tmux verb
// because tmux exports the answer in the environment ($TMUX_PANE) and asking the
// server instead would return the *active* pane, which is a different pane
// whenever a command runs in the background. Callers that identify themselves —
// a Work claim naming its owner — want this one.
func PaneIDFromEnv() string {
	return os.Getenv("TMUX_PANE")
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
