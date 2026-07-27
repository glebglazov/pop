package tmux

import (
	"fmt"
	"strings"
)

// PaneInfo is a pane's session name and current foreground command, read in a
// single tmux round-trip.
type PaneInfo struct {
	Session string
	Command string
}

// PaneInfo reads the session name and current foreground command of a pane.
func (t *realTmux) PaneInfo(paneID string) (PaneInfo, error) {
	out, err := t.run.output("display-message", "-t", paneID, "-p", "#{session_name}\t#{pane_current_command}")
	if err != nil {
		return PaneInfo{}, err
	}
	parts := strings.SplitN(out, "\t", 2)
	if len(parts) != 2 {
		return PaneInfo{}, fmt.Errorf("unexpected display-message output: %q", out)
	}
	return PaneInfo{Session: parts[0], Command: parts[1]}, nil
}

// PaneSession resolves just the session name owning a pane.
func (t *realTmux) PaneSession(paneID string) (string, error) {
	return t.run.output("display-message", "-t", paneID, "-p", "#{session_name}")
}

// IsActivePane reports whether the pane is visible to the user: active in its
// window, the window active in its session, and the session attached. Any
// lookup failure reports false.
func (t *realTmux) IsActivePane(paneID string) bool {
	out, err := t.run.output("display-message", "-t", paneID, "-p", "#{pane_active} #{window_active} #{session_attached}")
	if err != nil {
		return false
	}
	return out == "1 1 1"
}

// LivePanes lists the pane ids that exist across every session (a liveness
// poll). An error means liveness could not be determined — callers must not
// treat it as "no panes alive".
func (t *realTmux) LivePanes() ([]string, error) {
	out, err := t.run.output("list-panes", "-a", "-F", "#{pane_id}")
	if err != nil {
		return nil, err
	}
	var panes []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			panes = append(panes, line)
		}
	}
	return panes, nil
}
