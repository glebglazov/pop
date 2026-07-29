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

// PaneCurrentPath returns the pane's working directory.
func (t *realTmux) PaneCurrentPath(paneID string) (string, error) {
	return t.run.output("display-message", "-t", paneID, "-p", "#{pane_current_path}")
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

// PaneCommands maps every live pane id to its current foreground command
// across all sessions.
func (t *realTmux) PaneCommands() (map[string]string, error) {
	out, err := t.run.output("list-panes", "-a", "-F", "#{pane_id} #{pane_current_command}")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}

// CapturePreview captures a pane's visible content plus 50 lines of scrollback
// with escape sequences preserved (capture-pane -e), for coloured preview
// display. It differs from CapturePane, which strips ANSI codes.
func (t *realTmux) CapturePreview(paneID string) (string, error) {
	return t.run.output("capture-pane", "-p", "-e", "-S", "-50", "-t", paneID)
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

// SetPaneTitle sets a pane's title.
func (t *realTmux) SetPaneTitle(paneID, title string) error {
	_, err := t.run.output("select-pane", "-t", paneID, "-T", title)
	return err
}

// SetRemainOnExit toggles a pane's remain-on-exit option so its content stays
// readable after the command exits.
func (t *realTmux) SetRemainOnExit(paneID string, on bool) error {
	value := "off"
	if on {
		value = "on"
	}
	_, err := t.run.output("set-option", "-p", "-t", paneID, "remain-on-exit", value)
	return err
}

// SendKeys sends literal keys to a pane. Keys are not auto-terminated.
func (t *realTmux) SendKeys(paneID string, keys ...string) error {
	_, err := t.run.output(append([]string{"send-keys", "-t", paneID}, keys...)...)
	return err
}

// KillPane kills a pane.
func (t *realTmux) KillPane(paneID string) error {
	_, err := t.run.output("kill-pane", "-t", paneID)
	return err
}

// CapturePane captures a pane's visible content plus 50 lines of scrollback,
// stripped of ANSI codes (capture-pane -p).
func (t *realTmux) CapturePane(paneID string) (string, error) {
	return t.run.output("capture-pane", "-p", "-S", "-50", "-t", paneID)
}

// PaneDead reports whether a pane's process has exited. Any lookup failure
// reports false.
func (t *realTmux) PaneDead(paneID string) bool {
	out, err := t.run.output("display-message", "-t", paneID, "-p", "#{pane_dead}")
	if err != nil {
		return false
	}
	return out == "1"
}
