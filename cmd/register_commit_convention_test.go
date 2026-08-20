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

// resolvedCommitConvention is what the stack answers for the checkout at root —
// the value a register is expected to project into a manifest.
func resolvedCommitConvention(t *testing.T, td *tasks.Deps, root string) string {
	t.Helper()
	stack, err := conventions.Resolve(&conventions.Deps{FS: td.FS, Tasks: td}, conventions.KindCommits, root)
	if err != nil {
		t.Fatalf("resolve commits convention: %v", err)
	}
	want := strings.TrimSpace(conventions.StackProse(stack))
	if want == "" {
		t.Fatal("the commits kind must always answer")
	}
	return want
}

// TestTaskRegisterWritesTheConventionIntoAMalformedSet covers the path the
// issue-tracker doc prescribes: a set that registers MALFORMED, is fixed, and is
// re-registered. The set registers into state either way, and the fixing
// re-register is not a new registration, so a set skipped here for malformity
// would keep the planning agent's copy forever. The key therefore lands on the
// document itself — the invalid value the author typed is carried across
// untouched rather than replaced by pop's projection of the parsed manifest.
func TestTaskRegisterWritesTheConventionIntoAMalformedSet(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	setDir := registerConventionSet(t, tasksDir, "2026-08-20-broken", "STALE: subjects look roughly like `thing: summary`")

	// A task type no validator accepts: JSON-valid, manifest-invalid.
	raw := readSetKeys(t, setDir)
	raw["tasks"] = json.RawMessage(`[{"id":"01-a","file":"01-a.md","title":"A","type":"SIDEWAYS","status":"open","commit_subject":"feat(demo): teach the widget to blink"}]`)
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "index.json"), data, 0o644); err != nil {
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

	want := resolvedCommitConvention(t, td, root)
	after := readSetKeys(t, setDir)
	var got string
	if err := json.Unmarshal(after["commit_convention"], &got); err != nil {
		t.Fatalf("commit_convention on disk = %s", after["commit_convention"])
	}
	if got != want {
		t.Fatalf("a malformed set kept the planning agent's convention.\ngot:\n%s\nwant:\n%s", got, want)
	}
	// Nothing else was rewritten from the parse: the value that made the set
	// malformed is still the author's own, so the diagnostics still name it.
	if !strings.Contains(string(after["tasks"]), `"type": "SIDEWAYS"`) {
		t.Errorf("the write projected parsed fields back over the author's manifest: %s", after["tasks"])
	}

	// The human fixes the type and re-registers: the set is already registered,
	// so this register is not an activation — and the convention already written
	// stands.
	fixed := readSetKeys(t, setDir)
	fixed["tasks"] = json.RawMessage(`[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open","commit_subject":"feat(demo): teach the widget to blink"}]`)
	data, err = json.Marshal(fixed)
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
	m := tasks.LoadManifest(td, "2026-08-20-broken", filepath.Join(setDir, "index.json"))
	if !m.Valid {
		t.Fatalf("the fixed manifest is still not valid: %v", m.Errors)
	}
	if m.CommitConvention != want {
		t.Fatalf("after the fixing re-register the convention is %q", m.CommitConvention)
	}
}

// TestTaskRegisterFillsALiveSetsMissingConvention covers the other half of the
// boundary: a register leaves a registered set's convention alone, but a set
// carrying none at all is not left unable to answer what a commit looks like
// here — a mid-drain Remediation renders its subject from that key.
func TestTaskRegisterFillsALiveSetsMissingConvention(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	setDir := registerConventionSet(t, tasksDir, "2026-08-20-empty", "")

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("first register failed: %v\n%s", err, out.String())
	}

	// A set that registered before pop wrote the key at all: strip it back out.
	stripped := readSetKeys(t, setDir)
	delete(stripped, "commit_convention")
	data, err := json.Marshal(stripped)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("second register failed: %v\n%s", err, out.String())
	}
	want := resolvedCommitConvention(t, td, root)
	var got string
	if err := json.Unmarshal(readSetKeys(t, setDir)["commit_convention"], &got); err != nil {
		t.Fatalf("commit_convention on disk = %s", readSetKeys(t, setDir)["commit_convention"])
	}
	if got != want {
		t.Fatalf("a registered set was left with no convention.\ngot:\n%s\nwant:\n%s", got, want)
	}
}
