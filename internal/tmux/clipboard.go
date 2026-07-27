package tmux

// LoadBuffer writes text into tmux's paste buffer (load-buffer -w -), so a
// terminal's native paste subsequently yields it. Used by ui's clipboard-copy
// fallback (the error screen's "c" key) when running inside tmux.
func (t *realTmux) LoadBuffer(text string) error {
	return t.run.input(text, "load-buffer", "-w", "-")
}
