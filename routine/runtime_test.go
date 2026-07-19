package routine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestManifestRoundTripsAgentsAndEffort confirms the optional runtime fields
// survive a write/read cycle and that absent fields read back empty.
func TestManifestRoundTripsAgentsAndEffort(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	// Absent fields behave as today.
	m := readManifest(t, d, "daily")
	if len(m.Agents) != 0 || m.Effort != "" {
		t.Fatalf("fresh manifest has agents=%v effort=%q, want empty", m.Agents, m.Effort)
	}

	if _, err := ConfigureRuntimeWith(d, "daily", []string{"codex", "claude"}, true, "heavy", true); err != nil {
		t.Fatal(err)
	}
	m = readManifest(t, d, "daily")
	if strings.Join(m.Agents, ",") != "codex,claude" {
		t.Fatalf("Agents = %v, want [codex claude]", m.Agents)
	}
	if m.Effort != "heavy" {
		t.Fatalf("Effort = %q, want heavy", m.Effort)
	}
}

// TestResolveRoutineRunSpecsOrderAndEffort covers the fire-time resolution
// order (manifest list wins) and that effort pins the model on each spec.
func TestResolveRoutineRunSpecsOrderAndEffort(t *testing.T) {
	cfg := &config.Config{
		Routines: &config.RoutinesConfig{Agents: []string{"cursor"}},
		Task: &config.TasksConfig{
			Implement: &config.ImplementConfig{Agents: []string{"pi"}},
		},
	}

	// Manifest list wins over both config lists; effort pins the model.
	specs := resolveRoutineRunSpecs(cfg, Manifest{Agents: []string{"claude", "codex"}, Effort: "heavy"})
	if len(specs) != 2 {
		t.Fatalf("specs = %#v, want 2 entries", specs)
	}
	if !strings.HasPrefix(specs[0], "claude") || !strings.Contains(specs[0], "opus") {
		t.Fatalf("specs[0] = %q, want claude pinned to heavy model", specs[0])
	}
	if !strings.HasPrefix(specs[1], "codex") {
		t.Fatalf("specs[1] = %q, want codex", specs[1])
	}

	// No manifest list ⇒ [routines].agents head; empty effort ⇒ standard.
	specs = resolveRoutineRunSpecs(cfg, Manifest{})
	if len(specs) != 1 || !strings.HasPrefix(specs[0], "cursor") {
		t.Fatalf("specs = %#v, want cursor head", specs)
	}
}

func TestUpdateRuntimeValidatesAndPausesChanged(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	// Arm it so the changed-pause is observable.
	if _, err := ResumeWith(d, "daily"); err != nil {
		t.Fatal(err)
	}

	// Bad preset is rejected before writing.
	if _, err := UpdateRuntimeWith(d, "daily", []string{"nope"}, true, "", false); err == nil {
		t.Fatal("expected error for unknown preset")
	}
	if m := readManifest(t, d, "daily"); m.Paused {
		t.Fatal("rejected update must not have paused the routine")
	}

	// Bad effort is rejected before writing.
	if _, err := UpdateRuntimeWith(d, "daily", nil, false, "extreme", true); err == nil {
		t.Fatal("expected error for invalid effort")
	}

	// Valid edit writes and pauses with reason changed.
	res, err := UpdateRuntimeWith(d, "daily", []string{"claude"}, true, "light", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Paused {
		t.Fatal("result should report paused")
	}
	m := readManifest(t, d, "daily")
	if !m.Paused || m.PauseReason != PauseReasonChanged {
		t.Fatalf("manifest paused=%v reason=%q, want true/changed", m.Paused, m.PauseReason)
	}
	if strings.Join(m.Agents, ",") != "claude" || m.Effort != "light" {
		t.Fatalf("manifest agents=%v effort=%q, want [claude]/light", m.Agents, m.Effort)
	}
}

// TestConfigureRuntimeLeavesCreatedReason confirms the add-time write does not
// flip the created-on-scaffold pause reason to changed.
func TestConfigureRuntimeLeavesCreatedReason(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "daily", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := ConfigureRuntimeWith(d, "daily", []string{"claude"}, true, "standard", true); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, d, "daily")
	if m.PauseReason != PauseReasonCreated {
		t.Fatalf("PauseReason = %q, want %q (add-time write must not mark changed)", m.PauseReason, PauseReasonCreated)
	}
}
