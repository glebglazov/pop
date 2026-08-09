package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

func TestDashboardAgentOverridePickerOpensAndPromotes(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
			{DisplayName: "Cursor", Cmd: "cursor"},
		}},
	}}
	m := NewDashboard(nil, cfg, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	m.width, m.height = 80, 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	got := updated.(QueueDashboard)
	if got.agentPick == nil {
		t.Fatal("alt+a should open the agent override picker")
	}
	view := got.View().Content
	for _, want := range []string{"implement", "verify", "routine", "attended", "Agent override"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker view missing %q:\n%s", want, view)
		}
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	got = updated.(QueueDashboard)
	updated, _ = got.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	got = updated.(QueueDashboard)
	if got.agentPick != nil {
		t.Fatal("pick should close the overlay")
	}
	if cmd := got.agentOverrides().AttendedCmd(); cmd != "cursor" {
		t.Fatalf("attended override = %q, want cursor", cmd)
	}
	if !strings.Contains(got.agentOverrideStatusLine(), "Cursor") {
		t.Fatalf("status line = %q, want Cursor in force", got.agentOverrideStatusLine())
	}
}

func TestDashboardAgentOverrideKeyInertInsideMenu(t *testing.T) {
	m := NewDashboard(nil, &config.Config{}, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a should open action menu")
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	got = updated.(QueueDashboard)
	if got.agentPick != nil {
		t.Fatal("alt+a must be inert while the action menu is open")
	}
	if got.menu == nil {
		t.Fatal("action menu should stay open")
	}
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
	want := "assist · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg, nil))
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

func TestDashboardAgentOverrideNotWrittenAnywhere(t *testing.T) {
	m := NewDashboard(nil, &config.Config{}, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	m.ensureAgentOverrides().Promote("attended", "cursor")
	// A fresh bag (new process) is empty — nothing persisted.
	fresh := tasks.NewAgentOverrides()
	if fresh.AttendedCmd() != "" {
		t.Fatal("override must not outlive the process bag")
	}
	_ = m
}

func TestDashboardPersistentAgentBlock(t *testing.T) {
	m := NewDashboard(nil, &config.Config{}, DashboardSnapshot{
		Containers: []DashboardRow{{ID: "demo", CursorKey: "demo"}},
	})
	m.width, m.height = 100, 30
	view := m.View().Content
	want := tasks.FormatAgentOverrideStatus(tasks.EffectiveAttendedEntry(nil, nil))
	if !strings.Contains(view, want) {
		t.Fatalf("main view missing persistent agent block %q:\n%s", want, view)
	}
	if !strings.Contains(view, tasks.AgentOverrideKeyLabel) {
		t.Fatalf("main view missing override key label:\n%s", view)
	}
}

func TestActionKeyReservedIncludesOverrideChord(t *testing.T) {
	if !actionKeyReserved(tasks.AgentOverrideKey) {
		t.Fatal("alt+a must be reserved from kind Action.Key space")
	}
	// Plain "a" remains claimable (auto-drain, fan-out-here).
	if actionKeyReserved("a") {
		t.Fatal("plain a must stay available to kinds")
	}
	_ = work.Action{}
}
