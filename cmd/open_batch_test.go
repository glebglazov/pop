package cmd

// Batch/agent cmd tests stay serial: they stub package-level
// taskStdinInteractive / runTaskMultiSelect / taskConfigLoad hooks (ADR-0145).


import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

// Smoke layer only (ADR-0144): one happy-path test proves the confirm→apply→
// persist wiring, plus the cmd-only non-interactive guard. The three-way-split
// eligibility, cancel, and empty-selection breadth lives at the tasks domain
// twin (tasks/reset_batch_test.go).

// writeOpenTaskThoughts writes a demo set with a mix of statuses so the `open`
// three-way split (checkable / locked-at-target / inert) is exercised.
func writeOpenTaskThoughts(t *testing.T, tasksDir string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, "demo")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"01-a.md", "02-b.md", "03-c.md", "04-d.md"} {
		if err := os.WriteFile(filepath.Join(taskDir, f), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"tasks":[` +
		`{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"failed"},` +
		`{"id":"02-b","file":"02-b.md","title":"B","type":"AFK","status":"skipped"},` +
		`{"id":"03-c","file":"03-c.md","title":"C","type":"AFK","status":"open"},` +
		`{"id":"04-d","file":"04-d.md","title":"D","type":"AFK","status":"done"}` +
		`]}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupOpenTaskCmdFixture(t *testing.T) (root string, td *tasks.Deps) {
	t.Helper()
	root, td = setupRunTaskCmdFixture(t)
	tasksDir := cmdTasksDir(t, td, root)
	writeOpenTaskThoughts(t, tasksDir)
	if _, err := tasks.RefreshWith(td, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return root, td
}

func TestOpenTasksCmdNonInteractiveRejected(t *testing.T) {
	_, td := setupOpenTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, false)

	err := runTaskOpenTasksWith(td, &bytes.Buffer{}, strings.NewReader(""), "demo")
	if err == nil {
		t.Fatal("whole-set target with no TTY should error")
	}
	ee, ok := err.(*tasks.ExitError)
	if !ok || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v, want ExitOperational", err)
	}
	if !strings.Contains(err.Error(), "demo/<file>.md") {
		t.Fatalf("err should point at the file-reference form: %v", err)
	}
}

func TestOpenTasksCmdConfirmAppliesBatch(t *testing.T) {
	root, td := setupOpenTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)

	// Check the failed (0) and skipped (1) rows.
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: []int{0, 1}}, nil)

	var stdout bytes.Buffer
	if err := runTaskOpenTasksWith(td, &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("batch open failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "demo/01-a.md: failed→open") {
		t.Fatalf("missing failed→open transition line:\n%s", out)
	}
	if !strings.Contains(out, "demo/02-b.md: skipped→open") {
		t.Fatalf("missing skipped→open transition line:\n%s", out)
	}

	data, err := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, td, root), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Both reopened; the prior failed/skipped statuses are gone.
	if strings.Contains(string(data), `"failed"`) || strings.Contains(string(data), `"skipped"`) {
		t.Fatalf("reopened tasks should no longer be failed/skipped:\n%s", data)
	}
}
