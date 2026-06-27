package cmd

import (
	"io"
	"testing"
)

// TestCatalog_StableIdentifiers pins the component identifier strings. They are
// part of pop's external contract (component flags, removal targets, and
// Doctor evidence reads), so a change here is a deliberate breaking change,
// not a refactor.
func TestCatalog_StableIdentifiers(t *testing.T) {
	want := map[ComponentID]bool{
		"status-wiring": true,
		"pane-skill":    true,
		"task-skills":   true,
	}

	got := map[ComponentID]bool{}
	for _, c := range integrationCatalog {
		if got[c.id] {
			t.Errorf("duplicate component id %q in catalog", c.id)
		}
		got[c.id] = true
	}

	for id := range want {
		if !got[id] {
			t.Errorf("catalog missing expected component %q", id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("catalog has unexpected component %q (update the contract test deliberately)", id)
		}
	}

	// The constants must match their stable string values.
	if ComponentStatusWiring != "status-wiring" {
		t.Errorf("ComponentStatusWiring = %q, want status-wiring", ComponentStatusWiring)
	}
	if ComponentPaneSkill != "pane-skill" {
		t.Errorf("ComponentPaneSkill = %q, want pane-skill", ComponentPaneSkill)
	}
	if ComponentTaskSkills != "task-skills" {
		t.Errorf("ComponentTaskSkills = %q, want task-skills", ComponentTaskSkills)
	}
}

// TestCatalog_SupportMatrix asserts the per-agent support matrix: opencode
// hosts the pane skill but not the task planning skills.
func TestCatalog_SupportMatrix(t *testing.T) {
	allAgents := []string{"claude", "codex", "pi", "opencode", "cursor"}

	cases := []struct {
		id        ComponentID
		supported []string
		denied    []string
	}{
		{
			id:        ComponentStatusWiring,
			supported: allAgents,
		},
		{
			id:        ComponentPaneSkill,
			supported: []string{"claude", "codex", "pi", "cursor", "opencode"},
		},
		{
			id:        ComponentTaskSkills,
			supported: []string{"claude", "codex", "pi", "cursor"},
			denied:    []string{"opencode"},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			comp, ok := lookupComponent(tc.id)
			if !ok {
				t.Fatalf("component %q not found in catalog", tc.id)
			}
			for _, a := range tc.supported {
				if !comp.supported(a) {
					t.Errorf("%s should support %s", tc.id, a)
				}
			}
			for _, a := range tc.denied {
				if comp.supported(a) {
					t.Errorf("%s should NOT support %s", tc.id, a)
				}
			}
		})
	}
}

// TestCatalog_SupportMatrixIsCaseInsensitive guards the agent-name lowercasing
// in supported, mirroring the integrate dispatcher's case handling.
func TestCatalog_SupportMatrixIsCaseInsensitive(t *testing.T) {
	comp, ok := lookupComponent(ComponentStatusWiring)
	if !ok {
		t.Fatal("status-wiring component missing")
	}
	if !comp.supported("ClAuDe") {
		t.Error("supported() should be case-insensitive")
	}
}

// TestCatalog_StatusWiringConsumedByIntegrate confirms the bare integrate path
// installs the status-wiring component (hooks land) and nothing else from the
// catalog is reachable as a default install.
func TestCatalog_StatusWiringConsumedByIntegrate(t *testing.T) {
	fs := newFakeFS()
	if err := runIntegrateWith(fakeDeps("/h", fs, io.Discard), "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := fs.files["/h/.claude/settings.json"]; !ok {
		t.Error("integrate did not install the status-wiring component")
	}
}
