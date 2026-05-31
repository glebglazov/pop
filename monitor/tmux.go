package monitor

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/internal/deps"
)

// TmuxPaneInfo returns the session name and the current foreground command
// running in the given pane in a single tmux round-trip.
func TmuxPaneInfo(tmux deps.Tmux, paneID string) (session, cmdName string, err error) {
	out, err := tmux.Command("display-message", "-t", paneID, "-p", "#{session_name}\t#{pane_current_command}")
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(out, "\t", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected display-message output: %q", out)
	}
	return parts[0], parts[1], nil
}

// IsActiveTmuxPane reports whether the pane is visible to the user: active in
// its window, the window is active in its session, and the session is attached.
func IsActiveTmuxPane(tmux deps.Tmux, paneID string) bool {
	out, err := tmux.Command("display-message", "-t", paneID, "-p", "#{pane_active} #{window_active} #{session_attached}")
	if err != nil {
		return false
	}
	return out == "1 1 1"
}
