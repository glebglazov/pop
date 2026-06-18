package tasks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshAutoRegistration(t *testing.T) {
	root := t.TempDir()
	// An Task set without any PRD is discovered and registered.
	setupManifest(t, root, "new-feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "new-feature" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[result.DefinitionPath]
	if entry == nil || len(entry.TaskSets) != 1 || entry.TaskSets[0].Priority != 0 {
		t.Fatalf("state = %#v", entry)
	}
	if entry.TaskSets[0].AutoDrain {
		t.Fatalf("auto_drain default = true, want false")
	}

	// Persisted registration uses the issue_sets key and stores registration metadata only.
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\"issue_sets\"") {
		t.Fatalf("state file missing issue_sets key:\n%s", raw)
	}
	if strings.Contains(string(raw), "\"prds\"") || strings.Contains(string(raw), "\"title\"") {
		t.Fatalf("state file has stale PRD fields:\n%s", raw)
	}
	if !strings.Contains(string(raw), "\"auto_drain\": false") {
		t.Fatalf("state file missing default auto_drain bit:\n%s", raw)
	}
}

func TestRefreshEmptyTaskNoStateFile(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("rows = %d", len(result.Rows))
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("expected no state file for empty task")
	}
}

func TestRefreshTableSections(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "done-prd", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	setupManifest(t, root, "active-high", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "active-low", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	statePath := filepath.Join(root, "state.json")
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &GlobalState{
		Version: StateVersion,
		Tasks: map[string]*TaskEntry{canon: {TaskSets: []RegisteredTaskSet{
			{ID: "gone-prd", Priority: 5},
			{ID: "done-prd", Priority: 0},
			{ID: "active-high", Priority: 10},
			{ID: "active-low", Priority: 0},
		}}},
		path: statePath,
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}

	wantOrder := []string{"gone-prd", "done-prd", "active-high", "active-low"}
	if len(result.Rows) != len(wantOrder) {
		t.Fatalf("rows = %d, want %d: %#v", len(result.Rows), len(wantOrder), result.Rows)
	}
	for i, id := range wantOrder {
		if result.Rows[i].ID != id {
			t.Fatalf("row[%d] = %q, want %q", i, result.Rows[i].ID, id)
		}
	}
	if result.Rows[0].Status != StatusMissing {
		t.Fatalf("first status = %q", result.Rows[0].Status)
	}
	if result.Rows[1].Status != StatusDone {
		t.Fatalf("done status = %q", result.Rows[1].Status)
	}
	if result.Rows[2].Priority != 10 || result.Rows[3].Priority != 0 {
		t.Fatalf("active priorities wrong: %d, %d", result.Rows[2].Priority, result.Rows[3].Priority)
	}
}

func TestTaskSetWithoutPRDIsRegisteredAndReady(t *testing.T) {
	root := t.TempDir()
	// No thoughts/prds at all — a valid Task set must still register and be Ready.
	setupManifest(t, root, "standalone", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "standalone" || result.Rows[0].Status != StatusReady {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "standalone" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}
}

func TestStatusDerivation(t *testing.T) {
	root := t.TempDir()

	setupManifest(t, root, "ready", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "blocked-hitl", []Task{
		{ID: "01-hitl", File: "01-hitl.md", Title: "H", Type: "HITL", Status: "open"},
	})
	setupManifest(t, root, "failed", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})

	statePath := filepath.Join(root, "state.json")
	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}

	statusByID := map[string]TaskSetStatus{}
	for _, row := range result.Rows {
		statusByID[row.ID] = row.Status
	}

	if statusByID["ready"] != StatusReady {
		t.Fatalf("ready = %q", statusByID["ready"])
	}
	if statusByID["blocked-hitl"] != StatusBlocked {
		t.Fatalf("blocked = %q", statusByID["blocked-hitl"])
	}
	if statusByID["failed"] != StatusFailed {
		t.Fatalf("failed = %q", statusByID["failed"])
	}
}

func TestRenderDiagnostics(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "bad")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] a\n")
	writeManifest(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "in_progress"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "MALFORMED") {
		t.Fatalf("missing MALFORMED in output:\n%s", out)
	}
	if !strings.Contains(out, "Malformed diagnostics:") {
		t.Fatalf("missing diagnostics in output:\n%s", out)
	}
	if !strings.Contains(out, "in_progress") {
		t.Fatalf("missing detail in output:\n%s", out)
	}
}

func TestUnknownEffortMarksTaskSetMalformed(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "bad-effort")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] a\n")
	writeManifest(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", Effort: "extreme"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != StatusMalformed {
		t.Fatalf("rows = %#v", result.Rows)
	}
	var buf bytes.Buffer
	Render(&buf, result)
	out := buf.String()
	if !strings.Contains(out, `invalid effort "extreme"`) {
		t.Fatalf("diagnostic missing offending effort:\n%s", out)
	}
}

func TestFailedRowResetHints(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "failed-prd", []Task{
		{ID: "01-broken", File: "repair-broken.md", Title: "B", Type: "AFK", Status: "failed"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != StatusFailed {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if len(result.Rows[0].ResetHints) != 1 || result.Rows[0].ResetHints[0] != "pop tasks open failed-prd/repair-broken.md" {
		t.Fatalf("reset hints = %v", result.Rows[0].ResetHints)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	if !strings.Contains(buf.String(), "reset: pop tasks open") {
		t.Fatalf("output missing reset hint: %s", buf.String())
	}
}

func TestBlockedReasonInTable(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "blocked", []Task{
		{ID: "01-hitl", File: "01-hitl.md", Title: "H", Type: "HITL", Status: "open"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0].BlockedReason != "HITL: 01-hitl" {
		t.Fatalf("blocked reason = %q", result.Rows[0].BlockedReason)
	}
	if result.Rows[0].CompleteHint != "pop tasks complete blocked/01-hitl.md" {
		t.Fatalf("complete hint = %q", result.Rows[0].CompleteHint)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	if !strings.Contains(buf.String(), "complete: pop tasks complete") {
		t.Fatalf("output missing inline complete hint:\n%s", buf.String())
	}
}

func TestUnreadableDiscoveryDoesNotMutateState(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	setupManifest(t, tasksDir, "a", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	if _, err := RefreshWith(DefaultDeps(), tasksDir, statePath); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(tasksDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })

	_, err := RefreshWith(DefaultDeps(), tasksDir, statePath)
	if err == nil {
		t.Fatal("expected error")
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(tasksDir)
	if len(state.Tasks[canon].TaskSets) != 1 {
		t.Fatalf("state mutated: %#v", state.Tasks[canon])
	}
}

// setupManifest writes an Task set into tasksDir, which is the definition
// (Task storage tasks) directory: the set lives at tasksDir/<stem>/.
func setupManifest(t *testing.T, tasksDir, stem string, tasks []Task) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	for _, task := range tasks {
		writeTaskMD(t, taskDir, task.File, "## Acceptance criteria\n\n- [ ] ok\n")
	}
	writeManifest(t, taskDir, tasks)
}

func writeManifest(t *testing.T, taskDir string, tasks []Task) {
	t.Helper()
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"tasks": tasks}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
