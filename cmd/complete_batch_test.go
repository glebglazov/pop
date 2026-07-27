package cmd

// Batch/agent cmd tests stay serial: they stub package-level
// taskStdinInteractive / runTaskMultiSelect / taskConfigLoad hooks (ADR-0145).


import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

// Smoke layer only (ADR-0144): one happy-path test proves the confirm→apply→
// persist wiring, plus the cmd-only non-interactive guard. The eligibility,
// cancel, and empty-selection breadth lives at the tasks domain twin
// (tasks/complete_batch_test.go). The stubCompleteInteractive/stubCompleteSelect
// helpers below are shared with the skip and open batch smoke tests.

// stubCompleteInteractive forces the interactive-terminal check for the test.
func stubCompleteInteractive(t *testing.T, interactive bool) {
	t.Helper()
	prev := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return interactive }
	t.Cleanup(func() { taskStdinInteractive = prev })
}

// stubCompleteSelect installs a fake Multi-task selection result.
func stubCompleteSelect(t *testing.T, res ui.MultiSelectResult, capture *[]ui.MultiSelectItem) {
	t.Helper()
	prev := runTaskMultiSelect
	runTaskMultiSelect = func(_ string, items []ui.MultiSelectItem) (ui.MultiSelectResult, error) {
		if capture != nil {
			*capture = items
		}
		return res, nil
	}
	t.Cleanup(func() { runTaskMultiSelect = prev })
}

func TestCompleteTasksCmdNonInteractiveRejected(t *testing.T) {
	_, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, false)

	err := runTaskCompleteTasksWith(td, &bytes.Buffer{}, strings.NewReader(""), "demo")
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

func TestCompleteTasksCmdConfirmAppliesBatch(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)

	var items []ui.MultiSelectItem
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: []int{0}}, &items)

	var stdout bytes.Buffer
	if err := runTaskCompleteTasksWith(td, &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("batch complete failed: %v", err)
	}

	if len(items) != 1 || !strings.Contains(items[0].Label, "01-a.md") {
		t.Fatalf("selection items = %+v, want a row for 01-a.md", items)
	}
	if !strings.Contains(stdout.String(), "demo/01-a.md: open→done") {
		t.Fatalf("missing batch transition line:\n%s", stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, td, root), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("task not marked done:\n%s", data)
	}
}
