package queue

import (
	"charm.land/lipgloss/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
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

// livePaneCache holds per-poll activity liveness keyed by pane tag then set id.
// It is rebuilt from a single tmux ListActivityPanes query per dashboard poll —
// never from the DrainPane store.
type livePaneCache map[tmuxmod.PaneTag]map[string]livePaneState

func (c livePaneCache) state(tag tmuxmod.PaneTag, setID string) livePaneState {
	if c == nil || setID == "" {
		return livePaneNone
	}
	bySet := c[tag]
	if bySet == nil {
		return livePaneNone
	}
	return bySet[setID]
}

func (c livePaneCache) set(tag tmuxmod.PaneTag, setID string, state livePaneState) {
	if setID == "" || state == livePaneNone {
		return
	}
	if c[tag] == nil {
		c[tag] = map[string]livePaneState{}
	}
	c[tag][setID] = state
}

func stateFromCommand(cmd string) livePaneState {
	if tmuxmod.IsBareShell(cmd) {
		return livePaneIdle
	}
	return livePaneRunning
}

// loadLivePaneCache queries tmux once and maps each tagged activity pane to its
// live-pane state. A nil Tmux or a query failure yields an empty cache (all
// keys render dark) rather than blocking the dashboard poll.
func loadLivePaneCache(d *Deps) livePaneCache {
	cache := livePaneCache{}
	if d == nil {
		return cache
	}
	tmux := d.Tmux
	if tmux == nil {
		return cache
	}
	panes, err := tmux.ListActivityPanes()
	if err != nil {
		return cache
	}
	for _, p := range panes {
		state := stateFromCommand(p.Command)
		cache.set(tmuxmod.TagSet, p.Set, state)
		cache.set(tmuxmod.TagVerify, p.Verify, state)
		cache.set(tmuxmod.TagFold, p.Fold, state)
		cache.set(tmuxmod.TagAssist, p.Assist, state)
	}
	return cache
}

// runningTaggedPane returns the pane id when a tagged pane exists and its
// foreground command is not a bare shell — the green / jump case. An idle
// tagged pane (grey / respawn) returns "" so the caller falls through to
// EnsureTaggedPane. When the command cannot be read, the pane is treated as
// running so we never SendKeys into an unknown process.
func runningTaggedPane(t tmuxmod.Tmux, session string, tag tmuxmod.PaneTag, setID string) (string, error) {
	if t == nil || session == "" || setID == "" {
		return "", nil
	}
	paneID, err := t.FindTaggedPane(session, tag, setID)
	if err != nil || paneID == "" {
		return paneID, err
	}
	info, err := t.PaneInfo(paneID)
	if err != nil {
		return paneID, nil
	}
	if tmuxmod.IsBareShell(info.Command) {
		return "", nil
	}
	return paneID, nil
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

// menuItemLiveState returns the live-pane state for a handoff menu item on row.
// Non-handoff verbs and shell always return livePaneNone (dark).
func menuItemLiveState(item dashboardMenuItem, row DashboardRow, live livePaneCache) livePaneState {
	if row.IsMap || live == nil {
		return livePaneNone
	}
	switch item.action {
	case menuActionDrain:
		return live.state(tmuxmod.TagSet, row.SetID)
	case menuActionVerify:
		return live.state(tmuxmod.TagVerify, row.SetID)
	case menuActionFold:
		return live.state(tmuxmod.TagFold, row.SetID)
	case menuActionAssist:
		return live.state(tmuxmod.TagAssist, row.SetID)
	case menuActionShell:
		return livePaneNone
	default:
		return livePaneNone
	}
}
