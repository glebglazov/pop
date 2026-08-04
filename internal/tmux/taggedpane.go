package tmux

import (
	"fmt"
	"path/filepath"
	"strings"
)

// DrainWindow is the shared tmux window pop's routine-fire and drain panes live
// in: one pane per key (routine id or Task-set id), tagged with a @pop_* option
// and tiled alongside its siblings. The tagged-pane composites take the window
// as a parameter — a Map's session tiles its grilling panes in a window of its
// own — so this names the drain window rather than being the only window the
// primitive can reach.
const DrainWindow = "pop-work"

// PaneTag selects which @pop_* per-pane option a spawned pane is tagged with.
// The option key strings are internal to this module — no consumer names them.
type PaneTag int

const (
	// TagRoutine tags a pane with the routine id it fires (@pop_routine).
	TagRoutine PaneTag = iota
	// TagSet tags a pane with the Task-set id it drains (@pop_set).
	TagSet
	// TagVerify tags a pane with the Task-set id it verifies (@pop_verify).
	TagVerify
	// TagFold tags a pane with the Task-set id it folds (@pop_fold).
	TagFold
	// TagAssist tags a pane with the Task-set id its Assist session belongs to
	// (@pop_assist).
	TagAssist
	// TagTicket tags a pane with the Decision ticket id it grills (@pop_ticket).
	// A Map's window holds no ticket in its name, so the tag is the only thing
	// that says which ticket a pane belongs to.
	TagTicket
)

func (tg PaneTag) option() string {
	switch tg {
	case TagRoutine:
		return "@pop_routine"
	case TagSet:
		return "@pop_set"
	case TagVerify:
		return "@pop_verify"
	case TagFold:
		return "@pop_fold"
	case TagAssist:
		return "@pop_assist"
	case TagTicket:
		return "@pop_ticket"
	default:
		return ""
	}
}

// TagPane sets a pane's @pop_* tag to value.
func (t *realTmux) TagPane(paneID string, tag PaneTag, value string) error {
	_, err := t.run.output("set-option", "-p", "-t", paneID, tag.option(), value)
	return err
}

// PaneTagValue reads one pane's @pop_* tag, empty when the pane carries none. It
// answers the question a value lookup cannot: whether a pane is spoken for at all,
// which is what makes an unclaimed pane safe to adopt.
func (t *realTmux) PaneTagValue(paneID string, tag PaneTag) (string, error) {
	out, err := t.run.output("display-message", "-t", paneID, "-p", "#{"+tag.option()+"}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// FindTaggedPane returns the id of the pane in session's window tagged
// tag=value, or "" when none exists (an absent window included: a missing
// window is "no such pane", not an error, so a preview/lookup never creates the
// window as a side effect).
func (t *realTmux) FindTaggedPane(session, window string, tag PaneTag, value string) (string, error) {
	out, err := t.run.output("list-panes", "-t", windowTarget(session, window), "-F", "#{"+tag.option()+"}\t#{pane_id}")
	if err != nil {
		return "", nil
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == value && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1]), nil
		}
	}
	return "", nil
}

// EnsureTaggedPane is the one home for pop's tagged-pane spawn flow — drain,
// routine fire, and a Map's grilling panes alike. It ensures the session
// (created detached at dir when absent), finds or creates window, then reuses
// the pane already tagged tag=value or splits and tags a fresh one, sends
// command to it (Enter terminated), and re-tiles a freshly split window. It
// returns the pane id.
//
// The caller supplies the tag, the window, and the derived session/dir;
// focus-after-spawn and switch-client behaviour stay caller-side.
func EnsureTaggedPane(t Tmux, tag PaneTag, session, window, dir, value, command string) (string, error) {
	if err := Ensure(t, session, dir); err != nil {
		return "", err
	}

	exists, err := t.WindowExists(session, window)
	if err != nil {
		return "", err
	}
	var freshPane string
	if !exists {
		freshPane, err = t.NewWindow(session, window, dir)
		if err != nil {
			return "", err
		}
	}

	paneID, err := t.FindTaggedPane(session, window, tag, value)
	if err != nil {
		return "", err
	}
	if paneID != "" {
		if dir != "" {
			if err := ensurePaneDir(t, paneID, dir); err != nil {
				return "", err
			}
		}
		if err := t.SendKeys(paneID, command, "Enter"); err != nil {
			return "", fmt.Errorf("send command: %w", err)
		}
		return paneID, nil
	}

	if freshPane != "" {
		// The window was just created; reuse its initial pane so a fresh window
		// holds a single tagged pane instead of splitting a second.
		paneID = freshPane
	} else {
		paneID, err = t.SplitWindow(session, window, dir)
		if err != nil {
			return "", err
		}
		if err := t.RetileWindow(session, window); err != nil {
			return "", fmt.Errorf("retile %s: %w", window, err)
		}
	}

	if err := t.TagPane(paneID, tag, value); err != nil {
		return "", fmt.Errorf("tag pane: %w", err)
	}
	if err := t.SendKeys(paneID, command, "Enter"); err != nil {
		return "", fmt.Errorf("send command: %w", err)
	}
	return paneID, nil
}

// SpawnFreshPane ensures the session and shared drain window, then always
// creates a new untagged pane rooted at dir (reusing a freshly created
// window's initial pane, else splitting and retiling). An empty command leaves
// the pane's login shell alone; a non-empty command is send-keys'd with Enter.
// Unlike EnsureTaggedPane it never looks up or tags panes — every call yields a
// distinct pane (the Runtime shell path).
func SpawnFreshPane(t Tmux, session, dir, command string) (string, error) {
	if err := Ensure(t, session, dir); err != nil {
		return "", err
	}

	exists, err := t.WindowExists(session, DrainWindow)
	if err != nil {
		return "", err
	}
	var paneID string
	if !exists {
		paneID, err = t.NewWindow(session, DrainWindow, dir)
		if err != nil {
			return "", err
		}
	} else {
		paneID, err = t.SplitWindow(session, DrainWindow, dir)
		if err != nil {
			return "", err
		}
		if err := t.RetileWindow(session, DrainWindow); err != nil {
			return "", fmt.Errorf("retile drain window: %w", err)
		}
	}

	if strings.TrimSpace(command) != "" {
		if err := t.SendKeys(paneID, command, "Enter"); err != nil {
			return "", fmt.Errorf("send command: %w", err)
		}
	}
	return paneID, nil
}

// ensurePaneDir respawns a reused pane when its cwd differs from the checkout
// the caller asked for. An empty dir skips correction so callers that omit a
// directory keep their current behaviour.
func ensurePaneDir(t Tmux, paneID, dir string) error {
	current, err := t.PaneCurrentPath(paneID)
	if err != nil {
		return fmt.Errorf("read pane directory: %w", err)
	}
	if pathsSame(current, dir) {
		return nil
	}
	if err := t.RespawnPane(paneID, dir); err != nil {
		return fmt.Errorf("correct pane directory: %w", err)
	}
	return nil
}

func pathsSame(a, b string) bool {
	if a == b {
		return true
	}
	ca, errA := filepath.Abs(a)
	cb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	ra, errA := filepath.EvalSymlinks(ca)
	rb, errB := filepath.EvalSymlinks(cb)
	if errA == nil && errB == nil {
		return ra == rb
	}
	return ca == cb
}
