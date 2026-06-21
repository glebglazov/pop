package queue

import (
	"testing"
	"time"
)

func TestCompleteIntegrationSetIDsEmpty(t *testing.T) {
	td := queueDataDeps(t)
	ids, err := CompleteIntegrationSetIDs(&Deps{Tasks: td})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %#v, want empty", ids)
	}
}

func TestCompleteIntegrationSetIDsDedupesAndSorts(t *testing.T) {
	td := queueDataDeps(t)
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
		"b|set-b": {
			Project: "beta",
			SetID:   "set-b",
			Status:  MergeabilityConflicts,
		},
		"a|set-a": {
			Project: "alpha",
			SetID:   "set-a",
			Status:  MergeabilityClean,
		},
		"a|set-a-dup": {
			Project:   "alpha2",
			SetID:     "set-a",
			Status:    MergeabilityClean,
			CheckedAt: time.Now(),
		},
	})

	ids, err := CompleteIntegrationSetIDs(&Deps{Tasks: td})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "set-a" || ids[1] != "set-b" {
		t.Fatalf("ids = %#v, want [set-a set-b]", ids)
	}
}

func TestCompleteAbandonSetIDsEmpty(t *testing.T) {
	td := queueDataDeps(t)
	ids, err := CompleteAbandonSetIDs(&Deps{Tasks: td})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %#v, want empty", ids)
	}
}

func TestCompleteAbandonSetIDsDedupesAndSorts(t *testing.T) {
	td := queueDataDeps(t)
	seedBindingStore(t, td, map[string]WorktreeBinding{
		"beta\x00set-b":   {RuntimePath: "/wt/b", Project: "beta"},
		"alpha\x00set-a":  {RuntimePath: "/wt/a", Project: "alpha"},
		"alpha2\x00set-a": {RuntimePath: "/wt/a2", Project: "alpha2"},
	})

	ids, err := CompleteAbandonSetIDs(&Deps{Tasks: td})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "set-a" || ids[1] != "set-b" {
		t.Fatalf("ids = %#v, want [set-a set-b]", ids)
	}
}
