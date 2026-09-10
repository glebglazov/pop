package tmux

// DefaultShell reads the shell tmux gives a new pane.
func (t *realTmux) DefaultShell() (string, error) {
	return t.run.output("show-options", "-gv", "default-shell")
}
