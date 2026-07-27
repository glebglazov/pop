package tmux

import (
	"fmt"
	"strings"
)

// Generic window/pane primitives shared by the spawn composites (EnsureWindow,
// EnsureTaggedPane). A "session:window" target string is constructed only in
// this module; consumers name a session and a window and never assemble the
// target themselves.

func windowTarget(session, name string) string { return session + ":" + name }

// WindowExists reports whether session owns a window named name.
func (t *realTmux) WindowExists(session, name string) (bool, error) {
	out, err := t.run.output("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return false, fmt.Errorf("list windows in %q: %w", session, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if line == name {
			return true, nil
		}
	}
	return false, nil
}

// NewWindow creates a detached window named name in session rooted at dir (so
// it does not steal focus) and returns its initial pane's id.
//
// No -a: the window is targeted by name, so its index is irrelevant, and -a
// (insert after current) collides with an already-occupied next index in a
// live interactive session ("index N in use"). Let tmux append at the first
// free index instead.
func (t *realTmux) NewWindow(session, name, dir string) (string, error) {
	out, err := t.run.output("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", name, "-c", dir)
	if err != nil {
		return "", fmt.Errorf("create window %q in %q: %w", name, session, err)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", fmt.Errorf("create window %q in %q: tmux returned no pane id", name, session)
	}
	return id, nil
}

// SplitWindow splits a new pane into session's window name rooted at dir and
// returns the new pane's id.
func (t *realTmux) SplitWindow(session, name, dir string) (string, error) {
	out, err := t.run.output("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", windowTarget(session, name), "-c", dir)
	if err != nil {
		return "", fmt.Errorf("split pane in %q: %w", name, err)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", fmt.Errorf("split pane in %q: tmux returned no pane id", name)
	}
	return id, nil
}

// RetileWindow re-tiles session's window name (select-layout tiled).
func (t *realTmux) RetileWindow(session, name string) error {
	_, err := t.run.output("select-layout", "-t", windowTarget(session, name), "tiled")
	return err
}

// WindowPanes lists the pane ids in session's window name, in order.
func (t *realTmux) WindowPanes(session, name string) ([]string, error) {
	out, err := t.run.output("list-panes", "-t", windowTarget(session, name), "-F", "#{pane_id}")
	if err != nil {
		return nil, fmt.Errorf("list panes in %q: %w", name, err)
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// SelectPane makes paneID the active pane in its window.
func (t *realTmux) SelectPane(paneID string) error {
	_, err := t.run.output("select-pane", "-t", paneID)
	return err
}

// SelectWindow makes session's window name the current window.
func (t *realTmux) SelectWindow(session, name string) error {
	_, err := t.run.output("select-window", "-t", windowTarget(session, name))
	return err
}

// EnsureWindow ensures session (created detached at dir when absent) owns a
// window named name, returning that window's target pane id and whether the
// window was freshly created. Callers that run one command per named window
// (refine, wayfinder) send to the returned pane on creation; a false created
// flag means the window pre-existed and the caller decides whether to reuse or
// focus its pane.
func EnsureWindow(t Tmux, session, name, dir string) (paneID string, created bool, err error) {
	if err := Ensure(t, session, dir); err != nil {
		return "", false, err
	}
	exists, err := t.WindowExists(session, name)
	if err != nil {
		return "", false, err
	}
	if exists {
		panes, err := t.WindowPanes(session, name)
		if err != nil {
			return "", false, err
		}
		if len(panes) == 0 {
			return "", false, fmt.Errorf("window %s:%s has no pane", session, name)
		}
		return panes[0], false, nil
	}
	paneID, err = t.NewWindow(session, name, dir)
	if err != nil {
		return "", false, err
	}
	return paneID, true, nil
}
