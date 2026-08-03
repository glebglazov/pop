package routine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks"
)

// Shared fixtures for the tests that need a Routine data dir plus a tmux double
// that records what was run: the verb tests here, the refine-spawn tests, and the
// Project-routine edit tests all drive real spawns through it.

func routineTmuxDeps(t *testing.T) (*Deps, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	real := deps.NewRealFileSystem()
	td := tasks.DefaultDeps()
	td.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
		StatFunc:      real.Stat,
		ReadDirFunc:   real.ReadDir,
		UserHomeDirFunc: func() (string, error) {
			return filepath.Join(dir, "home"), nil
		},
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		FS:             td.FS,
		Now:            func() time.Time { return time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC) },
		LoadConfig:     func() (*config.Config, error) { return &config.Config{}, nil },
		Tasks:          td,
		Tmux:           newRecordingTmux(false, "0"),
		InTmux:         func() bool { return true },
		Executable:     func() (string, error) { return "/mock/bin/pop", nil },
		IsInteractive:  func() bool { return false },
		PID:            func() int { return 4242 },
		ProcStartToken: func(pid int) (string, bool) { return "test", true },
		ProcessAlive:   func(pid int, procStart string) bool { return pid == 9999 },
	}
	// Borrowers never close the process-cached store handle (ADR-0140); close it
	// once at test end through the accessor's closer.
	t.Cleanup(func() { _ = td.CloseStore() })
	return d, home
}

// recordingTmux is a command-recording tmux.Tmux for the routine tests that
// only need a placeholder tmux or assert on which subcommands ran. It embeds
// the shared stateful fake for the verbs it does not override, and replays the
// old session-agnostic seeded behaviour (windowNames + paneList) so the many
// non-spawn tests keep working unchanged. Dedicated spawn tests use a plain
// tmuxtest.Fake and assert on pane state instead.
type recordingTmux struct {
	*tmuxtest.Fake
	commands    [][]string
	hasSession  bool
	windowNames map[string]bool
	paneList    string
}

func newRecordingTmux(hasSession bool, listOut string) *recordingTmux {
	rt := &recordingTmux{Fake: &tmuxtest.Fake{}, hasSession: hasSession, windowNames: map[string]bool{}}
	for _, w := range strings.Split(listOut, "\n") {
		if w != "" {
			rt.windowNames[w] = true
		}
	}
	return rt
}

func (rt *recordingTmux) record(args ...string) { rt.commands = append(rt.commands, args) }

func (rt *recordingTmux) HasSession(string) bool { return rt.hasSession }

func (rt *recordingTmux) NewSession(name, dir string) error {
	rt.record("new-session", name, dir)
	return nil
}

func (rt *recordingTmux) WindowExists(session, name string) (bool, error) {
	rt.record("list-windows", "-t", session)
	return rt.windowNames[name], nil
}

func (rt *recordingTmux) NewWindow(session, name, dir string) (string, error) {
	rt.record("new-window", "-d", "-P", "-t", session, "-n", name, "-c", dir)
	return "%42", nil
}

func (rt *recordingTmux) SplitWindow(session, name, dir string) (string, error) {
	rt.record("split-window", "-d", "-P", "-t", session+":"+name, "-c", dir)
	return "%43", nil
}

func (rt *recordingTmux) RetileWindow(session, name string) error {
	rt.record("select-layout", "-t", session+":"+name, "tiled")
	return nil
}

func (rt *recordingTmux) WindowPanes(session, name string) ([]string, error) {
	rt.record("list-panes", "-t", session+":"+name)
	return recorderPaneIDs(rt.paneList), nil
}

func (rt *recordingTmux) FindTaggedPane(session string, _ tmuxmod.PaneTag, value string) (string, error) {
	rt.record("list-panes", "-t", session+":pop-work")
	return recorderTaggedPane(rt.paneList, value), nil
}

func (rt *recordingTmux) TagPane(paneID string, _ tmuxmod.PaneTag, value string) error {
	rt.record("set-option", "-p", "-t", paneID, value)
	return nil
}

func (rt *recordingTmux) SelectPane(paneID string) error {
	rt.record("select-pane", "-t", paneID)
	return nil
}

func (rt *recordingTmux) SwitchClient(target string) error {
	rt.record("switch-client", "-t", target)
	return nil
}

func (rt *recordingTmux) SendKeys(paneID string, keys ...string) error {
	rt.record(append([]string{"send-keys", "-t", paneID}, keys...)...)
	return nil
}

func recorderPaneIDs(list string) []string {
	var ids []string
	for _, line := range strings.Split(list, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func recorderTaggedPane(list, value string) string {
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

func (rt *recordingTmux) findCommand(verb string) ([]string, bool) {
	for _, c := range rt.commands {
		if len(c) > 0 && c[0] == verb {
			return c, true
		}
	}
	return nil, false
}

func tmuxRecorder(d *Deps) *recordingTmux {
	return d.Tmux.(*recordingTmux)
}

func tmuxHasCommand(rt *recordingTmux, name string) bool {
	_, ok := rt.findCommand(name)
	return ok
}

func containsArg(args []string, flag, value string) bool {
	if value == "" {
		return argPresent(args, flag)
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func argPresent(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
