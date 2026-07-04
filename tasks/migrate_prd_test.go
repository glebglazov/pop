package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storageDir resolves the repository's storage directory (parent of tasks/),
// the sibling under which the retired prds/ directory lives.
func (e *migrateEnv) storageDir(t *testing.T) string {
	t.Helper()
	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	return id.StorageDir
}

func (e *migrateEnv) tasksDir(t *testing.T) string {
	t.Helper()
	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	return id.TasksDir
}

// writePRD drops a retired sibling prds/<name> file.
func (e *migrateEnv) writePRD(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join(e.storageDir(t), legacyPRDSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("# PRD "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeSetDir creates an empty Task-set folder in storage.
func (e *migrateEnv) writeSetDir(t *testing.T, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(e.tasksDir(t), id), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestMigratePRDColocationMovesMatchedBySlug(t *testing.T) {
	e := newMigrateEnv(t)
	// PRD and set share the slug "user-auth" but carry different timestamps.
	e.writePRD(t, "2026-05-31-user-auth.md")
	e.writeSetDir(t, "2026-06-02-user-auth")

	mig, err := MigratePRDColocation(e.deps, e.tasksDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if mig == nil {
		t.Fatal("expected a migration summary")
	}
	if strings.Join(mig.Moved, ",") != "2026-05-31-user-auth.md -> 2026-06-02-user-auth" {
		t.Fatalf("moved = %v", mig.Moved)
	}
	if len(mig.Unmatched) != 0 {
		t.Fatalf("unexpected unmatched: %v", mig.Unmatched)
	}

	// The PRD now lives inside the set as prd.md, and the sibling copy is gone.
	if _, err := os.Stat(filepath.Join(e.tasksDir(t), "2026-06-02-user-auth", "prd.md")); err != nil {
		t.Fatalf("co-located prd.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.storageDir(t), legacyPRDSubdir, "2026-05-31-user-auth.md")); !os.IsNotExist(err) {
		t.Fatalf("sibling PRD still present: %v", err)
	}
}

func TestMigratePRDColocationLeavesUnmatchedUntouched(t *testing.T) {
	e := newMigrateEnv(t)
	e.writePRD(t, "2026-05-31-orphan.md")   // no matching set
	e.writePRD(t, "2026-05-31-shared.md")   // matches two sets — ambiguous
	e.writeSetDir(t, "2026-06-01-shared")
	e.writeSetDir(t, "2026-06-02-shared")

	mig, err := MigratePRDColocation(e.deps, e.tasksDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if mig == nil {
		t.Fatal("expected a summary reporting unmatched files")
	}
	if len(mig.Moved) != 0 {
		t.Fatalf("nothing should move: %v", mig.Moved)
	}
	if strings.Join(mig.Unmatched, ",") != "2026-05-31-orphan.md,2026-05-31-shared.md" {
		t.Fatalf("unmatched = %v", mig.Unmatched)
	}

	// Both unmatched files remain where they were.
	for _, name := range []string{"2026-05-31-orphan.md", "2026-05-31-shared.md"} {
		if _, err := os.Stat(filepath.Join(e.storageDir(t), legacyPRDSubdir, name)); err != nil {
			t.Fatalf("unmatched PRD %s moved or removed: %v", name, err)
		}
	}
}

func TestMigratePRDColocationNeverOverwritesExistingPRD(t *testing.T) {
	e := newMigrateEnv(t)
	e.writePRD(t, "2026-05-31-user-auth.md")
	e.writeSetDir(t, "2026-05-31-user-auth")
	// A co-located PRD already exists in the set.
	dst := filepath.Join(e.tasksDir(t), "2026-05-31-user-auth", "prd.md")
	if err := os.WriteFile(dst, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mig, err := MigratePRDColocation(e.deps, e.tasksDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if mig == nil || len(mig.Moved) != 0 {
		t.Fatalf("expected no moves, got %#v", mig)
	}
	if strings.Join(mig.Unmatched, ",") != "2026-05-31-user-auth.md" {
		t.Fatalf("unmatched = %v", mig.Unmatched)
	}
	// The existing prd.md is untouched.
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# existing\n" {
		t.Fatalf("existing prd.md was overwritten: %q", data)
	}
}

func TestMigratePRDColocationIdempotent(t *testing.T) {
	e := newMigrateEnv(t)
	e.writePRD(t, "2026-05-31-user-auth.md")
	e.writeSetDir(t, "2026-05-31-user-auth")

	if _, err := MigratePRDColocation(e.deps, e.tasksDir(t)); err != nil {
		t.Fatal(err)
	}
	// Second run has nothing left to move and reports nothing.
	mig, err := MigratePRDColocation(e.deps, e.tasksDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if mig != nil {
		t.Fatalf("second run reported activity: %#v", mig)
	}
}

func TestMigratePRDColocationNoPRDsDir(t *testing.T) {
	e := newMigrateEnv(t)
	e.writeSetDir(t, "2026-05-31-user-auth")

	mig, err := MigratePRDColocation(e.deps, e.tasksDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if mig != nil {
		t.Fatalf("migration ran with no prds/ dir: %#v", mig)
	}
}
