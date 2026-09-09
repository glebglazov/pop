package dashboard

import (
	"github.com/glebglazov/pop/tasks/drain"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work/ref"
)

// livePaneState is the three-state live-pane affordance for one supervised
// activity on a set (ADR-0158): none (dark / spawn), idle (grey / respawn),
// running (green / jump).
type livePaneState int

const (
	livePaneNone livePaneState = iota
	livePaneIdle
	livePaneRunning
)

// livePaneCache holds per-poll activity liveness keyed by pane tag then set id,
// the assist panes each container holds keyed by slot, plus Map liveness keyed by
// map id (its session). It is rebuilt from tmux list queries per dashboard poll —
// never from the DrainPane store.
type livePaneCache struct {
	byTag map[tmuxmod.PaneTag]map[string]livePaneState
	// assist keeps every assist pane a container holds rather than one state for
	// it, because the Assist pane menu puts each of them on its own digit
	// (ADR-0263). A Map's assist panes are in here beside a Task set's: they
	// carry the same tag keyed by the container id, and the menu is one menu.
	// byTag still carries the container's single answer, which is what a set
	// row's fixed-width activity cluster shows — a map row shows its session.
	assist    map[string]map[tmuxmod.PaneSlot]livePaneState
	wayfinder map[string]livePaneState
}

// assistPane is one of a container's assist panes as the last poll saw it: the
// slot that addresses it, and whether the session in it is still running.
type assistPane struct {
	slot  tmuxmod.PaneSlot
	state livePaneState
}

func (c livePaneCache) state(tag tmuxmod.PaneTag, setID string) livePaneState {
	if setID == "" {
		return livePaneNone
	}
	bySet := c.byTag[tag]
	if bySet == nil {
		return livePaneNone
	}
	return bySet[setID]
}

func (c livePaneCache) wayfinderState(mapID string) livePaneState {
	if mapID == "" || c.wayfinder == nil {
		return livePaneNone
	}
	return c.wayfinder[mapID]
}

// set records one pane's state under (tag, set id). A container may hold several
// panes for one activity, so the strongest state wins: the key is green while any
// of the set's assist panes is still running and only goes grey once every one of
// them has fallen back to its shell (ADR-0263).
func (c *livePaneCache) set(tag tmuxmod.PaneTag, setID string, state livePaneState) {
	if c == nil || setID == "" || state == livePaneNone {
		return
	}
	if c.byTag == nil {
		c.byTag = map[tmuxmod.PaneTag]map[string]livePaneState{}
	}
	if c.byTag[tag] == nil {
		c.byTag[tag] = map[string]livePaneState{}
	}
	if state > c.byTag[tag][setID] {
		c.byTag[tag][setID] = state
	}
}

// setAssistPane records the assist pane on one slot of a set, under the same
// strongest-wins rule set follows: two panes reading as the same slot — a
// pre-slot pane beside a stamped first — leave one live digit, not a coin flip.
func (c *livePaneCache) setAssistPane(setID string, slot tmuxmod.PaneSlot, state livePaneState) {
	if c == nil || setID == "" || state == livePaneNone || !slot.Valid() {
		return
	}
	if c.assist == nil {
		c.assist = map[string]map[tmuxmod.PaneSlot]livePaneState{}
	}
	if c.assist[setID] == nil {
		c.assist[setID] = map[tmuxmod.PaneSlot]livePaneState{}
	}
	if state > c.assist[setID][slot] {
		c.assist[setID][slot] = state
	}
}

// assistPanes lists the assist panes a container is holding, in slot order — the
// roster the Assist pane menu puts on digits. A slot no pane sits on is absent
// rather than dark: a digit that reaches nothing is not offered.
func (c livePaneCache) assistPanes(setID string) []assistPane {
	slots := c.assist[setID]
	panes := make([]assistPane, 0, len(slots))
	for slot, state := range slots {
		panes = append(panes, assistPane{slot: slot, state: state})
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].slot < panes[j].slot })
	return panes
}

// setWayfinder records one of a Map session's panes, under the same
// strongest-wins rule set follows. A Map's liveness is its session's, not one
// pane's: grilling panes come and go with the tickets, while the Map is running
// as long as anything in `pop-map-<id>` still holds a process.
func (c *livePaneCache) setWayfinder(mapID string, state livePaneState) {
	if c == nil || mapID == "" || state == livePaneNone {
		return
	}
	if c.wayfinder == nil {
		c.wayfinder = map[string]livePaneState{}
	}
	if state > c.wayfinder[mapID] {
		c.wayfinder[mapID] = state
	}
}

func stateFromCommand(cmd string) livePaneState {
	if tmuxmod.IsBareShell(cmd) {
		return livePaneIdle
	}
	return livePaneRunning
}

