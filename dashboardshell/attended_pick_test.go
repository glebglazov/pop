package dashboardshell

import (
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

type assistKind struct{ *pageKind }

func (k *assistKind) Actions(work.Container) []work.Action {
	return []work.Action{{Key: "A", Label: "assist", Verb: setkind.VerbAssist}}
}

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
		ReadFileFunc:    os.ReadFile, WriteFileFunc: os.WriteFile,
		MkdirAllFunc: os.MkdirAll, RenameFunc: os.Rename,
		RemoveAllFunc: os.RemoveAll, StatFunc: os.Stat,
	}
	cfg, err := config.LoadWith(&config.Deps{FS: fs}, userPath)
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
	}
	s, err := newShell(PageWork, d, cfg, userPath)
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	updated, _ := s.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return attendedFixture{
		shell: updated.(Shell), overridePath: filepath.Join(dataDir, "pop", "config.override.toml"),
		configPath: userPath, configBody: body,
	}
}

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

func TestDashboardShellHasNoAttendedChooser(t *testing.T) {
	fx := newAttendedFixture(t)
	if err := os.MkdirAll(filepath.Dir(fx.overridePath), 0o755); err != nil {
		t.Fatal(err)
	}
	overrideBody := "work.attended.agents = [\"cursor\"]\n"
	if err := os.WriteFile(fx.overridePath, []byte(overrideBody), 0o644); err != nil {
		t.Fatal(err)
	}
	s := fx.shell
	before := s.View().Content
	s = pressThrough(t, s, tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	if got := s.View().Content; got != before {
		t.Fatalf("alt+a changed the dashboard:\n%s", got)
	}

	s = pressThrough(t, s, tea.KeyPressMsg{Code: 'r', Text: "r"})
	menu := s.View().Content
	if !strings.Contains(menu, "assist · Claude Usual") || strings.Contains(menu, "tab to change") {
		t.Fatalf("Run menu has the wrong attended label:\n%s", menu)
	}
	s = pressThrough(t, s, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := s.View().Content; got != menu {
		t.Fatalf("tab opened or changed a dashboard chooser:\n%s", got)
	}

	override, err := os.ReadFile(fx.overridePath)
	if err != nil || string(override) != overrideBody {
		t.Fatalf("dashboard keypress changed the saved override: err=%v\n%s", err, override)
	}
	stored, err := os.ReadFile(fx.configPath)
	if err != nil || string(stored) != fx.configBody {
		t.Fatalf("dashboard keypress changed source config: err=%v\n%s", err, stored)
	}
}
