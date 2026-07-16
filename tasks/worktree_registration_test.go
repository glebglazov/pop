package tasks

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestRegisterIgnoresManagedWorktreeManifestKey verifies a managed worktree
// directive in the manifest is no longer read as a registration seed (ADR-0115):
// the set registers with a nil intent.
func TestRegisterIgnoresManagedWorktreeManifestKey(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "managed-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"worktree": map[string]any{"managed": true}})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status == StatusMalformed {
		t.Fatalf("rows = %#v, want a non-MALFORMED registration", result.Rows)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[result.DefinitionPath]
	if entry == nil || len(entry.TaskSets) != 1 {
		t.Fatalf("registration = %#v", entry)
	}
	if got := entry.TaskSets[0].WorktreeIntent; got != nil {
		t.Fatalf("worktree intent = %#v, want nil (key ignored)", got)
	}
}

// TestRegisterIgnoresNamedWorktreeManifestKey verifies a named worktree directive
// in the manifest is no longer read as a registration seed (ADR-0115).
func TestRegisterIgnoresNamedWorktreeManifestKey(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "named-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"worktree": map[string]any{"name": "feature-wt"}})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Tasks[result.DefinitionPath].TaskSets[0].WorktreeIntent; got != nil {
		t.Fatalf("worktree intent = %#v, want nil (key ignored)", got)
	}
}

// TestDiscoverySeedsNoWorktreeWhenAbsent verifies a set with no directive
// persists a nil (none) intent and registers as today.
func TestDiscoverySeedsNoWorktreeWhenAbsent(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "plain-wt-set", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "plain-wt-set" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Tasks[result.DefinitionPath].TaskSets[0].WorktreeIntent; got != nil {
		t.Fatalf("worktree intent = %#v, want nil", got)
	}
}

// TestWorktreeIntentNoResyncAfterManifestEdit verifies editing the manifest's
// worktree key after registration leaves the persisted intent unchanged: the
// directive is a one-time registration seed.
func TestWorktreeIntentNoResyncAfterManifestEdit(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "wt-edit-set", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	// Add a worktree directive after first registration.
	taskDir := filepath.Join(root, "wt-edit-set")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"worktree": map[string]any{"managed": true}})

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 0 {
		t.Fatalf("unexpected re-registration: %v", result.NewRegistrations)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Tasks[result.DefinitionPath].TaskSets[0].WorktreeIntent; got != nil {
		t.Fatalf("worktree intent = %#v, want nil (seed not re-read after edit)", got)
	}
}

// TestWorktreeIntentRoundTripsAcrossLoadSave verifies the seeded intent survives
// a save/load cycle through the store for all three states.
func TestWorktreeIntentRoundTripsAcrossLoadSave(t *testing.T) {
	d := DefaultDeps()
	defPath := filepath.Join(t.TempDir(), "rt", "tasks")
	statePath := StatePathFor(defPath)

	want := []RegisteredTaskSet{
		{ID: "managed", WorktreeIntent: &WorktreeDirective{Managed: true}},
		{ID: "named", WorktreeIntent: &WorktreeDirective{Name: "adopt-me"}},
		{ID: "none"},
	}
	if err := UpdateGlobalStateWith(d, statePath, func(s *GlobalState) error {
		s.Entry(defPath).TaskSets = want
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Tasks[defPath].TaskSets; !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip = %#v, want %#v", got, want)
	}
}