// loadLivePaneCache queries tmux once and maps each tagged activity pane to its
// live-pane state, plus each named window's primary pane for wayfinder map rows.
// A nil Tmux or a query failure yields an empty cache (all keys render dark)
// rather than blocking the dashboard poll.
func loadLivePaneCache(d *drain.Deps) livePaneCache {
	cache := livePaneCache{}
	if d == nil {
		return cache
	}
	tmux := d.Tmux
	if tmux == nil {
		return cache
	}
	panes, err := tmux.ListActivityPanes()
	if err == nil {
		for _, p := range panes {
			state := stateFromCommand(p.Command)
			cache.set(tmuxmod.TagSet, p.Set, state)
			cache.set(tmuxmod.TagVerify, p.Verify, state)
			cache.set(tmuxmod.TagFold, p.Fold, state)
			cache.set(tmuxmod.TagAssist, p.Assist, state)
			cache.setAssistPane(p.Assist, p.Slot, state)
		}
	}
	windows, err := tmux.ListWindowPanes()
	if err == nil {
		for _, w := range windows {
			mapID := wayfinder.MapIDFromSession(w.Session)
			if mapID == "" {
				continue
			}
			cache.setWayfinder(mapID, stateFromCommand(w.Command))
		}
	}
	return cache
}

// livePane styles for handoff-verb keys in the Run menu.
func livePaneIdleStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(ui.ColorClear()) }

var livePaneRunningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

// styleHandoffKey colours a handoff verb's shortcut by its live-pane state.
// Shell is always dark (livePaneNone): it is not an activity pop supervises.
// In-place verbs pass livePaneNone and render unstyled.
func styleHandoffKey(key string, state livePaneState) string {
	switch state {
	case livePaneIdle:
		return livePaneIdleStyle().Render(key)
	case livePaneRunning:
		return livePaneRunningStyle.Render(key)
	default:
		return key
	}
}

// dashboardActivityClusterPlain is the fixed-width, ANSI-free activity cluster
// used for column-width math and the static status table.
const dashboardActivityClusterPlain = "IVFA"

// dashboardMapWayfinderKeyPlain is the fixed-width wayfinder handoff key shown
// on map rows in the activity-cluster column.
const dashboardMapWayfinderKeyPlain = "I"

type rowActivityClusterItem struct {
	key string
	tag tmuxmod.PaneTag
}

// rowActivityCluster lists the supervised activities shown on each dashboard row,
// in the same order and casing as the action-menu handoff keys (ADR-0158).
var rowActivityCluster = []rowActivityClusterItem{
	{key: "I", tag: tmuxmod.TagSet},
	{key: "V", tag: tmuxmod.TagVerify},
	{key: "F", tag: tmuxmod.TagFold},
	{key: "A", tag: tmuxmod.TagAssist},
}

// dashboardActivityCluster renders the compact per-activity cluster for a row.
// Map rows carry a single wayfinder handoff key (I) coloured from the map
// window's liveness. Task-set rows carry IVFA from tagged panes. When styled is
// false the cluster is plain text for width measurement; when true each key is
// coloured by the cached live-pane state using the same rules as the Run menu.
func dashboardActivityCluster(row DashboardRow, live livePaneCache, styled bool) string {
	if mapRow(row) {
		state := live.wayfinderState(row.ID)
		if styled {
			return styleHandoffKey(dashboardMapWayfinderKeyPlain, state)
		}
		return dashboardMapWayfinderKeyPlain
	}
	if row.Kind != "" && row.Kind != ref.KindTaskSet {
		return ""
	}
	var b strings.Builder
	for _, item := range rowActivityCluster {
		state := live.state(item.tag, row.ID)
		if styled {
			b.WriteString(styleHandoffKey(item.key, state))
		} else {
			b.WriteString(item.key)
		}
	}
	return b.String()
}

// menuItemLiveState returns the live-pane state for a handoff menu item on row.
// Non-handoff verbs and shell always return livePaneNone (dark).
func menuItemLiveState(item dashboardMenuItem, row DashboardRow, live livePaneCache) livePaneState {
	if live.byTag == nil && live.wayfinder == nil {
		return livePaneNone
	}
	switch item.verb {
	case setkind.VerbDrain:
		return live.state(tmuxmod.TagSet, row.ID)
	case setkind.VerbVerify:
		return live.state(tmuxmod.TagVerify, row.ID)
	case setkind.VerbFold:
		return live.state(tmuxmod.TagFold, row.ID)
	case setkind.VerbAssist, wayfinder.VerbAssist:
		// A Map's assist pane carries the same @pop_assist tag a Task set's does,
		// keyed by the container id either way, so one lookup answers both kinds.
		return live.state(tmuxmod.TagAssist, row.ID)
	case wayfinder.VerbWork, wayfinder.VerbWorkHere, wayfinder.VerbFanOut, wayfinder.VerbFanOutHere:
		// The Map's frontier verbs read the same session liveness its activity-cluster
		// key does, so the menu and the row agree about whether a session is running.
		return live.wayfinderState(row.ID)
	default:
		return livePaneNone
	}
}
