package session

import "fmt"

// Attach ensures a tmux session exists for name at path, then switches to or
// attaches to it depending on whether the caller is already inside tmux.
func Attach(name, path string) error {
	return AttachWith(DefaultDeps(), name, path)
}

// AttachWith is the injectable variant of Attach.
func AttachWith(d *Deps, name, path string) error {
	if !d.Tmux.HasSession(name) {
		if err := d.Tmux.NewSession(name, path); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	if d.InTmux() {
		return d.Tmux.SwitchClient(name)
	}
	return d.Tmux.AttachSession(name)
}
