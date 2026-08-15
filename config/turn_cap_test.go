package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(n int) *int { return &n }

// TestRepoTurnCapResolvesForEveryWorktree pins ADR-0191 decision 4: the bound is
// keyed by repository identity, not by the exact checkout that keys the block, so
// every worktree of a bare repo reads the one number — the same keying trunk
// gained beside it (ADR-0212 decision 3).
func TestRepoTurnCapResolvesForEveryWorktree(t *testing.T) {
	bareRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bareRoot, ".bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(bareRoot, "main")
	feature := filepath.Join(bareRoot, "feature")
	for _, dir := range []string{main, feature} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d := DefaultDeps()

	cfg := &Config{Repo: map[string]RepoOverrideConfig{
		main: {TurnCap: intPtr(12), Trunk: trunkPtr(main)},
	}}
	for _, checkout := range []string{main, feature, bareRoot} {
		got, err := cfg.ResolveRepoConfig(d, checkout)
		if err != nil {
			t.Fatalf("%s: %v", checkout, err)
		}
		if got.TurnCap != 12 {
			t.Errorf("TurnCap at %s = %d, want 12 (one bound per repository)", checkout, got.TurnCap)
		}
	}

	// trunk is keyed the same way: the sibling worktree reads the repository's
	// trunk path — which is main, so the sibling is not itself the Trunk.
	sibling, err := cfg.ResolveRepoConfig(d, feature)
	if err != nil {
		t.Fatal(err)
	}
	if !sibling.IsTrunk(d, main) {
		t.Errorf("trunk at the sibling = %q, want %s (one fork base per repository)", sibling.Trunk, main)
	}
	if sibling.IsTrunk(d, feature) {
		t.Error("the sibling worktree must not read itself as the Trunk")
	}

	none, err := (&Config{}).ResolveRepoConfig(d, feature)
	if err != nil {
		t.Fatal(err)
	}
	if none.TurnCap != 0 {
		t.Errorf("TurnCap with no declaration = %d, want 0", none.TurnCap)
	}

	zero, err := (&Config{Repo: map[string]RepoOverrideConfig{main: {TurnCap: intPtr(0)}}}).ResolveRepoConfig(d, feature)
	if err != nil {
		t.Fatal(err)
	}
	if zero.TurnCap != 0 {
		t.Errorf("TurnCap = %d for turn_cap = 0, want 0 (a non-positive number bounds nothing)", zero.TurnCap)
	}
}

// TestTurnCapRejectedInPopTOML pins ADR-0191 decision 1: the key is central-only,
// so bounding a repository's drains never requires committing a pop artifact into
// it. The finding names the key and points at its real home.
func TestTurnCapRejectedInPopTOML(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pop"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "preferred_workbench = \"gs-dev\"\nturn_cap = 9\n"
	if err := os.WriteFile(filepath.Join(root, ".pop", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRepoConfigWith(DefaultDeps(), root)
	if err != nil {
		t.Fatalf("LoadRepoConfigWith: %v", err)
	}
	if cfg.TurnCap != 0 {
		t.Errorf("TurnCap = %d, want 0 (turn_cap in .pop/config.toml must not be honored)", cfg.TurnCap)
	}
	if cfg.PreferredWorkbench != "gs-dev" {
		t.Errorf("the legal key was lost: preferred_workbench = %q", cfg.PreferredWorkbench)
	}
	found := false
	for _, f := range cfg.Findings {
		if strings.Contains(f.Message, "turn_cap is only valid in a global") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a finding naming turn_cap, got: %+v", cfg.Findings)
	}
}

// TestTurnCapAcceptedInRepoBlock proves the central block takes the key with no
// finding, and that the accepted-key message an unknown sibling key draws now
// lists it.
func TestTurnCapAcceptedInRepoBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[repo.\"/srv/kestrel\"]\nturn_cap = 40\nnot_a_key = 1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	block, ok := cfg.Repo["/srv/kestrel"]
	if !ok {
		t.Fatal("repo block not parsed")
	}
	if block.TurnCap == nil || *block.TurnCap != 40 {
		t.Fatalf("turn_cap = %v, want 40", block.TurnCap)
	}
	listed := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "not_a_key") && strings.Contains(w, "turn_cap") {
			listed = true
		}
		if strings.Contains(w, "turn_cap") && strings.Contains(w, "unknown key \"turn_cap\"") {
			t.Fatalf("turn_cap flagged as unknown in a repo block: %q", w)
		}
	}
	if !listed {
		t.Errorf("accepted-key message should list turn_cap, got: %v", cfg.Warnings)
	}
}
