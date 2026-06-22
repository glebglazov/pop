package queue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// TestIntegrateRecomputesMergeabilityFromBindingWhenNoRecord verifies the third
// ADR-0051 surface: integrate is binding-driven. A worktree-bound set with no
// Mergeability record (its final task was completed by hand pre-fix) must still
// integrate — integrate recomputes Mergeability from the binding rather than
// refusing with "not awaiting integration".
func TestIntegrateRecomputesMergeabilityFromBindingWhenNoRecord(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-orphan")
	runGit(t, repo, "worktree", "add", "-b", "set-orphan", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-1")
	// Only a binding — deliberately NO mergeability record.
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-orphan"),
	})

	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := Integrate(d, cfg, "set-1", &out)
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if got.Noop {
		t.Fatalf("integrate no-oped on a binding-only orphan: %q", out.String())
	}
	if got.Branch != "set-orphan" {
		t.Fatalf("result = %+v, want branch set-orphan", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "set.txt")); err != nil {
		t.Fatalf("merged file missing from working branch: %v", err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree stat err = %v, want torn down", err)
	}
	if len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("binding = %+v, want cleared after integrate", loadBindingStore(t, td))
	}
	if len(loadMergeabilityStore(t, td)) != 0 {
		t.Fatalf("mergeability = %+v, want cleared after integrate", loadMergeabilityStore(t, td))
	}
	if !strings.Contains(out.String(), "integrated set-1") {
		t.Fatalf("output = %q, want integration message", out.String())
	}
}

// TestIntegrateNoBindingNoRecordStillNoops verifies the fallback is conservative:
// a set with neither a record nor a binding still no-ops (nothing to integrate).
func TestIntegrateNoBindingNoRecordStillNoops(t *testing.T) {
	repo := initMergeabilityRepo(t)
	td := queueDataDeps(t)
	d := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := Integrate(d, cfg, "ghost-set", &out)
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if !got.Noop {
		t.Fatalf("result = %+v, want noop for a set with no binding and no record", got)
	}
	if !strings.Contains(out.String(), "not awaiting integration") {
		t.Fatalf("output = %q, want not-awaiting message", out.String())
	}
}
