package queue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks/binding"
)

// TestReadDaemonStateMigratesLegacyBindings verifies that bindings still
// embedded in a pre-ADR-0036 daemon state file are surfaced on read, persisted
// to the shared binding store on the next write, and dropped from the daemon
// state file itself.
func TestReadDaemonStateMigratesLegacyBindings(t *testing.T) {
	td := queueDataDeps(t)
	key := setScopedKey("repo", "set-legacy")

	// Hand-write a legacy state.json carrying a worktree binding inline, the
	// pre-ADR-0036 layout the binding store now replaces.
	legacy, err := json.Marshal(map[string]any{
		"version": 1,
		"worktree_bindings": map[string]WorktreeBinding{
			key: {RuntimePath: "/some/checkout", Branch: "pop/set-legacy/x", Project: "proj", Provisioned: true},
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if err := td.FS.MkdirAll(QueueDataDir(td), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := td.FS.WriteFile(DaemonStatePath(td), legacy, 0o644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	b, ok := state.WorktreeBindings[key]
	if !ok {
		t.Fatalf("legacy binding not surfaced: %+v", state.WorktreeBindings)
	}
	if b.RuntimePath != "/some/checkout" || !b.Provisioned {
		t.Fatalf("legacy binding = %+v, want /some/checkout provisioned", b)
	}

	// Persist: the store now owns the binding; state.json must shed it.
	if err := WriteDaemonState(td, state); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := td.FS.ReadFile(DaemonStatePath(td))
	if err != nil {
		t.Fatalf("reread state: %v", err)
	}
	if strings.Contains(string(raw), "worktree_bindings") {
		t.Fatalf("daemon state still carries bindings: %s", raw)
	}

	store, err := binding.Load(td)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if got, ok := store.Get(key); !ok || got.RuntimePath != "/some/checkout" {
		t.Fatalf("store missing migrated binding: %+v", store.Bindings)
	}
}

func TestReadDaemonStateIgnoresLegacyAgentCooldowns(t *testing.T) {
	td := queueDataDeps(t)
	legacy, err := json.Marshal(map[string]any{
		"version": 1,
		"agent_cooldowns": map[string]string{
			"codex": "2026-06-20T12:00:00Z",
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if err := td.FS.MkdirAll(QueueDataDir(td), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := td.FS.WriteFile(DaemonStatePath(td), legacy, 0o644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read legacy cooldown state: %v", err)
	}
	if err := WriteDaemonState(td, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	raw, err := td.FS.ReadFile(DaemonStatePath(td))
	if err != nil {
		t.Fatalf("reread state: %v", err)
	}
	if strings.Contains(string(raw), "agent_cooldowns") {
		t.Fatalf("daemon state still carries legacy agent cooldowns: %s", raw)
	}
}
