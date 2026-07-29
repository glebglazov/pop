// Package tmuxtest provides the one shared, stateful in-memory fake for the
// tmux.Tmux interface. Consumer tests arrange in-memory state (sessions,
// and later windows/panes/options) and assert on that state — they never
// assert on tmux argument arrays. Func-field overrides exist only to inject
// failures. The fake grows one verb at a time alongside the module.
package tmuxtest

import (
	"fmt"
	"strings"

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

	// CurrentPaneID / CurrentPaneErr drive CurrentPane.
	CurrentPaneID  string
	CurrentPaneErr error

	// PaneCommandMap is the pane id -> current command map returned by
	// PaneCommands.
	PaneCommandMap map[string]string
	// PreviewContent is the text CapturePreview returns per pane id.
	PreviewContent map[string]string
	// CapturePreviewFunc, when set, replaces CapturePreview entirely — used to
	// inject failures.
	CapturePreviewFunc func(paneID string) (string, error)

	// Zoomed maps a switch target to whether its window is currently zoomed.
	// WindowZoomed reads it; ZoomPane toggles it, so an already-zoomed target
	// left alone by SwitchAndZoom stays zoomed.
	Zoomed map[string]bool
	// SelectedWindows records "session:window" targets SelectWindow selected,
	// in order.
	SelectedWindows []string

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

	// Topics maps a pane id to its Topic state. The Topic verbs (ReadTopic,
	// SetTopic, SetTopicWithKind, ClearTopic, PaneTopics) read and mutate it.
	Topics map[string]tmux.TopicState

	// Windows models named windows: session -> window name -> ordered pane ids.
	// The window/pane spawn verbs (WindowExists, NewWindow, SplitWindow,
	// WindowPanes, WindowTitledPanes, FindPaneByTitle, RetileWindow) and the
	// tagged-pane composite read and mutate it; tests arrange and assert on it.
	Windows map[string]map[string][]string
	// PaneTagValues maps a pane id to its @pop_* tag values keyed by PaneTag.
	// TagPane writes it, FindTaggedPane reads it.
	PaneTagValues map[string]map[tmux.PaneTag]string
	// SentCommands records SendKeys calls joined with spaces per pane id (a
	// spawned command is sent as "<command> Enter"), in order.
	SentCommands map[string][]string
	// Selected records the panes SelectPane targeted, in order.
	Selected []string
	// WindowRetiled records "session:window" retiled by RetileWindow, in order.
	WindowRetiled []string
	// WindowCwd records the creation directory of each window NewWindow makes,
	// keyed by "session:window".
	WindowCwd map[string]string

	// --- workbench layout (@pop_wb_window / @pop_pane) ---

	// LiveWBWindows is arranged input for LiveWorkbenchWindows: session ->
	// (@pop_wb_window identity -> window id). Merge tests arrange it.
	LiveWBWindows map[string]map[string]string
	// LiveWBPanes is arranged input for LivePaneIdentities: window ref ->
	// (@pop_pane identity -> pane id).
	LiveWBPanes map[string]map[string]string
	// LiveWBFallback is arranged input for LivePaneIdentities' fallback anchor:
	// window ref -> first pane id.
	LiveWBFallback map[string]string
	// WindowW / WindowH are the dimensions WindowSize reports for every target
	// (a test builds one window). Zero values default to tmux's 80x24.
	WindowW int
	WindowH int

	// WBWindowIdentity records StampWorkbenchWindow: window target -> identity.
	WBWindowIdentity map[string]string
	// AutoRenameOff records window targets whose automatic-rename was disabled.
	AutoRenameOff []string
	// PaneIdentity records StampPane: pane id -> @pop_pane identity.
	PaneIdentity map[string]string
	// PaneTitles records SetPaneTitle: pane id -> title.
	PaneTitles map[string]string
	// PaneCwd records each pane's working directory. NewWindow, SplitWindow, and
	// RespawnPane write it; PaneCurrentPath reads it.
	PaneCwd map[string]string
	// Respawned records RespawnPane: pane id -> new working directory.
	Respawned map[string]string
	// ResizedWidth / ResizedHeight record ResizePane by axis: pane id -> size.
	ResizedWidth  map[string]int
	ResizedHeight map[string]int
	// KilledWindows records KillWindow targets, in order.
	KilledWindows []string
	// ScaffoldSessions records NewScaffoldSession: session name -> dir.
	ScaffoldSessions map[string]string
	// SelectedWindowTargets records SelectWindowTarget targets, in order.
	SelectedWindowTargets []string
	// SplitPanes records SplitPane specs, in order.
	SplitPanes []tmux.SplitSpec

	// ClipboardBuffer records the last text LoadBuffer wrote.
	ClipboardBuffer string
	// LoadBufferFunc, when set, replaces LoadBuffer entirely — used to inject
	// failures (e.g. no tmux server).
	LoadBufferFunc func(text string) error

	// Failure-injection overrides. When set, each replaces its verb entirely.
	NewSessionFunc         func(name, dir string) error
	SwitchClientFunc       func(target string) error
	AttachSessionFunc      func(target string) error
	KillSessionFunc        func(name string) error
	PaneInfoFunc           func(paneID string) (tmux.PaneInfo, error)
	LivePanesFunc          func() ([]string, error)
	NewWindowFunc          func(session, name, dir string) (string, error)
	SplitWindowFunc        func(session, name, dir string) (string, error)
	NewScaffoldSessionFunc func(name, dir string) (string, error)
	SplitPaneFunc          func(spec tmux.SplitSpec) (string, error)

	// nextWindowNum seeds generated window ids for scaffold sessions ("@200",
	// "@201", …), high enough not to collide with test-arranged ids.
	nextWindowNum int
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

func (f *Fake) PaneCurrentPath(paneID string) (string, error) {
	if f.PaneCwd == nil {
		return "", nil
	}
	return f.PaneCwd[paneID], nil
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

func (f *Fake) CurrentPane() (string, error) {
	if f.CurrentPaneErr != nil {
		return "", f.CurrentPaneErr
	}
	return f.CurrentPaneID, nil
}

func (f *Fake) PaneCommands() (map[string]string, error) {
	return f.PaneCommandMap, nil
}

func (f *Fake) CapturePreview(paneID string) (string, error) {
	if f.CapturePreviewFunc != nil {
		return f.CapturePreviewFunc(paneID)
	}
	return f.PreviewContent[paneID], nil
}

func (f *Fake) WindowZoomed(target string) (bool, error) {
	return f.Zoomed[target], nil
}

func (f *Fake) ZoomPane(target string) error {
	if f.Zoomed == nil {
		f.Zoomed = map[string]bool{}
	}
	f.Zoomed[target] = !f.Zoomed[target]
	return nil
}

// --- pane-id primitives ---

// newPaneID mints a fresh pane id for a created pane.
func (f *Fake) newPaneID() string {
	if f.nextPaneNum == 0 {
		f.nextPaneNum = 100
	}
	id := fmt.Sprintf("%%%d", f.nextPaneNum)
	f.nextPaneNum++
	return id
}

func (f *Fake) SetPaneTitle(paneID, title string) error {
	if f.PaneTitles == nil {
		f.PaneTitles = map[string]string{}
	}
	f.PaneTitles[paneID] = title
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
	if f.SentCommands == nil {
		f.SentCommands = map[string][]string{}
	}
	f.SentCommands[paneID] = append(f.SentCommands[paneID], strings.Join(keys, " "))
	return nil
}

func (f *Fake) KillPane(paneID string) error {
	for session, windows := range f.Windows {
		for name, panes := range windows {
			kept := panes[:0]
			for _, id := range panes {
				if id != paneID {
					kept = append(kept, id)
				}
			}
			if len(kept) == 0 {
				delete(f.Windows[session], name)
			} else {
				f.Windows[session][name] = kept
			}
		}
		if len(f.Windows[session]) == 0 {
			delete(f.Windows, session)
		}
	}
	delete(f.PaneTitles, paneID)
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

// --- generic windows / panes ---

func (f *Fake) WindowExists(session, name string) (bool, error) {
	if f.Windows[session] == nil {
		return false, nil
	}
	_, ok := f.Windows[session][name]
	return ok, nil
}

func (f *Fake) addWindowPane(session, name, id string) {
	if f.Windows == nil {
		f.Windows = map[string]map[string][]string{}
	}
	if f.Windows[session] == nil {
		f.Windows[session] = map[string][]string{}
	}
	f.Windows[session][name] = append(f.Windows[session][name], id)
}

func (f *Fake) setPaneCwd(paneID, dir string) {
	if f.PaneCwd == nil {
		f.PaneCwd = map[string]string{}
	}
	f.PaneCwd[paneID] = dir
}

func (f *Fake) NewWindow(session, name, dir string) (string, error) {
	if f.NewWindowFunc != nil {
		return f.NewWindowFunc(session, name, dir)
	}
	id := f.newPaneID()
	f.addWindowPane(session, name, id)
	if f.WindowCwd == nil {
		f.WindowCwd = map[string]string{}
	}
	f.WindowCwd[session+":"+name] = dir
	f.setPaneCwd(id, dir)
	return id, nil
}

func (f *Fake) SplitWindow(session, name, dir string) (string, error) {
	if f.SplitWindowFunc != nil {
		return f.SplitWindowFunc(session, name, dir)
	}
	id := f.newPaneID()
	f.addWindowPane(session, name, id)
	f.setPaneCwd(id, dir)
	return id, nil
}

func (f *Fake) RetileWindow(session, name string) error {
	f.WindowRetiled = append(f.WindowRetiled, session+":"+name)
	return nil
}

func (f *Fake) WindowPanes(session, name string) ([]string, error) {
	if f.Windows[session] == nil {
		return nil, fmt.Errorf("no window %s:%s", session, name)
	}
	panes, ok := f.Windows[session][name]
	if !ok {
		return nil, fmt.Errorf("no window %s:%s", session, name)
	}
	return panes, nil
}

func (f *Fake) WindowTitledPanes(session, name string) ([]tmux.TitledPane, error) {
	ids, err := f.WindowPanes(session, name)
	if err != nil {
		return nil, fmt.Errorf("no window %q in session %q", name, session)
	}
	panes := make([]tmux.TitledPane, 0, len(ids))
	for _, id := range ids {
		panes = append(panes, tmux.TitledPane{Title: f.PaneTitles[id], ID: id})
	}
	return panes, nil
}

func (f *Fake) FindPaneByTitle(session, name, title string) (string, error) {
	panes, err := f.WindowTitledPanes(session, name)
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

func (f *Fake) SelectPane(paneID string) error {
	f.Selected = append(f.Selected, paneID)
	return nil
}

func (f *Fake) SelectWindow(session, name string) error {
	f.SelectedWindows = append(f.SelectedWindows, session+":"+name)
	return nil
}

// --- tagged panes ---

func (f *Fake) TagPane(paneID string, tag tmux.PaneTag, value string) error {
	if f.PaneTagValues == nil {
		f.PaneTagValues = map[string]map[tmux.PaneTag]string{}
	}
	if f.PaneTagValues[paneID] == nil {
		f.PaneTagValues[paneID] = map[tmux.PaneTag]string{}
	}
	f.PaneTagValues[paneID][tag] = value
	return nil
}

func (f *Fake) FindTaggedPane(session string, tag tmux.PaneTag, value string) (string, error) {
	for _, panes := range f.Windows[session] {
		for _, id := range panes {
			if f.PaneTagValues[id] != nil && f.PaneTagValues[id][tag] == value {
				return id, nil
			}
		}
	}
	return "", nil
}

// --- workbench layout ---

// newWindowID mints a fresh window id for a scaffold session.
func (f *Fake) newWindowID() string {
	if f.nextWindowNum == 0 {
		f.nextWindowNum = 200
	}
	id := fmt.Sprintf("@%d", f.nextWindowNum)
	f.nextWindowNum++
	return id
}

func (f *Fake) NewScaffoldSession(name, dir string) (string, error) {
	if f.NewScaffoldSessionFunc != nil {
		return f.NewScaffoldSessionFunc(name, dir)
	}
	if f.ScaffoldSessions == nil {
		f.ScaffoldSessions = map[string]string{}
	}
	f.ScaffoldSessions[name] = dir
	if f.Live == nil {
		f.Live = map[string]string{}
	}
	f.Live[name] = dir
	return f.newWindowID(), nil
}

func (f *Fake) LiveWorkbenchWindows(session string) (map[string]string, error) {
	result := make(map[string]string)
	for k, v := range f.LiveWBWindows[session] {
		result[k] = v
	}
	return result, nil
}

func (f *Fake) LivePaneIdentities(windowRef string) (map[string]string, string, error) {
	result := make(map[string]string)
	for k, v := range f.LiveWBPanes[windowRef] {
		result[k] = v
	}
	return result, f.LiveWBFallback[windowRef], nil
}

func (f *Fake) StampWorkbenchWindow(windowTarget, identity string) error {
	if f.WBWindowIdentity == nil {
		f.WBWindowIdentity = map[string]string{}
	}
	f.WBWindowIdentity[windowTarget] = identity
	return nil
}

func (f *Fake) DisableAutomaticRename(windowTarget string) error {
	f.AutoRenameOff = append(f.AutoRenameOff, windowTarget)
	return nil
}

func (f *Fake) StampPane(paneID, identity string) error {
	if f.PaneIdentity == nil {
		f.PaneIdentity = map[string]string{}
	}
	f.PaneIdentity[paneID] = identity
	return nil
}

func (f *Fake) WindowSize(target string) (int, int, error) {
	w, h := f.WindowW, f.WindowH
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	return w, h, nil
}

func (f *Fake) ResizePane(paneID string, horizontal bool, size int) error {
	if horizontal {
		if f.ResizedWidth == nil {
			f.ResizedWidth = map[string]int{}
		}
		f.ResizedWidth[paneID] = size
	} else {
		if f.ResizedHeight == nil {
			f.ResizedHeight = map[string]int{}
		}
		f.ResizedHeight[paneID] = size
	}
	return nil
}

func (f *Fake) RespawnPane(paneID, dir string) error {
	if f.Respawned == nil {
		f.Respawned = map[string]string{}
	}
	f.Respawned[paneID] = dir
	f.setPaneCwd(paneID, dir)
	return nil
}

func (f *Fake) SplitPane(spec tmux.SplitSpec) (string, error) {
	if f.SplitPaneFunc != nil {
		return f.SplitPaneFunc(spec)
	}
	f.SplitPanes = append(f.SplitPanes, spec)
	return f.newPaneID(), nil
}

func (f *Fake) KillWindow(target string) error {
	f.KilledWindows = append(f.KilledWindows, target)
	return nil
}

func (f *Fake) SelectWindowTarget(target string) error {
	f.SelectedWindowTargets = append(f.SelectedWindowTargets, target)
	return nil
}

func (f *Fake) LoadBuffer(text string) error {
	if f.LoadBufferFunc != nil {
		return f.LoadBufferFunc(text)
	}
	f.ClipboardBuffer = text
	return nil
}
