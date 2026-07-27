package tmux

import (
	"fmt"
	"strings"
)

// agentWindow is the name of the shared window pop's agentic panes live in.
// The "<session>:agent" target string is built only here.
const agentWindow = "agent"

// AgentPane is one named pane in the agent window: its title and its pane-id
// target (glossary: Agentic pane, Pane ID target).
type AgentPane struct {
	Title string
	ID    string
}

func agentTarget(session string) string { return session + ":" + agentWindow }

// HasAgentWindow reports whether session owns the shared agent window.
func (t *realTmux) HasAgentWindow(session string) bool {
	out, err := t.run.output("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return false
	}
	for _, w := range strings.Split(out, "\n") {
		if w == agentWindow {
			return true
		}
	}
	return false
}

// AgentPanes lists the named panes in session's agent window. A missing agent
// window surfaces as an error.
func (t *realTmux) AgentPanes(session string) ([]AgentPane, error) {
	out, err := t.run.output("list-panes", "-t", agentTarget(session), "-F", "#{pane_title}\t#{pane_id}")
	if err != nil {
		return nil, fmt.Errorf("no agent window in session %q", session)
	}
	var panes []AgentPane
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		panes = append(panes, AgentPane{Title: parts[0], ID: parts[1]})
	}
	return panes, nil
}

// FindAgentPane resolves a pane id by title in session's agent window.
func (t *realTmux) FindAgentPane(session, title string) (string, error) {
	panes, err := t.AgentPanes(session)
	if err != nil {
		return "", err
	}
	for _, p := range panes {
		if p.Title == title {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("pane %q not found in session %q", title, session)
}

// NewAgentWindow creates session's agent window rooted at dir (detached, so it
// does not steal focus) and returns the new pane's id.
func (t *realTmux) NewAgentWindow(session, dir string) (string, error) {
	out, err := t.run.output("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", agentWindow, "-c", dir)
	if err != nil {
		return "", fmt.Errorf("failed to create agent window: %w", err)
	}
	return out, nil
}

// SplitAgentPane splits a new pane into session's agent window rooted at dir
// and returns the new pane's id.
func (t *realTmux) SplitAgentPane(session, dir string) (string, error) {
	out, err := t.run.output("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", agentTarget(session), "-c", dir)
	if err != nil {
		return "", fmt.Errorf("failed to create pane: %w", err)
	}
	return out, nil
}

// RetileAgentWindow re-tiles session's agent window.
func (t *realTmux) RetileAgentWindow(session string) error {
	_, err := t.run.output("select-layout", "-t", agentTarget(session), "tiled")
	return err
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
