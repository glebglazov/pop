package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The chord opens nothing where there is no choice to make: no chooser, no
// overlay, no menu, and no page state moved (ADR-0264 decision 4).
func TestDashboardAttendedChordOpensNothingWithoutAChoice(t *testing.T) {
	m := NewDashboard(nil, &config.Config{}, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	m.width, m.height = 80, 24
	before := m.View().Content

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatal("alt+a must not run a command")
	}
	if got.attendedPick != nil {
		t.Fatal("alt+a must open no chooser over a list with nothing to choose")
	}
	if got.menu != nil || got.filter != nil || got.detail != nil {
		t.Fatal("alt+a must not open any overlay")
	}
	if got.View().Content != before {
		t.Fatalf("alt+a changed the view:\n%s", got.View().Content)
	}
}

// The chord opens the attended chooser over every row, so no Work kind may
// claim it: what it means cannot depend on which row the cursor sits on
// (ADR-0264 decision 3). Movement keys stay reserved beside it.
func TestActionKeySpaceReservesTheAttendedPickChord(t *testing.T) {
	if !actionKeyReserved(ui.AttendedPickChord) {
		t.Fatal("alt+a must be reserved from Work-kind key space")
	}
	for _, key := range []string{"j", "k", "J", "K"} {
		if !actionKeyReserved(key) {
			t.Fatalf("movement key %q must stay reserved", key)
		}
	}
	if actionKeyReserved("a") {
		t.Fatal("plain a must stay available to kinds")
	}
	_ = work.Action{}
}

func TestDashboardAttendedMenuRowNamesEntry(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	m := NewDashboard(nil, cfg, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	items := m.menuItemsFor(DashboardRow{ID: "demo"})
	var assist dashboardMenuItem
	found := false
	for _, item := range items {
		if item.verb == setkind.VerbAssist {
			assist = item
			found = true
			break
		}
	}
	if !found {
		t.Fatal("assist verb missing")
	}
	// The row names the entry its sessions run and trails the mark that says the
	// key opens the Assist pane menu rather than launching (ADR-0263).
	want := "assist · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg)) + " ▸"
	if assist.label != want {
		t.Fatalf("assist label = %q, want %q", assist.label, want)
	}

	mapItems := m.menuItemsFor(DashboardRow{Kind: ref.KindMap, ID: "map"})
	for _, item := range mapItems {
		if attendedActionVerb(item.verb) && !strings.Contains(item.label, "Claude Usual") {
			t.Fatalf("attended verb %s label = %q, want entry name", item.verb, item.label)
		}
	}
}

func TestDashboardPersistentAgentBlockNamesTheConfigDashboardKey(t *testing.T) {
	m := NewDashboard(nil, &config.Config{}, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	m.width, m.height = 100, 30
	view := m.View().Content
	want := tasks.FormatAttendedAgentStatus(tasks.EffectiveAttendedEntry(nil))
	if !strings.Contains(view, want) {
		t.Fatalf("main view missing persistent agent block %q:\n%s", want, view)
	}
	if !strings.Contains(view, ui.ConfigDashboardKeyLabel) {
		t.Fatalf("subheader must name the Config dashboard key:\n%s", view)
	}
	if strings.Contains(view, tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(nil))+" · A-a") {
		t.Fatalf("subheader still names the retired chord:\n%s", view)
	}
}

// An entry whose command names no model renders as the entry alone — the
// subheader and the attended rows never invent one.
func TestDashboardAttendedRendersInventNoModel(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Cursor Usual", Cmd: "cursor"},
		}},
	}}
	m := NewDashboard(nil, cfg, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	if got := m.attendedAgentStatusLine(); got != "agent Cursor Usual · "+ui.ConfigDashboardKeyLabel {
		t.Fatalf("subheader = %q", got)
	}
	if got := m.enrichAttendedActionLabel(setkind.VerbAssist, "assist"); got != "assist · Cursor Usual" {
		t.Fatalf("action row = %q", got)
	}
}

