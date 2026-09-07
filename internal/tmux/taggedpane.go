package tmux

import (
	"fmt"
	"path/filepath"
	"strconv"
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
	// TagSlot tags a pane with the slot that addresses it among the panes one
	// container holds for a single activity (@pop_slot). It sits beside an
	// activity tag rather than inside it: the activity tag keeps holding the
	// bare container id, which is what every other reader of it wants
	// (ADR-0263).
	TagSlot
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
	case TagSlot:
		return "@pop_slot"
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

// PaneSlot addresses one of the panes a container may hold for a single
// activity — the digit an operator types to reach a particular one (ADR-0263).
// The digit is the slot rather than a position in a list, so a slot reaches the
// same pane for as long as that pane lives and closing one leaves its digit
// unlisted instead of renumbering its siblings.
type PaneSlot int

const (
	// FirstPaneSlot opens the range. It is also what a pane carrying an
	// activity tag and no slot option reads as: a pane spawned before slots
	// existed is its container's first pane, so a single-pane lookup still
	// finds what it always found.
	FirstPaneSlot PaneSlot = 1
	// LastPaneSlot closes the range. Nine is the whole of it — a tenth pane
	// would be a session no digit could address, so it is refused.
	LastPaneSlot PaneSlot = 9

	// anyPaneSlot is the single-pane lookup: the pane carrying the activity
	// tag, whichever slot it sits on. EnsureTaggedPane asks with it.
	anyPaneSlot PaneSlot = 0
)

// Valid reports whether s is a slot a pane can be addressed by.
func (s PaneSlot) Valid() bool { return s >= FirstPaneSlot && s <= LastPaneSlot }

func (s PaneSlot) String() string { return strconv.Itoa(int(s)) }

// ParsePaneSlot reads a @pop_slot option value. Anything the option does not
// hold as a digit in range reads as FirstPaneSlot, which is the pre-slot pane's
// case: it carries the container tag and no slot at all. Every Tmux
// implementation — the real one and the fakes — reads the option through here so
// they agree on that.
func ParsePaneSlot(raw string) PaneSlot {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return FirstPaneSlot
	}
	if slot := PaneSlot(n); slot.Valid() {
		return slot
	}
	return FirstPaneSlot
}

// TaggedPane is one of a container's panes for a single activity: the slot that
// addresses it and the foreground command that says whether the session in it
// is still going.
type TaggedPane struct {
	PaneID  string
	Slot    PaneSlot
	Command string
}

// Live reports whether the pane still runs the command it was spawned with,
// rather than having fallen back to its login shell — the same rule the
// live-pane affordance decides jump-vs-respawn on.
func (p TaggedPane) Live() bool { return !IsBareShell(p.Command) }

// ListTaggedPanes returns every pane in session's window tagged tag=value, with
// its slot and foreground command, in the window's own pane order. It is the
// plural of FindTaggedPane and answers the same way about an absent window: no
// panes and no error, so a lookup never creates the window as a side effect.
func (t *realTmux) ListTaggedPanes(session, window string, tag PaneTag, value string) ([]TaggedPane, error) {
	format := strings.Join([]string{
		"#{" + tag.option() + "}",
		"#{" + TagSlot.option() + "}",
		"#{pane_id}",
		"#{pane_current_command}",
	}, "\t")
	out, err := t.run.output("list-panes", "-t", windowTarget(session, window), "-F", format)
	if err != nil {
		return nil, nil
	}
	var panes []TaggedPane
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 4)
		if len(parts) != 4 {
			continue
		}
		paneID := strings.TrimSpace(parts[2])
		if parts[0] != value || paneID == "" {
			continue
		}
		panes = append(panes, TaggedPane{
			PaneID:  paneID,
			Slot:    ParsePaneSlot(parts[1]),
			Command: strings.TrimSpace(parts[3]),
		})
	}
	return panes, nil
}

// ErrNoFreePaneSlot is the refusal of a pane beyond the addressable range.
var ErrNoFreePaneSlot = fmt.Errorf("all %d pane slots are taken", LastPaneSlot)

// LowestFreePaneSlot returns the lowest slot no pane tagged tag=value holds —
// the slot a new pane takes. With the whole range held it refuses rather than
// naming a slot twice: a tenth pane would be a session no digit could address
// (ADR-0263).
func LowestFreePaneSlot(t Tmux, tag PaneTag, session, window, value string) (PaneSlot, error) {
	panes, err := t.ListTaggedPanes(session, window, tag, value)
	if err != nil {
		return anyPaneSlot, err
	}
	taken := map[PaneSlot]bool{}
	for _, p := range panes {
		taken[p.Slot] = true
	}
	for slot := FirstPaneSlot; slot <= LastPaneSlot; slot++ {
		if !taken[slot] {
			return slot, nil
		}
	}
	return anyPaneSlot, fmt.Errorf("%w for %s", ErrNoFreePaneSlot, value)
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
	return ensurePane(t, tag, anyPaneSlot, session, window, dir, value, command)
}

// EnsureSlottedPane is EnsureTaggedPane addressed by slot: it opens or reuses
// the one of tag=value's panes sitting on slot, so a container can hold several
// panes for one activity and a digit still reaches a particular one (ADR-0263).
// A pane it spawns is tagged with the container id like any other and stamped
// with the slot beside it.
func EnsureSlottedPane(t Tmux, tag PaneTag, slot PaneSlot, session, window, dir, value, command string) (string, error) {
	if !slot.Valid() {
		return "", fmt.Errorf("pane slot %d is outside %d-%d", slot, FirstPaneSlot, LastPaneSlot)
	}
	return ensurePane(t, tag, slot, session, window, dir, value, command)
}

// ensurePane is the shared spawn-or-reuse flow. slot selects which of
// tag=value's panes is reused and is stamped on a pane it spawns; anyPaneSlot
// takes whichever pane carries the tag and stamps nothing, which is the
// single-pane behaviour every pre-slot caller has.
func ensurePane(t Tmux, tag PaneTag, slot PaneSlot, session, window, dir, value, command string) (string, error) {
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

	paneID, err := findPaneOnSlot(t, tag, slot, session, window, value)
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
	if slot != anyPaneSlot {
		if err := t.TagPane(paneID, TagSlot, slot.String()); err != nil {
			return "", fmt.Errorf("tag pane slot: %w", err)
		}
	}
	if err := t.SendKeys(paneID, command, "Enter"); err != nil {
		return "", fmt.Errorf("send command: %w", err)
	}
	return paneID, nil
}

// findPaneOnSlot resolves the pane ensurePane may reuse, "" when there is none.
func findPaneOnSlot(t Tmux, tag PaneTag, slot PaneSlot, session, window, value string) (string, error) {
	if slot == anyPaneSlot {
		return t.FindTaggedPane(session, window, tag, value)
	}
	panes, err := t.ListTaggedPanes(session, window, tag, value)
	if err != nil {
		return "", err
	}
	for _, p := range panes {
		if p.Slot == slot {
			return p.PaneID, nil
		}
	}
	return "", nil
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
