// Package tmuxtest provides the one shared, stateful in-memory fake for the
// tmux.Tmux interface. Consumer tests arrange in-memory state (sessions,
// and later windows/panes/options) and assert on that state — they never
// assert on tmux argument arrays. Func-field overrides exist only to inject
// failures. The fake grows one verb at a time alongside the module.
package tmuxtest

import (
	"fmt"

	"github.com/glebglazov/pop/internal/tmux"
)

// Fake is an in-memory tmux.Tmux.
type Fake struct {
	// SessionList is the arranged live-session state returned by Sessions.
	SessionList []tmux.SessionActivity

	// SessionsFunc, when set, replaces Sessions entirely — used to inject
	// failures.
	SessionsFunc func() ([]tmux.SessionActivity, error)

	// Live is the set of existing sessions (name -> creation dir). HasSession
	// reads it, NewSession adds to it, KillSession removes from it. Tests
	// arrange it to model pre-existing sessions and assert on it afterwards.
	Live map[string]string

	// Inside is the arranged inside-tmux state returned by InTmux; it drives
	// SwitchTarget's switch-vs-attach choice.
	Inside bool

	// Switched, Attached and Killed record verb targets in call order so
	// tests can assert which path ran.
	Switched []string
	Attached []string
	Killed   []string

	// PaneInfos maps pane id -> its session/command. PaneInfo and PaneSession
	// read it; an absent pane yields an error (matching tmux for a dead pane).
	PaneInfos map[string]tmux.PaneInfo

	// ActivePanes marks which panes IsActivePane reports as the attended pane.
	ActivePanes map[string]bool

	// LivePaneIDs is the set of pane ids returned by LivePanes.
	LivePaneIDs []string

	// InstalledHooks is the global-hook state: InstallHook appends, GlobalHooks
	// reads, UninstallHook removes by Index. Tests arrange and assert on it.
	InstalledHooks []tmux.Hook

	// CurrentSessionName / CurrentSessionErr drive CurrentSession.
	CurrentSessionName string
	CurrentSessionErr  error

	// AgentWindows maps a session name to its agent-window panes in order. The
	// agent-window verbs (HasAgentWindow, AgentPanes, FindAgentPane,
	// NewAgentWindow, SplitAgentPane, RetileAgentWindow, KillPane) read and
	// mutate it; tests arrange and assert on it.
	AgentWindows map[string][]tmux.AgentPane
	// nextPaneNum seeds generated pane ids for created panes ("%100", "%101",
	// …), high enough not to collide with test-arranged ids.
	nextPaneNum int

	// SentKeys records every SendKeys call per pane id, in order.
	SentKeys map[string][][]string
	// PaneContent is the text CapturePane returns per pane id.
	PaneContent map[string]string
	// DeadPanes marks which panes PaneDead reports as dead.
	DeadPanes map[string]bool
	// RemainOnExit records the last remain-on-exit value set per pane id.
	RemainOnExit map[string]bool
	// Retiled records the sessions whose agent window was re-tiled, in order.
	Retiled []string

	// Topics maps a pane id to its Topic state. The Topic verbs (ReadTopic,
	// SetTopic, SetTopicWithKind, ClearTopic, PaneTopics) read and mutate it.
	Topics map[string]tmux.TopicState

	// Failure-injection overrides. When set, each replaces its verb entirely.
	NewSessionFunc    func(name, dir string) error
	SwitchClientFunc  func(target string) error
	AttachSessionFunc func(target string) error
	KillSessionFunc   func(name string) error
	PaneInfoFunc      func(paneID string) (tmux.PaneInfo, error)
	LivePanesFunc     func() ([]string, error)
}

var _ tmux.Tmux = (*Fake)(nil)

func (f *Fake) Sessions() ([]tmux.SessionActivity, error) {
	if f.SessionsFunc != nil {
		return f.SessionsFunc()
	}
	return f.SessionList, nil
}

func (f *Fake) HasSession(name string) bool {
	_, ok := f.Live[name]
	return ok
}

func (f *Fake) NewSession(name, dir string) error {
	if f.NewSessionFunc != nil {
		return f.NewSessionFunc(name, dir)
	}
	if f.Live == nil {
		f.Live = map[string]string{}
	}
	f.Live[name] = dir
	return nil
}

func (f *Fake) SwitchClient(target string) error {
	if f.SwitchClientFunc != nil {
		return f.SwitchClientFunc(target)
	}
	f.Switched = append(f.Switched, target)
	return nil
}

func (f *Fake) AttachSession(target string) error {
	if f.AttachSessionFunc != nil {
		return f.AttachSessionFunc(target)
	}
	f.Attached = append(f.Attached, target)
	return nil
}

func (f *Fake) KillSession(name string) error {
	if f.KillSessionFunc != nil {
		return f.KillSessionFunc(name)
	}
	f.Killed = append(f.Killed, name)
	delete(f.Live, name)
	return nil
}

func (f *Fake) InTmux() bool { return f.Inside }

func (f *Fake) PaneInfo(paneID string) (tmux.PaneInfo, error) {
	if f.PaneInfoFunc != nil {
		return f.PaneInfoFunc(paneID)
	}
	info, ok := f.PaneInfos[paneID]
	if !ok {
		return tmux.PaneInfo{}, fmt.Errorf("pane not found: %s", paneID)
	}
	return info, nil
}