// The renders resolve through the merged config, so an override written by the
// Config dashboard is what the subheader and the attended rows report.
func TestDashboardAttendedRendersFollowTheOverrideLayer(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config", "config.toml")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(userPath, `
[work.attended]
agents = [{ display_name = "Claude Usual", cmd = "claude --model opus" }]
`)
	write(filepath.Join(dataDir, "pop", "config.override.toml"), `
[work.attended]
agents = [{ display_name = "Cursor", cmd = "cursor" }]
`)
	cfgDeps := &config.Deps{FS: &deps.MockFileSystem{
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
	cfg, err := config.LoadWith(cfgDeps, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}

	m := NewDashboard(nil, cfg, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	m.width, m.height = 100, 30
	if got := m.attendedAgentStatusLine(); !strings.Contains(got, "Cursor") {
		t.Fatalf("subheader = %q, want the override's entry", got)
	}
	if got := m.enrichAttendedActionLabel(setkind.VerbAssist, "assist"); got != "assist · Cursor" {
		t.Fatalf("action row = %q, want the override's entry", got)
	}
	if !strings.Contains(m.View().Content, "Cursor") {
		t.Fatalf("main view missing the override's entry:\n%s", m.View().Content)
	}
}

// twoEntryDashboard is a page over a config holding a choice: two usable
// attended entries, which is what makes the chord live (ADR-0264 decision 4).
func twoEntryDashboard(t *testing.T) QueueDashboard {
	t.Helper()
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
			{DisplayName: "Cursor", Cmd: "cursor"},
		}},
	}}
	m := NewDashboard(nil, cfg, DashboardSnapshot{
		Containers: []DashboardRow{
			{ID: "demo", CursorKey: "demo"},
			{ID: "other", CursorKey: "other"},
		},
	})
	m.width, m.height = 120, 30
	return m
}

func pressKey(t *testing.T, m QueueDashboard, msg tea.KeyPressMsg) QueueDashboard {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(QueueDashboard)
}

func altA() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt} }

// The affordance is on the row that names the entry, and only while a choice
// exists — a list of one has nothing to open.
func TestDashboardAttendedRowAdvertisesTheChordOnlyWithAChoice(t *testing.T) {
	m := twoEntryDashboard(t)
	label := m.enrichAttendedActionLabel(setkind.VerbAssist, "assist")
	if !strings.Contains(label, ui.AttendedPickChordLabel+" to change") {
		t.Fatalf("action row = %q, want the chord advertised", label)
	}

	one := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	m.cfg = one
	if label := m.enrichAttendedActionLabel(setkind.VerbAssist, "assist"); strings.Contains(label, ui.AttendedPickChordLabel) {
		t.Fatalf("action row = %q, want no chord over a list of one", label)
	}
}

// While the chooser is open the page's own keys are suspended: the movement, the
// menu openers and the marking keys all belong to the list being chosen from.
func TestDashboardAttendedChooserSuspendsThePagesKeys(t *testing.T) {
	m := pressKey(t, twoEntryDashboard(t), altA())
	if m.attendedPick == nil {
		t.Fatal("alt+a opened no chooser over two usable entries")
	}
	if !strings.Contains(m.View().Content, "Attended agent") {
		t.Fatalf("chooser not rendered over the rows:\n%s", m.View().Content)
	}
	if m.ViewToggleAllowed() {
		t.Fatal("the chooser must own the keyboard, page toggle included")
	}

	cursor := m.ListCursor()
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.menu != nil {
		t.Fatal("`r` opened the run menu through the chooser")
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.selection.Active() {
		t.Fatal("tab marked a row through the chooser")
	}
	if m.ListCursor() != cursor {
		t.Fatalf("the row cursor moved to %d under the chooser", m.ListCursor())
	}
	if m.attendedPick == nil {
		t.Fatal("the chooser closed on a key that is not its own")
	}
}

// Esc leaves the agent unchanged, and the two keys that mark rows keep working
// on either side of the chooser (ADR-0254 decision 4).
func TestDashboardMarkingSurvivesTheChooser(t *testing.T) {
	m := twoEntryDashboard(t)
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.selection.Active() {
		t.Fatal("tab marked nothing before the chooser")
	}

	m = pressKey(t, m, altA())
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.attendedPick != nil {
		t.Fatal("esc left the chooser open")
	}
	if m.actionErr != nil {
		t.Fatalf("esc wrote something: %v", m.actionErr)
	}

	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.selection.Active() {
		t.Fatal("shift+tab cleared nothing after the chooser")
	}
}

// The assist verb is unchanged by the chooser existing: it opens the assist-pane
// menu on the entry in force, never the chooser (ADR-0264 decision 3).
func TestDashboardAssistVerbOpensNoChooser(t *testing.T) {
	m := twoEntryDashboard(t)
	updated, _ := m.dispatchVerb(setkind.VerbAssist, m.snap.Containers[0])
	got := updated.(QueueDashboard)
	if got.attendedPick != nil {
		t.Fatal("the assist verb opened the chooser")
	}
	if got.menu == nil || got.menu.assist == nil {
		t.Fatal("the assist verb opened no assist-pane menu")
	}
}

// A task-set assist launch pins the spawned session to the entry the row named,
// as `pop tasks assist --agent` (ADR-0264 decision 8): the pane resolves config
// for itself, so without the argument the row's promise would be advisory.
func TestDashboardAssistLaunchNamesTheRowsAttendedEntry(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-agent-arg", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath, row.ProjectPath = repo, repo
	cfg.Work = &config.WorkConfig{Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
		{DisplayName: "Picked", Cmd: "claude --model opus"},
		{Cmd: "codex"},
	}}}
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})

	m.launchAssist(row, tmuxmod.FirstPaneSlot)()

	command, ok := extractAssistSpawnCommand(rt)
	if !ok {
		t.Fatalf("no assist command was sent; commands=%v", rt.Commands)
	}
	if !strings.Contains(command, "--agent 'claude --model opus'") {
		t.Fatalf("assist command = %q, want it pinned to the entry the row names", command)
	}
}
