package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/ui"
)

// overrideEditFixture is the real thing behind the dashboard: a hand-authored
// config.toml and a pop data dir on disk, reached through the same writer `pop
// config dashboard` hands the component.
type overrideEditFixture struct {
	deps     *config.Deps
	userPath string
	writer   configOverrideWriter
	// seeds is what pop put in front of the human on each editor open.
	seeds []string
}

func newOverrideEditFixture(t *testing.T, configTOML string) *overrideEditFixture {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config", "config.toml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(userPath, []byte(configTOML), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	d := &config.Deps{FS: &deps.MockFileSystem{
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
	}}
	return &overrideEditFixture{
		deps:     d,
		userPath: userPath,
		writer:   configOverrideWriter{deps: d, configPath: userPath},
	}
}

// dashboard opens the component exactly as runConfigDashboard does, with a
// scripted editor standing in for the human's $EDITOR.
func (f *overrideEditFixture) dashboard(t *testing.T, replies ...string) *ui.ConfigDashboard {
	t.Helper()
	rows, err := f.writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	editor := func(path string, done tea.ExecCallback) tea.Cmd {
		seed, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("scripted editor: %v", err)
		}
		f.seeds = append(f.seeds, string(seed))
		reply := ""
		if len(f.seeds) <= len(replies) {
			reply = replies[len(f.seeds)-1]
		}
		if err := os.WriteFile(path, []byte(reply), 0o644); err != nil {
			t.Errorf("scripted editor: %v", err)
		}
		return func() tea.Msg { return done(nil) }
	}
	m := ui.NewConfigDashboard(rows, ui.ConfigDashboardOpts{Writer: f.writer, Editor: editor})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

// press sends one key and drains the commands it produces, the way the tea
// runtime would.
func pressConfigDashboard(m *ui.ConfigDashboard, msg tea.KeyPressMsg) {
	_, cmd := m.Update(msg)
	for cmd != nil {
		next := cmd()
		if next == nil {
			return
		}
		_, cmd = m.Update(next)
	}
}

// selectKey moves the highlight onto one key, failing when the registry no
// longer lists it.
func selectKey(t *testing.T, m *ui.ConfigDashboard, key string) {
	t.Helper()
	for i := 0; i < len(config.OverrideKeys()); i++ {
		if row, ok := m.Selected(); ok && row.Key == key {
			return
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	t.Fatalf("no row for %s in the dashboard", key)
}

const overrideEditConfigTOML = `
projects = [{ path = "/main" }]

[work.implement]
agents = ["claude"]

[work.verify]
agents = ["claude --verify"]
`

// TestConfigDashboardEditReachesTheMergedConfig is the slice end to end: a human
// edits a Work agent group's list in the dashboard, and the config every drain
// loads reports the new list.
func TestConfigDashboardEditReachesTheMergedConfig(t *testing.T) {
	f := newOverrideEditFixture(t, overrideEditConfigTOML)
	m := f.dashboard(t, `work.verify.agents = ["codex --model gpt", "claude"]`)
	selectKey(t, m, "work.verify.agents")

	pressConfigDashboard(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	cfg, err := config.LoadWith(f.deps, f.userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	if got := cfg.VerifyAgents(); !reflect.DeepEqual(got, []string{"codex --model gpt", "claude"}) {
		t.Fatalf("VerifyAgents() = %#v, want the list edited in the dashboard", got)
	}
	if len(cfg.Findings) != 0 {
		t.Errorf("the write produced config findings: %+v", cfg.Findings)
	}

	row, _ := m.Selected()
	if !row.Overridden || row.Preview.Provenance != "override" {
		t.Errorf("row = %+v, want the override visible the moment the editor closed", row)
	}
}

// TestConfigDashboardEmptyListDisablesTheFallthrough is the demo case ADR-0202
// decision 6 keeps apart from a removal: stated emptiness is a value, and the
// preview says so.
func TestConfigDashboardEmptyListDisablesTheFallthrough(t *testing.T) {
	f := newOverrideEditFixture(t, `
projects = [{ path = "/main" }]

[work.implement]
agents = ["claude"]
`)
	m := f.dashboard(t, "work.routine.agents = []")
	selectKey(t, m, "work.routine.agents")

	pressConfigDashboard(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	row, _ := m.Selected()
	if !row.Overridden {
		t.Fatalf("row = %+v, want an override in force", row)
	}
	if !strings.Contains(row.Preview.Note, "fallthrough disabled") {
		t.Errorf("note = %q, want the disabled fallthrough said in words", row.Preview.Note)
	}
}

// TestConfigDashboardRefusedValueNeverReachesTheFile is decision 8 through the
// real validator: the editor re-opens with the finding a load would report, and
// the override file is never written.
func TestConfigDashboardRefusedValueNeverReachesTheFile(t *testing.T) {
	f := newOverrideEditFixture(t, overrideEditConfigTOML)
	m := f.dashboard(t,
		`work.verify.agents = [{ display_name = "Claude" }]`, // no cmd: a finding
		"work.verify.agents = [",                             // still broken: not TOML
		"",                                                   // give up
	)
	selectKey(t, m, "work.verify.agents")

	pressConfigDashboard(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(f.seeds) != 3 {
		t.Fatalf("editor opened %d times, want one pass per refusal plus the human giving up", len(f.seeds))
	}
	if !strings.Contains(f.seeds[1], "agents entry 1 is malformed") {
		t.Errorf("the re-opened buffer does not carry the finding a load would report:\n%s", f.seeds[1])
	}
	if !strings.Contains(f.seeds[2], "not valid TOML") {
		t.Errorf("the second refusal does not say what is wrong:\n%s", f.seeds[2])
	}
	if _, err := os.Stat(config.DefaultOverrideConfigPathWith(f.deps)); !os.IsNotExist(err) {
		data, _ := os.ReadFile(config.DefaultOverrideConfigPathWith(f.deps))
		t.Fatalf("override file written from a refused value:\n%s", data)
	}
	cfg, err := config.LoadWith(f.deps, f.userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	if got := cfg.VerifyAgents(); !reflect.DeepEqual(got, []string{"claude --verify"}) {
		t.Errorf("VerifyAgents() = %#v, want the hand-authored list untouched", got)
	}
}

// TestConfigDashboardCopyThenRemoveRoundTrips walks the two editor-free actions
// over the real layer: copy makes the source the override, remove puts things
// back exactly as they were.
func TestConfigDashboardCopyThenRemoveRoundTrips(t *testing.T) {
	f := newOverrideEditFixture(t, overrideEditConfigTOML)
	m := f.dashboard(t)
	selectKey(t, m, "work.implement.agents")

	pressConfigDashboard(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})

	row, _ := m.Selected()
	if !row.Overridden || !strings.Contains(row.Preview.ValueTOML, `"claude"`) {
		t.Fatalf("row = %+v, want the source value copied down as an override", row)
	}
	cfg, err := config.LoadWith(f.deps, f.userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	if got := cfg.ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Fatalf("ImplementAgents() = %#v, want the copied list", got)
	}

	pressConfigDashboard(m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})

	row, _ = m.Selected()
	if row.Overridden || row.Preview.Provenance != "config.toml" {
		t.Errorf("row = %+v, want the hand-authored source back", row)
	}
	if _, err := os.Stat(config.DefaultOverrideConfigPathWith(f.deps)); !os.IsNotExist(err) {
		t.Error("the override file outlived its last key")
	}
}
