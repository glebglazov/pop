package config

import (
	"strings"
	"testing"
)

// The Config dashboard's repository scope (ADR-0212 decisions 3 and 6): the rows
// that answer for the repository a human is standing in, and the writes they
// make. They are what makes the dashboard the place a Preferred workbench is
// chosen now that the chord that opened a picker for it is gone.

// repoView picks one row of the repository scope out of a resolution over the
// whole block, so every case exercises the entry point the dashboard calls.
func (f *overrideScopeFixture) repoView(t *testing.T, checkout, key string) OverrideKeyView {
	t.Helper()
	views, err := RepoOverrideKeyViews(f.d, f.userPath, checkout)
	if err != nil {
		t.Fatalf("RepoOverrideKeyViews: %v", err)
	}
	for _, view := range views {
		if view.Key == key {
			return view
		}
	}
	t.Fatalf("no %s row in %d rows", key, len(views))
	return OverrideKeyView{}
}

// The preferred workbench is settable here: the dashboard offers it as a row,
// storing the edited buffer states it for the repository, and resolution reads
// the stated value back at every worktree of it.
func TestDashboardSetsThePreferredWorkbench(t *testing.T) {
	f := newOverrideScopeFixture(t)
	cfg := namedWorkbenches("solo")

	if name, _ := cfg.ResolvePreferredWorkbench(f.d, f.main); name != "" {
		t.Fatalf("preferred workbench starts at %q, want none", name)
	}

	key := RepoScopeKey("preferred_workbench")
	before := f.repoView(t, f.main, key)
	if before.Overridden {
		t.Fatal("row starts overridden")
	}

	problem, err := StoreRepoOverrideBufferWith(f.d, f.main, key, key+` = "solo"`+"\n")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if problem != "" {
		t.Fatalf("store reported %q", problem)
	}

	for _, checkout := range []string{f.main, f.feature} {
		if name, _ := cfg.ResolvePreferredWorkbench(f.d, checkout); name != "solo" {
			t.Errorf("preferred workbench at %s = %q, want solo", checkout, name)
		}
	}

	after := f.repoView(t, f.main, key)
	if !after.Overridden {
		t.Error("row is not marked as overridden after the write")
	}
	if after.EffectiveTOML != key+` = "solo"` {
		t.Errorf("effective TOML = %q", after.EffectiveTOML)
	}
	if after.Provenance() != string(OverrideLayerOverride) {
		t.Errorf("provenance = %q, want the override layer", after.Provenance())
	}
}

// A row reports which layer produced the value in force, in the same order
// resolution walks: the override over the checkout-keyed declaration over the
// committed file.
func TestRepoScopeRowNamesTheLayerInForce(t *testing.T) {
	f := newOverrideScopeFixture(t)
	key := RepoScopeKey("preferred_workbench")

	f.commit(t, f.main, "committed")
	committed := f.repoView(t, f.main, key)
	if committed.Layer != OverrideLayerRepoTOML || committed.Locus != f.main {
		t.Errorf("committed value reads as %s (%s), want the .pop/config.toml at %s",
			committed.Layer, committed.Locus, f.main)
	}

	writeConfigFile(t, f.userPath, "[repo."+quoted(f.main)+"]\npreferred_workbench = \"declared\"\n")
	declared := f.repoView(t, f.main, key)
	if declared.Layer != OverrideLayerConfig || !strings.Contains(declared.EffectiveTOML, "declared") {
		t.Errorf("declared value reads as %s: %q", declared.Layer, declared.EffectiveTOML)
	}

	f.writeOverride(t, "[repo."+quoted(f.identity)+"]\npreferred_workbench = \"stated\"\n")
	stated := f.repoView(t, f.main, key)
	if !stated.Overridden || !strings.Contains(stated.EffectiveTOML, "stated") {
		t.Errorf("override reads as %s: %q", stated.Layer, stated.EffectiveTOML)
	}
	if !strings.Contains(stated.SourceTOML, "declared") {
		t.Errorf("source below the override = %q, want the declared value", stated.SourceTOML)
	}
}

// The buffer gate is the write side's own: a buffer that sets the wrong key, sets
// a second one, or holds a value the block cannot read back is a problem the
// human is sent back to fix, and nothing is written.
func TestRepoScopeBufferRefusals(t *testing.T) {
	f := newOverrideScopeFixture(t)
	key := RepoScopeKey("preferred_workbench")

	for _, tc := range []struct {
		name   string
		key    string
		buffer string
	}{
		{"a key of no scope", "repo.nonsense", `repo.nonsense = "x"`},
		{"a buffer setting nothing", key, "\n"},
		{"a buffer setting another key", key, `repo.trunk = "/elsewhere"`},
		{"a buffer setting two keys", key, key + " = \"solo\"\nrepo.trunk = \"/elsewhere\"\n"},
		{"a value the block cannot hold", RepoScopeKey("turn_cap"), `repo.turn_cap = "lots"`},
	} {
		problem, err := StoreRepoOverrideBufferWith(f.d, f.main, tc.key, tc.buffer)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if problem == "" {
			t.Errorf("%s: stored without complaint", tc.name)
		}
	}

	if _, ok, err := RepoOverrideValueWith(f.d, f.main, "preferred_workbench"); err != nil || ok {
		t.Errorf("a refused buffer still wrote something (present=%v, err=%v)", ok, err)
	}
}

// Copying the source down and removing the override are the two keystroke
// actions, and they undo each other: what the layer below says becomes the
// override, and removing it puts that layer back in force.
func TestRepoScopeCopySourceAndRemove(t *testing.T) {
	f := newOverrideScopeFixture(t)
	key := RepoScopeKey("preferred_workbench")
	f.commit(t, f.main, "committed")

	if err := CopyRepoOverrideFromSourceWith(f.d, f.userPath, f.main, key); err != nil {
		t.Fatalf("copy source: %v", err)
	}
	copied := f.repoView(t, f.main, key)
	if !copied.Overridden || !strings.Contains(copied.EffectiveTOML, "committed") {
		t.Fatalf("copied override = %q (overridden=%v)", copied.EffectiveTOML, copied.Overridden)
	}

	if err := DeleteRepoOverrideValueWith(f.d, f.main, "preferred_workbench"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	restored := f.repoView(t, f.main, key)
	if restored.Overridden || restored.Layer != OverrideLayerRepoTOML {
		t.Errorf("removing the override left %s (overridden=%v)", restored.Layer, restored.Overridden)
	}
}

// The row list is reflected from the block schema, minus the keys the walker
// unions across rungs — for those no single layer holds the value a preview
// would name. Every key it does offer has to be readable out of a block, or a
// row would render a value no layer could ever supply.
func TestRepoScopeRowsCoverTheBlockSchema(t *testing.T) {
	offered := map[string]bool{}
	for _, doc := range RepoScopeKeyDocs() {
		offered[doc.Key] = true
	}
	if !offered["preferred_workbench"] {
		t.Error("preferred_workbench is not offered")
	}
	if offered["workbenches"] {
		t.Error("workbenches is unioned across rungs and cannot be previewed as one layer's value")
	}

	cap := 3
	trunk := TrunkPath("/repo/main")
	block := RepoOverrideConfig{
		RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "solo"},
		Trunk:           &trunk,
		TurnCap:         &cap,
	}
	doc := repoBlockDoc(block, "/repo/main")
	for key := range offered {
		if _, ok := doc[key]; !ok {
			t.Errorf("a block declaring every key supplies no %q: the row would never resolve", key)
		}
	}
}
