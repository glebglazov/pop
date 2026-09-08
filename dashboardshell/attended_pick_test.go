package dashboardshell

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The attended pick, whole: tab on an attended Run-menu row opens the chooser,
// and the picked session choice reaches the row and persistent subheader on both
// pages without changing the real config file in the temp config directory.

// assistKind offers the attended verb, so a menu row names the entry a launch
// from this page would run.
type assistKind struct{ *pageKind }

func (k *assistKind) Actions(work.Container) []work.Action {
	return []work.Action{{Key: "A", Label: "assist", Verb: setkind.VerbAssist}}
}

// attendedFixture is a temp config dir holding two usable attended entries, the
// deps that read it, and the shell over them.
type attendedFixture struct {
	shell        Shell
	overridePath string
	configPath   string
	configBody   string
}

func newAttendedFixture(t *testing.T) attendedFixture {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config", "config.toml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
[work.attended]
agents = [
  { display_name = "Claude Usual", cmd = "claude --model opus" },
  { display_name = "Cursor", cmd = "cursor" },
]
`
	if err := os.WriteFile(userPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataDir
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		WriteFileFunc:   os.WriteFile,
		MkdirAllFunc:    os.MkdirAll,
		RenameFunc:      os.Rename,
		RemoveAllFunc:   os.RemoveAll,
		StatFunc:        os.Stat,
	}
	cfgDeps := &config.Deps{FS: fs}
	load := func() (*config.Config, error) { return config.LoadWith(cfgDeps, userPath) }
	cfg, err := load()
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}

	d := &drain.Deps{
		Tasks: &tasks.Deps{FS: fs},
		Kinds: func(*drain.Deps, *config.Config) []work.Kind {
			return []work.Kind{&assistKind{pageKind: &pageKind{
				id: ref.KindTaskSet, containers: setRows(),
				columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set",
			}}}
		},
		RoutineKinds: func(*drain.Deps, *config.Config) []work.Kind {
			return []work.Kind{&assistKind{pageKind: &pageKind{
				id: ref.KindRoutine, containers: routineRows(),
				columns: []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"}, noun: "routine",
			}}}
		},
	}
	s, err := newShell(PageWork, d, cfg, userPath)
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	s.reloadConfig = load
	updated, _ := s.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return attendedFixture{
		shell:        updated.(Shell),
		overridePath: filepath.Join(dataDir, "pop", "config.override.toml"),
		configPath:   userPath,
		configBody:   body,
	}
}

// pressThrough presses a key and runs whatever the shell asked for, which is how
// the page's request for a re-read reaches the shell.
func pressThrough(t *testing.T, s Shell, msg tea.Msg) Shell {
	t.Helper()
	updated, cmd := s.Update(msg)
	s = updated.(Shell)
	for cmd != nil {
		next := cmd()
		if next == nil {
			return s
		}
		updated, cmd = s.Update(next)
		s = updated.(Shell)
	}
	return s
}

func TestAttendedPickStaysInTheShellAndRerendersBothPages(t *testing.T) {
	fx := newAttendedFixture(t)
	s := fx.shell
	before := s.View().Content
	if !strings.Contains(before, "Claude Usual") {
		t.Fatalf("the page does not name the configured head entry:\n%s", before)
	}

	var view string
	out := captureShellStdout(t, func() {
		s = pressThrough(t, s, tea.KeyPressMsg{Code: 'r', Text: "r"})
		s = pressThrough(t, s, tea.KeyPressMsg{Code: tea.KeyTab})
		if !s.PageDashboard(PageWork).AttendedPickOpen() {
			t.Error("tab on the attended Run-menu row opened no chooser")
			return
		}
		// The shell's own chord is suspended for as long as the chooser is up.
		s = pressThrough(t, s, altShiftC())
		if s.ConfigModalOpen() {
			t.Error("the Config modal opened over the chooser")
			return
		}
		s = pressThrough(t, s, tea.KeyPressMsg{Code: '2', Text: "2"})
		view = s.View().Content
	})
	if t.Failed() {
		return
	}
	if out != "" {
		t.Fatalf("the pick wrote to stdout: %q", out)
	}
	if s.PageDashboard(PageWork).AttendedPickOpen() {
		t.Fatal("the chooser stayed open after a pick")
	}

	if _, err := os.Stat(fx.overridePath); !os.IsNotExist(err) {
		t.Fatalf("the session choice wrote an override: %v", err)
	}
	stored, err := os.ReadFile(fx.configPath)
	if err != nil || string(stored) != fx.configBody {
		t.Fatalf("the session choice changed config: err=%v\n%s", err, stored)
	}

	// The subheader is the persistent one; the row is the assist verb's, reached
	// by opening the run menu over the cursored row.
	if !strings.Contains(view, tasks.FormatAttendedAgentStatus(tasks.AgentGroupEntry{DisplayName: "Cursor", Cmd: "cursor"})) {
		t.Fatalf("subheader was not re-rendered on the picked entry:\n%s", view)
	}
	menu := s.View().Content
	if !strings.Contains(menu, "assist · Cursor") {
		t.Fatalf("the attended action row was not re-rendered on the picked entry:\n%s", menu)
	}
	s = pressThrough(t, s, tea.KeyPressMsg{Code: tea.KeyEscape})
	s = pressThrough(t, s, tea.KeyPressMsg{Code: 'v', Text: "v"})
	if view := s.View().Content; !strings.Contains(view, "agent Cursor") {
		t.Fatalf("the other page did not inherit the session choice:\n%s", view)
	}
}

func TestAttendedPickDoesNotReachAnotherShell(t *testing.T) {
	fx := newAttendedFixture(t)
	first := fx.shell
	first = pressThrough(t, first, tea.KeyPressMsg{Code: 'r', Text: "r"})
	first = pressThrough(t, first, tea.KeyPressMsg{Code: tea.KeyTab})
	first = pressThrough(t, first, tea.KeyPressMsg{Code: '2', Text: "2"})
	if !strings.Contains(first.View().Content, "Cursor") {
		t.Fatal("first shell did not keep its choice")
	}
	second, err := newShell(PageWork, first.d, first.cfg, first.cfgPath)
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	updated, _ := second.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	second = updated.(Shell)
	if view := second.View().Content; !strings.Contains(view, "Claude Usual") || strings.Contains(view, "agent Cursor") {
		t.Fatalf("a fresh shell inherited another shell's choice:\n%s", view)
	}
}

// captureShellStdout swaps os.Stdout for a pipe, runs body, and returns whatever
// was written to it. Both picker hosts read stdout as a data channel, so the
// chooser's silence there is part of what makes it hostable at all.
func captureShellStdout(t *testing.T, body func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	real := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	defer func() { _ = r.Close() }()
	body()
	os.Stdout = real
	_ = w.Close()
	return <-done
}
