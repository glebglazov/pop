package integrate

import (
	"testing"
)

func TestRegistry_OneProfilePerSupportedAgent(t *testing.T) {
	t.Parallel()
	want := []string{"claude", "codex", "pi", "opencode", "cursor", "kimi"}
	if len(profiles) != len(want) {
		t.Fatalf("profiles len = %d, want %d", len(profiles), len(want))
	}
	if len(Agents) != len(want) {
		t.Fatalf("Agents len = %d, want %d", len(Agents), len(want))
	}
	seen := map[string]bool{}
	for i, p := range profiles {
		if p.Name != want[i] {
			t.Errorf("profiles[%d].Name = %q, want %q", i, p.Name, want[i])
		}
		if Agents[i] != want[i] {
			t.Errorf("Agents[%d] = %q, want %q", i, Agents[i], want[i])
		}
		if seen[p.Name] {
			t.Errorf("duplicate profile for %q", p.Name)
		}
		seen[p.Name] = true
		if p.InstallStatusWiring == nil || p.RemoveStatusWiring == nil || p.DetectStatusWiring == nil {
			t.Errorf("%s: missing status-wiring func field", p.Name)
		}
		if p.SkillDir == nil {
			t.Errorf("%s: missing SkillDir", p.Name)
		}
		if p.LegacyArtifacts == nil {
			t.Errorf("%s: missing LegacyArtifacts", p.Name)
		}
		got, ok := LookupProfile(p.Name)
		if !ok || got.Name != p.Name {
			t.Errorf("LookupProfile(%q) failed", p.Name)
		}
	}
}

func TestRegistry_LookupProfileCaseInsensitive(t *testing.T) {
	t.Parallel()
	p, ok := LookupProfile("ClAuDe")
	if !ok || p.Name != "claude" {
		t.Fatalf("LookupProfile(ClAuDe) = (%q, %v), want claude", p.Name, ok)
	}
}
