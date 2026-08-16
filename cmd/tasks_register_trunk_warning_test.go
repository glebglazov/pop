package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// The register-time trunk warning is one line, and these tests care only about
// that line: the rest of register's output is a status render.
const trunkAutoDrainWarnMarker = "is bound to the Trunk worktree"

func trunkAutoDrainWarnings(out string) []string {
	var found []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "warning: auto-drain is on and") && strings.Contains(line, trunkAutoDrainWarnMarker) {
			found = append(found, line)
		}
	}
	return found
}

// stubTaskConfigProjects points config resolution at one project path for the
// test's duration, as the surrounding register tests do inline.
func stubTaskConfigProjects(t *testing.T, path string) {
	t.Helper()
	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: path}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })
}

// TestTaskRegisterWarnsOnTrunkAutoDrain covers ADR-0192 decision 4: registering
// an auto-drained set from the Trunk worktree, with no managed worktree asked
// for, says out loud that the Work daemon may now drain unattended on the branch
// the human is standing on — once, and without failing the registration.
func TestTaskRegisterWarnsOnTrunkAutoDrain(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubTaskConfigProjects(t, root)
	writeTaskThoughts(t, cmdTasksDir(t, td, root), "draft")

	taskRegisterAutoDrain = true
	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("register --auto-drain on trunk must succeed, got: %v", err)
	}

	warnings := trunkAutoDrainWarnings(out.String())
	if len(warnings) != 1 {
		t.Fatalf("want exactly one trunk auto-drain warning, got %d:\n%s", len(warnings), out.String())
	}
	// The line has to name the hazard and the way out, not just the fact.
	for _, want := range []string{"unattended", "managed"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning does not mention %q: %s", want, warnings[0])
		}
	}
}

// TestTaskRegisterTrunkWarningStaysQuiet pins the whole rest of the predicate:
// the warning fires on the auto-drained-and-on-trunk-and-unmanaged shape and on
// nothing else. A bare repository is quiet through the existing
// bare-reads-worktree rule, not through a second gate in the register path.
func TestTaskRegisterTrunkWarningStaysQuiet(t *testing.T) {
	// The linked-worktree case is asserted where that registration already runs:
	// TestTaskRegisterFromWorktreeBindsInPlaceAndAutoDrains.

	t.Run("without auto-drain", func(t *testing.T) {
		root, _, td := setupCmdRepoTest(t)
		resetTaskFlags()
		t.Cleanup(resetTaskFlags)
		stubTaskConfigProjects(t, root)
		writeTaskThoughts(t, cmdTasksDir(t, td, root), "draft")

		assertQuietRegister(t, td)
	})

	t.Run("with a managed worktree", func(t *testing.T) {
		root, _, td := setupCmdRepoTest(t)
		resetTaskFlags()
		t.Cleanup(resetTaskFlags)
		stubTaskConfigProjects(t, root)
		writeTaskThoughts(t, cmdTasksDir(t, td, root), "draft")

		taskRegisterAutoDrain = true
		taskRegisterManaged = true
		assertQuietRegister(t, td)
	})

	t.Run("in a bare repository", func(t *testing.T) {
		base := t.TempDir()
		seed := filepath.Join(base, "seed")
		if err := os.MkdirAll(seed, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepoWithCommitCmd(t, seed)
		bare := filepath.Join(base, "bare.git")
		runGitCheckout(t, base, "clone", "--bare", seed, bare)
		// A bare repo has no trunk to stand in, so the only checkout of one you
		// can register from is a worktree of it.
		wt := filepath.Join(base, "bare-wt")
		runGitCheckout(t, bare, "worktree", "add", "-b", "bare-branch", wt)

		deps := newTestCmdDeps(t, wt, filepath.Join(base, ".xdg"), "")
		setCmdLayerDeps(t, deps)
		td := deps.tasksDeps()
		resetTaskFlags()
		t.Cleanup(resetTaskFlags)
		stubTaskConfigProjects(t, bare)
		writeTaskThoughts(t, cmdTasksDir(t, td, wt), "draft")

		// The quiet below has to come from the bare-reads-worktree rule the
		// locality predicate already carries, not from anything register added.
		if locality, isBare, err := checkoutLocality(td, realPath(t, wt)); err != nil || !isBare || locality != localityWorktree {
			t.Fatalf("setup: want a bare checkout reading %q, got locality=%q bare=%v err=%v", localityWorktree, locality, isBare, err)
		}

		taskRegisterAutoDrain = true
		assertQuietRegister(t, td)
	})
}

func assertQuietRegister(t *testing.T, td *tasks.Deps) {
	t.Helper()
	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("register must succeed: %v", err)
	}
	if warnings := trunkAutoDrainWarnings(out.String()); len(warnings) != 0 {
		t.Fatalf("register must be quiet here, got:\n%s", strings.Join(warnings, "\n"))
	}
}
