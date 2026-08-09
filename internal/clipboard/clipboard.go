// Package clipboard copies text to the system clipboard without depending on
// any TUI framework, so callers on both sides of ADR-0143's TUI boundary
// (queue's styled dashboards and work's plain data core, or tasks' HITL
// gates) can deliver text to a human without pulling bubbletea/lipgloss into
// their import graph.
package clipboard

import (
	"encoding/base64"
	"os"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

// defaultTmuxMod is the production tmux module handle used by Copy's
// inside-tmux path (ADR-0142).
var defaultTmuxMod tmuxmod.Tmux = tmuxmod.New(config.ConfiguredTmuxSocket())

// Copy copies text to the system clipboard.
// Prefers `tmux load-buffer` when inside tmux, falls back to OSC 52 otherwise.
func Copy(text string) error {
	return CopyWith(defaultTmuxMod, text)
}

// CopyWith is Copy with an injectable tmux module handle, so tests can assert
// against the tmuxtest fake instead of a real tmux server.
func CopyWith(mod tmuxmod.Tmux, text string) error {
	if os.Getenv("TMUX") != "" {
		if err := mod.LoadBuffer(text); err == nil {
			return nil
		}
		// Fall through to OSC 52 if tmux load-buffer failed.
	}
	return osc52Copy(text)
}

// osc52Copy writes an OSC 52 escape sequence to /dev/tty, which most modern
// terminal emulators honor to update the system clipboard.
func osc52Copy(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := "\x1b]52;c;" + encoded + "\x07"

	// Write to /dev/tty so the sequence reaches the terminal even if stderr/stdout
	// have been redirected.
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		// Fall back to stderr as a last resort.
		if _, werr := os.Stderr.WriteString(seq); werr != nil {
			return werr
		}
		return nil
	}
	defer tty.Close()
	if _, err := tty.WriteString(seq); err != nil {
		return err
	}
	return nil
}
