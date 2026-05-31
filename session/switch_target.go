package session

// SwitchTarget jumps to an existing tmux session or pane ID without creating
// anything. Uses switch-client when already inside tmux, attach-session with
// stdio wired when outside.
func SwitchTarget(target string) error {
	return SwitchTargetWith(DefaultDeps(), target)
}

// SwitchTargetWith is the injectable variant of SwitchTarget.
func SwitchTargetWith(d *Deps, target string) error {
	if d.InTmux() {
		return d.Tmux.SwitchClient(target)
	}
	return d.Tmux.AttachSession(target)
}
