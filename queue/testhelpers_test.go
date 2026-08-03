package queue

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/setkind"
)

// detailRowWithTasks returns row carrying what the Task-set kind would have
// built from manifest: the Work items the detail view lists, the malformed
// verdict, and the progress headline. The detail view reads the container, so a
// test that wants tasks on screen puts them there — through the kind's own
// projection, never a second one.
func detailRowWithTasks(row DashboardRow, manifest *tasks.Manifest, taskRow *tasks.Row) DashboardRow {
	row.Items = setkind.ItemsFromManifest(manifest)
	if manifest != nil && !manifest.Valid {
		row.Broken, row.BrokenReason = true, strings.Join(manifest.Errors, "; ")
	}
	if taskRow != nil {
		row.Headline = taskRow.Progress
	}
	return row
}

// newTaskDetailView opens a detail view over row as the Task-set kind would have
// loaded it.
func newTaskDetailView(row DashboardRow, manifest *tasks.Manifest, taskRow *tasks.Row) *detailView {
	return newDetailView(detailRowWithTasks(row, manifest, taskRow))
}

// initGitRepoWithBase creates a temp git repo with one committed file and
// returns its path. Shared across queue tests that need a real repository.
func initGitRepoWithBase(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "pop@example.test")
	runGit(t, repo, "config", "user.name", "Pop Test")
	writeFile(t, filepath.Join(repo, "shared.txt"), "base\n")
	runGit(t, repo, "add", "shared.txt")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

// bindSetInPlace binds setID to the repo checkout itself, so the set is
// Queue-drainable (ADR-0072) and routes in-place at the repo. With the
// integration-target fallback gone (ADR-0070), an unbound, no-directive set is
// no longer routed by the Queue, so tests that exercise the spawn machinery on
// the repo checkout bind the set explicitly.
func bindSetInPlace(t *testing.T, d *Deps, repo, setID string) {
	t.Helper()
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatalf("resolveRepoKey: %v", err)
	}
	// Merge into the existing store (binding.Put load-merges) so repeated calls
	// across repos accumulate rather than clobber.
	if err := binding.Put(d.Tasks, setScopedKey(repoKey, setID), WorktreeBinding{RuntimePath: repo, Project: filepath.Base(repo)}); err != nil {
		t.Fatalf("binding.Put: %v", err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
	return string(out)
}

// testKinds is the wiring list a test renders and dispatches through: the same
// real adapters `queue.Deps.WorkKinds` hands the snapshot builder, so a test row
// gets the cells and the verbs its own kind composes.
func testKinds() workKinds {
	return newWorkKinds((&Deps{}).WorkKinds(nil))
}
