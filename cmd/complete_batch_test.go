package cmd

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

// stubCompleteInteractive forces the interactive-terminal check for the test.
func stubCompleteInteractive(t *testing.T, interactive bool) {
	t.Helper()
	prev := completeTaskInteractive
	completeTaskInteractive = func(io.Reader) bool { return interactive }
	t.Cleanup(func() { completeTaskInteractive = prev })
}

// stubCompleteSelect installs a fake Multi-task selection result.
func stubCompleteSelect(t *testing.T, res ui.MultiSelectResult, capture *[]ui.MultiSelectItem) {
	t.Helper()
	prev := completeTaskSelect
	completeTaskSelect = func(_ string, items []ui.MultiSelectItem) (ui.MultiSelectResult, error) {
		if capture != nil {
			*capture = items
		}
		return res, nil
	}
	t.Cleanup(func() { completeTaskSelect = prev })
}

func TestCompleteDispatchByTargetShape(t *testing.T) {
	// complete shares implement's shape dispatch (ADR 0020): a ".md" reference
	// is the single-task path; bare <set> and the <set>/ synonym open the
	// Multi-task selection.
	cases := []struct {
		target   string
		wantFile bool
	}{
		{"demo", false},
		{"demo/", false},
		{"demo/01-a.md", true},
	}
	for _, c := range cases {
		if got := isTaskFileTarget(c.target); got != c.wantFile {
			t.Errorf("isTaskFileTarget(%q) = %v, want %v", c.target, got, c.wantFile)
		}
	}
}

func TestCompleteTasksCmdNonInteractiveRejected(t *testing.T) {
	setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, false)

	err := runTaskCompleteTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), "demo")
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
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)

	var items []ui.MultiSelectItem
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: []int{0}}, &items)

	var stdout bytes.Buffer
	if err := runTaskCompleteTasksWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("batch complete failed: %v", err)
	}

	if len(items) != 1 || !strings.Contains(items[0].Label, "01-a.md") {
		t.Fatalf("selection items = %+v, want a row for 01-a.md", items)
	}
	if !strings.Contains(stdout.String(), "demo/01-a.md: open→done") {
		t.Fatalf("missing batch transition line:\n%s", stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("task not marked done:\n%s", data)
	}
}

func TestCompleteTasksCmdCancelNoWrites(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: false}, nil)

	var stdout bytes.Buffer
	if err := runTaskCompleteTasksWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("cancel should be a clean exit: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("cancel should render nothing, got:\n%s", stdout.String())
	}
	data, _ := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))
	if strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("cancel must not write:\n%s", data)
	}
}

func TestCompleteTasksCmdEmptySelectionNoop(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: nil}, nil)

	var stdout bytes.Buffer
	if err := runTaskCompleteTasksWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("empty selection should be a clean no-op: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("empty selection should render nothing, got:\n%s", stdout.String())
	}
	data, _ := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))
	if strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("empty selection must not write:\n%s", data)
	}
}
