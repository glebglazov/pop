package routine

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// openAgentEffortModal builds the dashboard and drives the action menu into the
// agent/effort modal (a opens the menu, g selects the verb).
func openAgentEffortModal(t *testing.T, d *Deps) RoutineDashboard {
	t.Helper()
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	updated, cmd := updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if cmd != nil {
		t.Fatal("opening the agent/effort modal should not schedule a command")
	}
	m = updated.(RoutineDashboard)
	if m.agentEffort == nil {
		t.Fatal("g in menu should open the agent/effort modal")
	}
	if m.menu != nil {
		t.Fatal("opening the agent/effort modal should close the action menu")
	}
	return m
}

// typeChars feeds each rune of s to the model as a printable key press, so the
// characters land in the active modal field.
func typeChars(t *testing.T, m RoutineDashboard, s string) RoutineDashboard {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(RoutineDashboard)
	}
	return m
}

// clearAgentEffortField backspaces the active modal field down to empty.
func clearAgentEffortField(t *testing.T, m RoutineDashboard) RoutineDashboard {
	t.Helper()
	for m.agentEffort != nil && len(*m.agentEffort.current()) > 0 {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(RoutineDashboard)
	}
	return m
}

func TestRoutineDashboardAgentEffortPrefill(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigureRuntimeWith(d, "alpha", []string{"claude", "codex"}, true, "heavy", true); err != nil {
		t.Fatal(err)
	}
	m := openAgentEffortModal(t, d)
	if m.agentEffort.agents != "claude, codex" {
		t.Fatalf("agents pre-fill = %q, want %q", m.agentEffort.agents, "claude, codex")
	}
	if m.agentEffort.effort != "heavy" {
		t.Fatalf("effort pre-fill = %q, want %q", m.agentEffort.effort, "heavy")
	}
	view := m.View().Content
	if !strings.Contains(view, "agent / effort") ||
		!strings.Contains(view, "agents: claude, codex") ||
		!strings.Contains(view, "effort: heavy") {
		t.Fatalf("modal view missing pre-fill:\n%s", view)
	}
}

func TestRoutineDashboardAgentEffortValidWrite(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Arm it so the changed-pause is observable.
	if _, err := ResumeWith(d, "alpha"); err != nil {
		t.Fatal(err)
	}
	m := openAgentEffortModal(t, d)

	// Type the agents field, tab to the effort field, type the tier.
	m = typeChars(t, m, "claude")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(RoutineDashboard)
	if m.agentEffort.field != 1 {
		t.Fatalf("tab should focus the effort field, got field %d", m.agentEffort.field)
	}
	m = typeChars(t, m, "light")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.agentEffort != nil {
		t.Fatal("valid enter should close the modal")
	}
	if cmd == nil {
		t.Fatal("valid enter should schedule a reload")
	}
	if !strings.Contains(m.statusMsg, "alpha") {
		t.Fatalf("status message = %q", m.statusMsg)
	}
	r, err := loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Manifest.Agents, ",") != "claude" || r.Manifest.Effort != "light" {
		t.Fatalf("manifest agents=%v effort=%q, want [claude]/light", r.Manifest.Agents, r.Manifest.Effort)
	}
	// A runtime edit is run-affecting: it pauses with reason changed.
	if !r.Manifest.Paused || r.Manifest.PauseReason != PauseReasonChanged {
		t.Fatalf("manifest paused=%v reason=%q, want true/changed", r.Manifest.Paused, r.Manifest.PauseReason)
	}
}

func TestRoutineDashboardAgentEffortClear(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigureRuntimeWith(d, "alpha", []string{"claude"}, true, "heavy", true); err != nil {
		t.Fatal(err)
	}
	m := openAgentEffortModal(t, d)

	// Clear both fields, then submit to reset back to unset.
	m = clearAgentEffortField(t, m)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(RoutineDashboard)
	m = clearAgentEffortField(t, m)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.agentEffort != nil {
		t.Fatal("clearing submission should close the modal")
	}
	if cmd == nil {
		t.Fatal("clearing submission should schedule a reload")
	}
	r, err := loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Manifest.Agents) != 0 || r.Manifest.Effort != "" {
		t.Fatalf("manifest agents=%v effort=%q, want cleared", r.Manifest.Agents, r.Manifest.Effort)
	}
}

func TestRoutineDashboardAgentEffortInvalidReedit(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigureRuntimeWith(d, "alpha", []string{"claude"}, true, "standard", true); err != nil {
		t.Fatal(err)
	}
	// Arm it so a rejected write leaving it unpaused is observable.
	if _, err := ResumeWith(d, "alpha"); err != nil {
		t.Fatal(err)
	}
	m := openAgentEffortModal(t, d)

	// Replace the agents field with an unknown preset.
	m = clearAgentEffortField(t, m)
	m = typeChars(t, m, "nope")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.agentEffort == nil {
		t.Fatal("invalid enter should keep the modal open")
	}
	if cmd != nil {
		t.Fatal("invalid enter should not schedule a reload")
	}
	if m.agentEffort.err == nil {
		t.Fatal("invalid enter should record a validation error")
	}
	if !strings.Contains(m.View().Content, "error:") {
		t.Fatalf("modal view should show the inline error:\n%s", m.View().Content)
	}
	// Nothing persisted: agents and pause state unchanged.
	r, err := loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Manifest.Agents, ",") != "claude" {
		t.Fatalf("manifest agents after invalid attempt = %v, want unchanged [claude]", r.Manifest.Agents)
	}
	if r.Manifest.Paused {
		t.Fatal("rejected write must not pause the routine")
	}

	// Correcting the field and retrying persists.
	m = clearAgentEffortField(t, m)
	m = typeChars(t, m, "codex")
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.agentEffort != nil {
		t.Fatal("corrected enter should close the modal")
	}
	if cmd == nil {
		t.Fatal("corrected enter should schedule a reload")
	}
	r, err = loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Manifest.Agents, ",") != "codex" {
		t.Fatalf("manifest agents after correction = %v, want [codex]", r.Manifest.Agents)
	}
}
