package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/tasks"
)

// registerConventionSet writes a set whose manifest carries a planning agent's
// own `commit_convention` beside a frozen per-task subject, so a register can be
// watched replacing one and leaving the other alone.
func registerConventionSet(t *testing.T, tasksDir, stem, convention string) string {
	t.Helper()
	setDir := filepath.Join(tasksDir, stem)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"tasks": []map[string]any{{
			"id": "01-a", "file": "01-a.md", "title": "A", "type": "AFK", "status": "open",
			"commit_subject": "feat(demo): teach the widget to blink",
		}},
		"commit_convention": convention,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return setDir
}

// readSetKeys reads the manifest back as raw JSON, so the assertions are about
// what is on disk rather than what a loader reconstructs.
func readSetKeys(t *testing.T, setDir string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("manifest is not JSON after register: %v", err)
	}
	return raw
}

// TestTaskRegisterWritesTheResolvedCommitConvention covers ADR-0228: the set's
// `commit_convention` is pop's projection of the resolved stack — overlay
// included — written at register time over whatever a planning agent retyped,
// while the frozen per-task subject is left exactly as authored.
func TestTaskRegisterWritesTheResolvedCommitConvention(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	setDir := registerConventionSet(t, tasksDir, "2026-08-20-demo", "STALE: subjects look roughly like `thing: summary`")

	// The repository's own commit document answers, with the human's overlay
	// appended — the two clauses a hand-copy is observed to drop.
	repoDoc := filepath.Join(root, "docs", "agents", "commits.md")
	if err := os.MkdirAll(filepath.Dir(repoDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoDoc, []byte("# Commits here\n\nSubjects read `verb(area): summary`, imperative, no trailing period.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(root, ".xdg", "home", ".agents", "docs", "commits.overlay.md")
	if err := os.MkdirAll(filepath.Dir(overlay), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, []byte("Never amend a commit that is already pushed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("register failed: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "warning:") {
		t.Fatalf("register warned about the convention it was meant to write:\n%s", out.String())
	}

	stack, err := conventions.Resolve(&conventions.Deps{FS: td.FS, Tasks: td}, conventions.KindCommits, root)
	if err != nil {
		t.Fatalf("resolve commits convention: %v", err)
	}
	want := strings.TrimSpace(conventions.StackProse(stack))
	if want == "" {
		t.Fatal("the commits kind must always answer")
	}

	raw := readSetKeys(t, setDir)
	var got string
	if err := json.Unmarshal(raw["commit_convention"], &got); err != nil {
		t.Fatalf("commit_convention on disk = %s", raw["commit_convention"])
	}
	if got != want {
		t.Fatalf("registered convention is not what the stack resolves.\ngot:\n%s\nwant:\n%s", got, want)
	}
	// Both halves of the resolved answer survive: the answering rank's document
	// and the overlay appended to it.
	for _, clause := range []string{"verb(area): summary", "Never amend a commit that is already pushed."} {
		if !strings.Contains(got, clause) {
			t.Errorf("registered convention dropped %q:\n%s", clause, got)
		}
	}
	// The agent's own copy is gone, not merged.
	if strings.Contains(got, "STALE:") {
		t.Errorf("a planning agent's value survived the register:\n%s", got)
	}
	// The frozen subject is the set's, used verbatim at commit time, and this
	// write must not go near it.
	if !strings.Contains(string(raw["tasks"]), "feat(demo): teach the widget to blink") {
		t.Errorf("the per-task commit subject did not survive the register: %s", raw["tasks"])
	}
	m := tasks.LoadManifest(td, "2026-08-20-demo", filepath.Join(setDir, "index.json"))
	if !m.Valid {
		t.Fatalf("manifest is no longer valid after the register: %v", m.Errors)
	}
	if m.Tasks[0].CommitSubject != "feat(demo): teach the widget to blink" {
		t.Errorf("planned subject = %q, want it verbatim", m.Tasks[0].CommitSubject)
	}
}

// TestTaskRegisterLeavesALiveSetsConventionAlone draws the boundary the write
// keeps: only the sets a register activates are written. Re-registering does not
// re-resolve, because moving the prose under a set that is already Work would
// change what a running drain's spawned task renders a subject from.
func TestTaskRegisterLeavesALiveSetsConventionAlone(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	setDir := registerConventionSet(t, tasksDir, "2026-08-20-live", "")

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("first register failed: %v\n%s", err, out.String())
	}
	first := string(readSetKeys(t, setDir)["commit_convention"])
	if first == "" {
		t.Fatal("the first register wrote no commit convention")
	}

	// A human's own edit to a registered set's key stands.
	edited := readSetKeys(t, setDir)
	edited["commit_convention"] = json.RawMessage(`"mine, hand-tuned"`)
	data, err := json.Marshal(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("re-register failed: %v\n%s", err, out.String())
	}
	if got := string(readSetKeys(t, setDir)["commit_convention"]); got != `"mine, hand-tuned"` {
		t.Fatalf("re-register rewrote a live set's convention: %s", got)
	}
}
