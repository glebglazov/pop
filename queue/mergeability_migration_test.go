package queue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks/integration"
)

func TestReadDaemonStateMigratesLegacyMergeability(t *testing.T) {
	td := queueDataDeps(t)
	key := setScopedKey("repo", "set-legacy")

	legacy, err := json.Marshal(map[string]any{
		"version": 1,
		"mergeability": map[string]MergeabilityRecord{
			key: {RuntimePath: "/some/checkout", SetID: "set-legacy", Status: MergeabilityClean},
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
	store, err := integration.Load(td)
	if err != nil {
		t.Fatalf("load mergeability store: %v", err)
	}
	rec, ok := store.Get(key)
	if !ok || rec.RuntimePath != "/some/checkout" || rec.Status != MergeabilityClean {
		t.Fatalf("legacy mergeability not migrated: %+v", store.Records)
	}

	if err := WriteDaemonState(td, state); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := td.FS.ReadFile(DaemonStatePath(td))
	if err != nil {
		t.Fatalf("reread state: %v", err)
	}
	if strings.Contains(string(raw), "mergeability") {
		t.Fatalf("daemon state still carries mergeability: %s", raw)
	}

	store, err = integration.Load(td)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if got, ok := store.Get(key); !ok || got.RuntimePath != "/some/checkout" {
		t.Fatalf("store missing migrated record: %+v", store.Records)
	}
}
