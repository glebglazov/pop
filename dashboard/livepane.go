package dashboard

import (
	"github.com/glebglazov/pop/tasks/drain"
	"strings"

	"charm.land/lipgloss/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
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
// plus Map liveness keyed by map id (its session). It is rebuilt from tmux list
// queries per dashboard poll — never from the DrainPane store.
type livePaneCache struct {
	byTag     map[tmuxmod.PaneTag]map[string]livePaneState
	wayfinder map[string]livePaneState
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
	c.byTag[tag][setID] = state
}

func (c *livePaneCache) setWayfinder(mapID string, state livePaneState) {
	if c == nil || mapID == "" || state == livePaneNone {
		return
	}
	if c.wayfinder == nil {
		c.wayfinder = map[string]livePaneState{}
	}
	c.wayfinder[mapID] = state
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
		}
	}
	windows, err := tmux.ListWindowPanes()
	if err == nil {
		// A Map's liveness is its session's, not one window's: grilling windows are
		// named after tickets and come and go, while the Map is running as long as
		// any window in `pop-map-<id>` still holds a process.
		for _, w := range windows {
			mapID := wayfinder.MapIDFromSession(w.Session)
			if mapID == "" {
				continue
			}
			state := stateFromCommand(w.Command)
			if state == livePaneRunning || cache.wayfinderState(mapID) == livePaneNone {
				cache.setWayfinder(mapID, state)
			}
		}
	}
	return cache
}

// livePane styles for handoff-verb keys in the action menu.
var (
	livePaneIdleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	livePaneRunningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// styleHandoffKey colours a handoff verb's shortcut by its live-pane state.
// Shell is always dark (livePaneNone): it is not an activity pop supervises.
// In-place verbs pass livePaneNone and render unstyled.
func styleHandoffKey(key string, state livePaneState) string {
	switch state {
	case livePaneIdle:
		return livePaneIdleStyle.Render(key)
	case livePaneRunning:
		return livePaneRunningStyle.Render(key)
	default:
		return key
	}
}

// dashboardActivityClusterPlain is the fixed-width, ANSI-free activity cluster
// used for column-width math and the static status table.
const dashboardActivityClusterPlain = "IVFS"

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
	{key: "S", tag: tmuxmod.TagAssist},
}

// dashboardActivityCluster renders the compact per-activity cluster for a row.
// Map rows carry a single wayfinder handoff key (I) coloured from the map
// window's liveness. Task-set rows carry IVFS from tagged panes. When styled is
// false the cluster is plain text for width measurement; when true each key is
// coloured by the cached live-pane state using the same rules as the action menu.
func dashboardActivityCluster(row DashboardRow, live livePaneCache, styled bool) string {
	if mapRow(row) {
		state := live.wayfinderState(row.ID)
		if styled {
			return styleHandoffKey(dashboardMapWayfinderKeyPlain, state)
		}
		return dashboardMapWayfinderKeyPlain
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
	case setkind.VerbAssist:
		return live.state(tmuxmod.TagAssist, row.ID)
	case wayfinder.VerbWork:
		// The Map's frontier verb reads the same window liveness its activity-cluster
		// key does, so the menu and the row agree about whether a session is running.
		return live.wayfinderState(row.ID)
	default:
		return livePaneNone
	}
}