func (f *Fake) PaneSession(paneID string) (string, error) {
	info, err := f.PaneInfo(paneID)
	if err != nil {
		return "", err
	}
	return info.Session, nil
}

func (f *Fake) IsActivePane(paneID string) bool {
	return f.ActivePanes[paneID]
}

func (f *Fake) LivePanes() ([]string, error) {
	if f.LivePanesFunc != nil {
		return f.LivePanesFunc()
	}
	return f.LivePaneIDs, nil
}

func (f *Fake) InstallHook(event, command string) error {
	f.InstalledHooks = append(f.InstalledHooks, tmux.Hook{Index: event, Command: command})
	return nil
}

func (f *Fake) GlobalHooks() ([]tmux.Hook, error) {
	return f.InstalledHooks, nil
}

func (f *Fake) UninstallHook(indexed string) error {
	kept := f.InstalledHooks[:0]
	for _, h := range f.InstalledHooks {
		if h.Index != indexed {
			kept = append(kept, h)
		}
	}
	f.InstalledHooks = kept
	return nil
}

func (f *Fake) CurrentSession() (string, error) {
	if f.CurrentSessionErr != nil {
		return "", f.CurrentSessionErr
	}
	return f.CurrentSessionName, nil
}

// --- agentic panes ---

func (f *Fake) HasAgentWindow(session string) bool {
	return len(f.AgentWindows[session]) > 0
}

func (f *Fake) AgentPanes(session string) ([]tmux.AgentPane, error) {
	panes, ok := f.AgentWindows[session]
	if !ok {
		return nil, fmt.Errorf("no agent window in session %q", session)
	}
	return panes, nil
}

func (f *Fake) FindAgentPane(session, title string) (string, error) {
	panes, err := f.AgentPanes(session)
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

// newPaneID mints a fresh pane id for a created pane.
func (f *Fake) newPaneID() string {
	if f.nextPaneNum == 0 {
		f.nextPaneNum = 100
	}
	id := fmt.Sprintf("%%%d", f.nextPaneNum)
	f.nextPaneNum++
	return id
}

func (f *Fake) NewAgentWindow(session, dir string) (string, error) {
	if f.AgentWindows == nil {
		f.AgentWindows = map[string][]tmux.AgentPane{}
	}
	id := f.newPaneID()
	f.AgentWindows[session] = append(f.AgentWindows[session], tmux.AgentPane{ID: id})
	return id, nil
}

func (f *Fake) SplitAgentPane(session, dir string) (string, error) {
	return f.NewAgentWindow(session, dir)
}

func (f *Fake) RetileAgentWindow(session string) error {
	f.Retiled = append(f.Retiled, session)
	return nil
}

// --- pane-id primitives ---

func (f *Fake) SetPaneTitle(paneID, title string) error {
	for session, panes := range f.AgentWindows {
		for i := range panes {
			if panes[i].ID == paneID {
				f.AgentWindows[session][i].Title = title
				return nil
			}
		}
	}
	return nil
}

func (f *Fake) SetRemainOnExit(paneID string, on bool) error {
	if f.RemainOnExit == nil {
		f.RemainOnExit = map[string]bool{}
	}
	f.RemainOnExit[paneID] = on
	return nil
}

func (f *Fake) SendKeys(paneID string, keys ...string) error {
	if f.SentKeys == nil {
		f.SentKeys = map[string][][]string{}
	}
	f.SentKeys[paneID] = append(f.SentKeys[paneID], append([]string(nil), keys...))
	return nil
}

func (f *Fake) KillPane(paneID string) error {
	for session, panes := range f.AgentWindows {
		kept := panes[:0]
		for _, p := range panes {
			if p.ID != paneID {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			delete(f.AgentWindows, session)
		} else {
			f.AgentWindows[session] = kept
		}
	}
	return nil
}

func (f *Fake) CapturePane(paneID string) (string, error) {
	return f.PaneContent[paneID], nil
}

func (f *Fake) PaneDead(paneID string) bool {
	return f.DeadPanes[paneID]
}

// --- Topic ---

func (f *Fake) ReadTopic(paneID string) (tmux.TopicState, error) {
	st, ok := f.Topics[paneID]
	if !ok {
		return tmux.TopicState{}, fmt.Errorf("pane not found: %s", paneID)
	}
	return st, nil
}

func (f *Fake) SetTopic(paneID, topic string) error {
	if f.Topics == nil {
		f.Topics = map[string]tmux.TopicState{}
	}
	st := f.Topics[paneID]
	st.Topic = topic
	f.Topics[paneID] = st
	return nil
}

func (f *Fake) SetTopicWithKind(paneID, topic, kind string) error {
	if f.Topics == nil {
		f.Topics = map[string]tmux.TopicState{}
	}
	st := f.Topics[paneID]
	st.Topic = topic
	st.Kind = kind
	f.Topics[paneID] = st
	return nil
}

func (f *Fake) ClearTopic(paneID string) error {
	return f.SetTopicWithKind(paneID, "", "")
}

func (f *Fake) PaneTopics() (map[string]string, error) {
	result := make(map[string]string)
	for id, st := range f.Topics {
		if st.Topic != "" {
			result[id] = st.Topic
		}
	}
	return result, nil
}
