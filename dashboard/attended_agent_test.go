package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The retired chord opens nothing on any dashboard page (ADR-0202 decision 5):
// no overlay, no menu, and no page state moved.
func TestDashboardRetiredOverrideChordOpensNothing(t *testing.T) {
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
	if got.menu != nil || got.filter != nil || got.detail != nil {
		t.Fatal("alt+a must not open any overlay")
	}
	if got.View().Content != before {
		t.Fatalf("alt+a changed the view:\n%s", got.View().Content)
	}
}

// The chord's carve-out from kind-supplied key space goes with the picker, so a
// kind may claim it. Movement keys stay reserved.
func TestActionKeySpaceReleasesTheRetiredChord(t *testing.T) {
	if actionKeyReserved("alt+a") {
		t.Fatal("alt+a must be claimable by a Work kind again")
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
	want := "assist · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg))
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
