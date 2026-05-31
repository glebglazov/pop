package session

import "fmt"

// Attach ensures a tmux session exists for name at path, then switches to or
// attaches to it depending on whether the caller is already inside tmux.
func Attach(name, path string) error {
	return AttachWith(DefaultDeps(), name, path)
}

// Ensure creates the tmux session for name at path if it does not already exist.
func Ensure(name, path string) error {
	return EnsureWith(DefaultDeps(), name, path)
}

// EnsureWith is the injectable variant of Ensure.
func EnsureWith(d *Deps, name, path string) error {
	if !d.Tmux.HasSession(name) {
		if err := d.Tmux.NewSession(name, path); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}
	return nil
}

// AttachWith is the injectable variant of Attach.
func AttachWith(d *Deps, name, path string) error {
	if err := EnsureWith(d, name, path); err != nil {
		return err
	}
	return SwitchTargetWith(d, name)
}
