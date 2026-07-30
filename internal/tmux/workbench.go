package tmux

import (
	"fmt"
	"strings"
)

// Workbench-layout tmux verbs (ADR-0075, ADR-0142). The layout merge/realize
// engine lives in cmd — it is Workbench domain knowledge; only its tmux touches
// live here. pop's workbench window identity (@pop_wb_window) and pane identity
// (@pop_pane) are pop-owned tmux options constructed only in this module: no
// consumer names them, identity never lives in the clobberable window_name or
// pane title (ADR-0075).
const (
	optWBWindow = "@pop_wb_window"
	optWBPane   = "@pop_pane"
)

// NewScaffoldSession creates a brand-new detached session named name rooted at
// dir and returns the id of the stray initial window tmux always births, so the
// caller can kill it once the real layout is realized.
func (t *realTmux) NewScaffoldSession(name, dir string) (string, error) {
	out, err := t.run.output("new-session", "-d", "-s", name, "-c", dir, "-P", "-F", "#{window_id}")
	if err != nil {
		return "", fmt.Errorf("failed to create session %q: %w", name, err)
	}
	return strings.TrimSpace(out), nil
}

// LiveWorkbenchWindows maps each pop-stamped window's @pop_wb_window identity to
// its tmux window id within session. Windows lacking the stamp (anything not
// born of a Workbench apply) are skipped.
func (t *realTmux) LiveWorkbenchWindows(session string) (map[string]string, error) {
	out, err := t.run.output("list-windows", "-t", session, "-F", "#{"+optWBWindow+"}\t#{window_id}")
	if err != nil {
		return nil, err
	}
	windows := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		if _, ok := windows[parts[0]]; !ok {
			windows[parts[0]] = parts[1]
		}
	}
	return windows, nil
}

// LivePaneIdentities maps the @pop_pane identity of each stamped pane in
// windowRef to its tmux pane id, and returns the window's first pane id as a
// fallback anchor for the rare matched-window-with-no-recognizable-panes case.
func (t *realTmux) LivePaneIdentities(windowRef string) (map[string]string, string, error) {
	out, err := t.run.output("list-panes", "-t", windowRef, "-F", "#{"+optWBPane+"}\t#{pane_id}")
	if err != nil {
		return nil, "", err
	}
	names := make(map[string]string)
	fallback := ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if fallback == "" {
			fallback = parts[1]
		}
		if parts[0] != "" {
			if _, ok := names[parts[0]]; !ok {
				names[parts[0]] = parts[1]
			}
		}
	}
	return names, fallback, nil
}

// StampWorkbenchWindow records identity as the window's @pop_wb_window option so
// it survives auto-rename (ADR-0075). windowTarget is any tmux window target (a
// window id for a merged window, or session:name for a fresh one).
func (t *realTmux) StampWorkbenchWindow(windowTarget, identity string) error {
	_, err := t.run.output("set-option", "-w", "-t", windowTarget, optWBWindow, identity)
	return err
}

// DisableAutomaticRename turns off automatic-rename for windowTarget so its
// display name stays stable for humans.
func (t *realTmux) DisableAutomaticRename(windowTarget string) error {
	_, err := t.run.output("set-option", "-w", "-t", windowTarget, "automatic-rename", "off")
	return err
}

// StampPane records identity as the pane's @pop_pane option so a later reapply
// can match this pane regardless of how its display title gets clobbered.
func (t *realTmux) StampPane(paneID, identity string) error {
	_, err := t.run.output("set-option", "-p", "-t", paneID, optWBPane, identity)
	return err
}

// PaneSize reads the width and height (in cells) of the named pane. The query
// must target that pane (-t), never be left untargeted: an untargeted read
// returns the current client's window size, which for a detached session (born
// 80x24 from new-session -d) differs from the window the panes actually occupy
// and would skew the resize math.
func (t *realTmux) PaneSize(target string) (int, int, error) {
	out, err := t.run.output("display-message", "-t", target, "-p", "#{pane_width}\t#{pane_height}")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.SplitN(strings.TrimSpace(out), "\t", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected pane-size output: %q", out)
	}
	var w, h int
	fmt.Sscanf(parts[0], "%d", &w)
	fmt.Sscanf(parts[1], "%d", &h)
	return w, h, nil
}

// ResizePane resizes a pane along one axis to an exact cell size: width (-x)
// when horizontal is true (a columns container), height (-y) otherwise.
func (t *realTmux) ResizePane(paneID string, horizontal bool, size int) error {
	flag := "-y"
	if horizontal {
		flag = "-x"
	}
	_, err := t.run.output("resize-pane", "-t", paneID, flag, fmt.Sprintf("%d", size))
	return err
}

// RespawnPane restarts a pane's shell with a new working directory (-k kills the
// running command first), used when a reused container pane needs a child's cwd.
func (t *realTmux) RespawnPane(paneID, dir string) error {
	_, err := t.run.output("respawn-pane", "-c", dir, "-t", paneID, "-k")
	return err
}

// SplitSpec describes one layout split for SplitPane: which pane to split from
// (Target), the axis (Horizontal → -h side-by-side columns, else -v stacked
// rows), whether to place the new pane before the target (Before → -b), an
// optional exact size in cells (Cells → -l when > 0), and the new pane's
// working directory (Dir).
//
// -l sizes the new pane exactly and charges the border cell to the surviving
// pane on both axes (verified: -l 8 on a 24-row pane → 8/15; -l 20 on an
// 80-col pane → 20/59).
type SplitSpec struct {
	Target     string
	Horizontal bool
	Before     bool
	Cells      int
	Dir        string
}

// SplitPane splits a new pane off spec.Target per spec and returns its id.
func (t *realTmux) SplitPane(spec SplitSpec) (string, error) {
	args := []string{"split-window"}
	if spec.Horizontal {
		args = append(args, "-h")
	} else {
		args = append(args, "-v")
	}
	if spec.Before {
		args = append(args, "-b")
	}
	args = append(args, "-t", spec.Target)
	if spec.Cells > 0 {
		args = append(args, "-l", fmt.Sprintf("%d", spec.Cells))
	}
	args = append(args, "-P", "-F", "#{pane_id}", "-c", spec.Dir)
	out, err := t.run.output(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// KillWindow kills the window at target.
func (t *realTmux) KillWindow(target string) error {
	_, err := t.run.output("kill-window", "-t", target)
	return err
}

// SelectWindowTarget makes an arbitrary window target the current window (a
// window id for a merged window, or session:name for a fresh one). It differs
// from SelectWindow, which builds the target from a session and window name.
func (t *realTmux) SelectWindowTarget(target string) error {
	_, err := t.run.output("select-window", "-t", target)
	return err
}
