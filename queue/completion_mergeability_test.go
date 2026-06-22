package queue

import (
	"testing"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/tasks"
)

// doneCompletionRefresh builds a minimal RefreshResult whose set derives to Done.
func doneCompletionRefresh(setID string) *tasks.RefreshResult {
	return &tasks.RefreshResult{
		Manifests: map[string]*tasks.Manifest{
			setID: {Valid: true, Tasks: []tasks.Task{{ID: "t1", Status: "done"}}},
		},
	}
}

func completionDeps(td *tasks.Deps) *integration.Deps {
	id := integration.DefaultDeps()
	id.Tasks = td
	return id
}

// TestRecordCompletionMergeabilityWorktreeSetEntersBacklog verifies that
// manually completing a worktree-bound set to Done records Mergeability, so the
// set enters the Integration backlog without ever being drained (ADR-0051).
func TestRecordCompletionMergeabilityWorktreeSetEntersBacklog(t *testing.T) {
	td := implementMergeabilityTestDeps(t)
	repo := initImplementRepo(t)
	wt := addImplementLinkedWorktree(t, repo, "feature")

	adopted, err := binding.AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a")
	if err != nil || !adopted {
		t.Fatalf("setup adopt: adopted=%v err=%v", adopted, err)
	}

	if err := integration.RecordCompletionMergeability(completionDeps(td), repo, "set-a", doneCompletionRefresh("set-a")); err != nil {
		t.Fatalf("RecordCompletionMergeability: %v", err)
	}

	state, err := EnsureDaemonState(td)
	if err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	snap, err := statusFromDecisions(&Deps{Tasks: td}, nil, state)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(snap.AwaitingIntegration) != 1 || snap.AwaitingIntegration[0].SetID != "set-a" {
		t.Fatalf("AwaitingIntegration = %+v, want one entry for set-a", snap.AwaitingIntegration)
	}
}

// TestRecordCompletionMergeabilitySkipsWhenNotDone verifies that completing a
// non-final task (set not yet Done) records nothing — Mergeability belongs to
// concluded sets only.
func TestRecordCompletionMergeabilitySkipsWhenNotDone(t *testing.T) {
	td := implementMergeabilityTestDeps(t)
	repo := initImplementRepo(t)
	wt := addImplementLinkedWorktree(t, repo, "feature")

	adopted, err := binding.AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a")
	if err != nil || !adopted {
		t.Fatalf("setup adopt: adopted=%v err=%v", adopted, err)
	}

	openRefresh := &tasks.RefreshResult{
		Manifests: map[string]*tasks.Manifest{
			"set-a": {Valid: true, Tasks: []tasks.Task{{ID: "t1", Type: "AFK", Status: "open"}}},
		},
	}
	if err := integration.RecordCompletionMergeability(completionDeps(td), repo, "set-a", openRefresh); err != nil {
		t.Fatalf("RecordCompletionMergeability: %v", err)
	}

	state, err := EnsureDaemonState(td)
	if err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	snap, err := statusFromDecisions(&Deps{Tasks: td}, nil, state)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(snap.AwaitingIntegration) != 0 {
		t.Fatalf("not-done set must record nothing, got %+v", snap.AwaitingIntegration)
	}
}

// TestRecordCompletionMergeabilitySkipsTrunkSet verifies that a Done set with no
// worktree binding (a trunk drain) records nothing.
func TestRecordCompletionMergeabilitySkipsTrunkSet(t *testing.T) {
	td := implementMergeabilityTestDeps(t)
	repo := initImplementRepo(t)

	if err := integration.RecordCompletionMergeability(completionDeps(td), repo, "set-trunk", doneCompletionRefresh("set-trunk")); err != nil {
		t.Fatalf("RecordCompletionMergeability: %v", err)
	}

	state, err := EnsureDaemonState(td)
	if err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	snap, err := statusFromDecisions(&Deps{Tasks: td}, nil, state)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(snap.AwaitingIntegration) != 0 {
		t.Fatalf("trunk set must record nothing, got %+v", snap.AwaitingIntegration)
	}
}
