package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/ui"
)

// The two picker hosts of the Config dashboard (ADR-0202 decisions 10 and 11).
// Their key suspension is proven in ui, over the picker model itself; what
// belongs here is the wiring and what the commands tell a human.

func TestPickerHelpDocumentsTheConfigDashboard(t *testing.T) {
	t.Parallel()
	for name, long := range map[string]string{
		"project dashboard":  projectDashboardCmd.Long,
		"worktree dashboard": worktreeDashboardCmd.Long,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(long, ui.ConfigDashboardKeyLabel) && !strings.Contains(long, "alt-c") {
				t.Errorf("%s help names no chord for the Config dashboard:\n%s", name, long)
			}
			// The picker's own popup is 60%; the Config dashboard wants the roomier
			// one, and a human reading either binding needs both.
			if !strings.Contains(long, "display-popup") {
				t.Fatalf("%s help lost its tmux binding:\n%s", name, long)
			}
			if !strings.Contains(long, "pop config dashboard") || !strings.Contains(long, "-w 80% -h 80%") {
				t.Errorf("%s help does not carry the Config dashboard's own popup geometry:\n%s", name, long)
			}
		})
	}
}

// The chord opens the component from the picker each command actually builds.
// The key suspension that follows is proven in ui; what is proven here is that
// these two pickers are hosts at all.
func TestBothPickersHostTheConfigDashboard(t *testing.T) {
	t.Run("worktree picker", func(t *testing.T) {
		setCmdLayerDeps(t, newTestCmdDeps(t, "", t.TempDir(), t.TempDir()))
		opts := worktreePickerOptions(nil, "alt", -1, nil, nil, false)
		assertChordOpensConfigDashboard(t, opts)
	})

	t.Run("project picker", func(t *testing.T) {
		d := testProjectDeps(t)
		var opts []ui.PickerOption
		d.RunPicker = func(items []ui.Item, o ...ui.PickerOption) (ui.Result, error) {
			opts = o
			return ui.Result{Action: ui.ActionCancel}, nil
		}
		if err := RunProject(d); err != nil {
			t.Fatalf("RunProject: %v", err)
		}
		assertChordOpensConfigDashboard(t, opts)
	})
}

func assertChordOpensConfigDashboard(t *testing.T, opts []ui.PickerOption) {
	t.Helper()
	p := ui.NewPicker([]ui.Item{{Name: "one", Path: "/one"}}, opts...)
	p.Init()
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	p.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !p.ConfigModalOpen() {
		t.Error("alt+c opened no Config dashboard on this picker")
	}
}

// The opener a picker is handed resolves the real override layer against the
// config path the command loaded from, and hands back a component with rows.
func TestConfigDashboardOpenerResolvesTheLayer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[work.implement]\nagents = [\"claude\"]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)

	previous := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = previous })

	m := configDashboardOpener()()
	if m == nil {
		t.Fatal("opener built no component")
	}
	row, ok := m.Selected()
	if !ok {
		t.Fatalf("component has no rows:\n%s", m.ViewContent())
	}
	if !strings.HasPrefix(row.Key, "work.") {
		t.Errorf("first row = %q, want an override-exposed work key", row.Key)
	}
}
