// Package queuetest holds the test doubles and repository fixtures the drain
// pipeline, the Work dashboard and the supervisor loop all drive their tests
// through. They live in one importable package rather than three copies because
// the three packages exercise the same seams from different sides.
package queuetest

import (
	"testing"

	"fmt"
	"strings"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

// RecordingTmux captures tmux subcommands so spawn behaviour can be asserted by
// the non-spawn tests that only need a placeholder tmux. It embeds the shared
// stateful fake for the verbs it does not override and replays the old
// session-agnostic seeded behaviour (windowNames + paneList). Dedicated spawn
// tests use a plain tmuxtest.Fake and assert on pane state instead.
// drainWindowName mirrors the drain pipeline's tmux window name. It is copied
// rather than imported so this fixture stays importable from that package's own
// tests.
const drainWindowName = "pop-work"

type RecordingTmux struct {
	*tmuxtest.Fake
	Commands      [][]string
	SessionLive   bool
	WindowNames   map[string]bool
	PaneList      string
	SplitErr      error
	NextSplitPane int
}

func NewRecordingTmux(hasSession bool, windowNames string) *RecordingTmux {
	rt := &RecordingTmux{Fake: &tmuxtest.Fake{}, SessionLive: hasSession, WindowNames: map[string]bool{}}
	for _, w := range strings.Split(windowNames, "\n") {
		if w != "" {
			rt.WindowNames[w] = true
		}
	}
	return rt
}

func (rt *RecordingTmux) record(args ...string) { rt.Commands = append(rt.Commands, args) }

func (rt *RecordingTmux) HasSession(string) bool { return rt.SessionLive }

func (rt *RecordingTmux) NewSession(name, dir string) error {
	rt.record("new-session", name, dir)
	return nil
}

func (rt *RecordingTmux) WindowExists(session, name string) (bool, error) {
	rt.record("list-windows", "-t", session)
	return rt.WindowNames[name], nil
}

func (rt *RecordingTmux) NewWindow(session, name, dir string) (string, error) {
	rt.record("new-window", "-d", "-P", "-t", session, "-n", name, "-c", dir)
	return "%3", nil
}

func (rt *RecordingTmux) SplitWindow(session, name, dir string) (string, error) {
	rt.record("split-window", "-d", "-P", "-t", session+":"+name, "-c", dir)
	if rt.SplitErr != nil {
		return "", rt.SplitErr
	}
	if rt.NextSplitPane == 0 {
		rt.NextSplitPane = 7
	}
	paneID := fmt.Sprintf("%%%d", rt.NextSplitPane)
	rt.NextSplitPane++
	return paneID, nil
}

func (rt *RecordingTmux) RetileWindow(session, name string) error {
	rt.record("select-layout", "-t", session+":"+name, "tiled")
	return nil
}

func (rt *RecordingTmux) WindowPanes(session, name string) ([]string, error) {
	rt.record("list-panes", "-t", session+":"+name)
	return PaneIDs(rt.PaneList), nil
}

func (rt *RecordingTmux) FindTaggedPane(session string, tag tmuxmod.PaneTag, value string) (string, error) {
	rt.record("list-panes", "-t", session+":"+drainWindowName)
	for paneID, tags := range rt.PaneTagValues {
		if tags[tag] == value {
			return paneID, nil
		}
	}
	return "", nil
}

func (rt *RecordingTmux) TagPane(paneID string, tag tmuxmod.PaneTag, value string) error {
	rt.record("set-option", "-p", "-t", paneID, value)
	return rt.Fake.TagPane(paneID, tag, value)
}

func (rt *RecordingTmux) SelectPane(paneID string) error {
	rt.record("select-pane", "-t", paneID)
	return nil
}

func (rt *RecordingTmux) SwitchClient(target string) error {
	rt.record("switch-client", "-t", target)
	return nil
}

func (rt *RecordingTmux) SendKeys(paneID string, keys ...string) error {
	rt.record(append([]string{"send-keys", "-t", paneID}, keys...)...)
	return nil
}

func PaneIDs(list string) []string {
	var ids []string
	for _, line := range strings.Split(list, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func TaggedPane(list, value string) string {
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.LastIndex(line, " %")
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) == value {
			return strings.TrimSpace(line[idx+1:])
		}
	}
	return ""
}

func (rt *RecordingTmux) FindCommand(verb string) ([]string, bool) {
	for _, c := range rt.Commands {
		if len(c) > 0 && c[0] == verb {
			return c, true
		}
	}
	return nil, false
}

func (rt *RecordingTmux) CountCommand(verb string) int {
	var n int
	for _, c := range rt.Commands {
		if len(c) > 0 && c[0] == verb {
			n++
		}
	}
	return n
}

func (rt *RecordingTmux) FindSwitched(target string) bool {
	for _, c := range rt.Commands {
		if len(c) >= 2 && c[0] == "switch-client" && c[len(c)-1] == target {
			return true
		}
		if len(c) >= 2 && c[0] == "select-pane" && c[len(c)-1] == target {
			return true
		}
	}
	return false
}

func ExtractSpawnCommand(rt *RecordingTmux) (string, bool) {
	sendKeys, ok := rt.FindCommand("send-keys")
	if !ok {
		return "", false
	}
	for i, arg := range sendKeys {
		if strings.HasPrefix(arg, "pop tasks implement ") {
			return arg, true
		}
		if i > 0 && sendKeys[i-1] == "-t" && strings.HasPrefix(arg, "pop tasks implement ") {
			return arg, true
		}
	}
	joined := strings.Join(sendKeys, " ")
	if idx := strings.Index(joined, "pop tasks implement "); idx >= 0 {
		cmd := joined[idx:]
		if end := strings.Index(cmd, " Enter"); end >= 0 {
			cmd = cmd[:end]
		}
		return cmd, true
	}
	return "", false
}

// AssertReusesFreshPane checks the supervisor spawn reused a freshly created
// single-pane drain window (no split, no retile) and sent the drain command
// into it. Still recorder-based because the supervisor tick tests assert on the
// recorded subcommands.
func AssertReusesFreshPane(t *testing.T, rt *RecordingTmux, paneID string) {
	t.Helper()
	if _, ok := rt.FindCommand("split-window"); ok {
		t.Fatal("must reuse the freshly created window's pane, not split a second pane")
	}
	if _, ok := rt.FindCommand("select-layout"); ok {
		t.Fatal("must not retile a single-pane drain window")
	}
	sendKeys, ok := rt.FindCommand("send-keys")
	if !ok {
		t.Fatal("expected the drain command to be sent into the pane")
	}
	if !ArgsContain(sendKeys, "-t", paneID) {
		t.Fatalf("send-keys must target reused pane %s: %v", paneID, sendKeys)
	}
}
