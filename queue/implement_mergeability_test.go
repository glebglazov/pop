package queue

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// implementMergeabilityTestDeps returns task Deps backed by real git/fs with
// XDG_DATA_HOME redirected to a temp dir so binding store and daemon state
// write into the test sandbox.
func implementMergeabilityTestDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:    real.ReadFile,
		WriteFileFunc:   real.WriteFile,
		MkdirAllFunc:    real.MkdirAll,
		RenameFunc:      real.Rename,
		RemoveAllFunc:   real.RemoveAll,
		EvalSymlinksFunc: real.EvalSymlinks,
		GetwdFunc:       real.Getwd,
		UserHomeDirFunc: real.UserHomeDir,
		StatFunc:        real.Stat,
	}
	d.Git = deps.NewRealGit()
	return d
}

func implementRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

// initImplementRepo creates a non-bare repo with one commit and returns the trunk path.
func initImplementRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	implementRunGit(t, repo, "init")
	implementRunGit(t, repo, "config", "user.email", "pop@example.test")
	implementRunGit(t, repo, "config", "user.name", "Pop Test")
	if err := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "base").Run(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return repo
}

// addImplementLinkedWorktree adds a linked worktree on a fresh branch, commits
// a file in it, and returns its path.
func addImplementLinkedWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	implementRunGit(t, repo, "worktree", "add", "-b", branch, wt, "HEAD")
	writeFile(t, filepath.Join(wt, branch+".txt"), "change\n")
	implementRunGit(t, wt, "add", branch+".txt")
	implementRunGit(t, wt, "commit", "-m", "change in "+branch)
	return wt
}

// TestIntegrationBacklogWorktreeDrainAppearsRegardlessOfTrigger verifies that
// an implement-sourced backlog entry and a queue-sourced backlog entry are both
// visible in AwaitingIntegration and are indistinguishable in the view.
func TestIntegrationBacklogWorktreeDrainAppearsRegardlessOfTrigger(t *testing.T) {
	td := implementMergeabilityTestDeps(t)
	repo := initImplementRepo(t)
	wt := addImplementLinkedWorktree(t, repo, "feature")

	// Adopt the worktree checkout (as implement would via BindCheckout).
	adopted, err := binding.AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a")
	if err != nil || !adopted {
		t.Fatalf("setup adopt: adopted=%v err=%v", adopted, err)
	}

	qd := &Deps{Tasks: td}

	// Seed a queue-sourced mergeability record in the shared store.
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
		"queue-repo\x00set-b": {
			Project:     "queue-project",
			SetID:       "set-b",
			RuntimePath: "/some/queue/worktree",
			Status:      MergeabilityClean,
			CheckedAt:   time.Now().UTC(),
		},
	})

	// Record implement-sourced mergeability.
	if err := RecordImplementMergeability(qd, repo, wt, "set-a", "implement-project"); err != nil {
		t.Fatalf("RecordImplementMergeability: %v", err)
	}

	// Load the final backlog view.
	finalState, err := EnsureDaemonState(td)
	if err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	snap, err := statusFromDecisions(&Deps{Tasks: td}, nil, finalState)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if len(snap.AwaitingIntegration) != 2 {
		t.Fatalf("AwaitingIntegration = %d entries, want 2 (one queue, one implement)", len(snap.AwaitingIntegration))
	}

	setIDs := map[string]bool{}
	for _, entry := range snap.AwaitingIntegration {
		setIDs[entry.SetID] = true
		if entry.SetID == "set-a" && entry.Status != MergeabilityClean {
			t.Errorf("implement entry status = %q, want %q", entry.Status, MergeabilityClean)
		}
	}
	if !setIDs["set-a"] {
		t.Errorf("implement-sourced entry (set-a) missing from backlog")
	}
	if !setIDs["set-b"] {
		t.Errorf("queue-sourced entry (set-b) missing from backlog")
	}
}

// TestIntegrationBacklogTrunkDrainNeverAppears verifies that a trunk drain
// (no worktree binding) is never added to the Integration backlog.
func TestIntegrationBacklogTrunkDrainNeverAppears(t *testing.T) {
	td := implementMergeabilityTestDeps(t)
	repo := initImplementRepo(t)

	// No binding adoption — simulates a trunk drain where AdoptCurrentCheckout
	// returned (false, nil) because the checkout is the main working tree.
	qd := &Deps{Tasks: td}
	if err := RecordImplementMergeability(qd, repo, repo, "set-trunk", "trunk-project"); err != nil {
		t.Fatalf("RecordImplementMergeability: %v", err)
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
		t.Fatalf("trunk drain must not appear in backlog, got %d entries: %+v", len(snap.AwaitingIntegration), snap.AwaitingIntegration)
	}
}
