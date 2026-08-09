package tmux

import "strings"

// WindowPane is one pane in a named tmux window with its current foreground
// command. Wayfinder map windows are keyed by window name (the map id).
type WindowPane struct {
	Session    string
	WindowName string
	PaneID     string
	Command    string
}

// ListWindowPanes returns every live pane across all sessions with its window
// name and foreground command — one list-panes -a round-trip for wayfinder
// window liveness on the Work dashboard (ADR-0158). An absent server is an
// empty list, not an error (ADR-0199 decision 8).
func (t *realTmux) ListWindowPanes() ([]WindowPane, error) {
	format := strings.Join([]string{
		"#{session_name}",
		"#{window_name}",
		"#{pane_id}",
		"#{pane_current_command}",
	}, "\t")
	out, err := t.run.output("list-panes", "-a", "-F", format)
	if err != nil {
		if absentServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var panes []WindowPane
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		panes = append(panes, WindowPane{
			Session:    parts[0],
			WindowName: parts[1],
			PaneID:     parts[2],
			Command:    parts[3],
		})
	}
	return panes, nil
}
