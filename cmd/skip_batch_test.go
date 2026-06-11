package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

// writeSkipTaskThoughts writes a demo set with a mix of statuses so the `skip`
// three-way split (checkable / locked-at-target / inert) is exercised.
func writeSkipTaskThoughts(t *testing.T, tasksDir string) {
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
		`{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"},` +
		`{"id":"02-b","file":"02-b.md","title":"B","type":"AFK","status":"open"},` +
		`{"id":"03-c","file":"03-c.md","title":"C","type":"AFK","status":"skipped"},` +
		`{"id":"04-d","file":"04-d.md","title":"D","type":"AFK","status":"done"}` +
		`]}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupSkipTaskCmdFixture(t *testing.T) string {
	t.Helper()
	root := setupRunTaskCmdFixture(t)
	tasksDir := cmdTasksDir(t, root)
	writeSkipTaskThoughts(t, tasksDir)
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSkipDispatchByTargetShape(t *testing.T) {
	// skip shares implement's shape dispatch (ADR 0020): a ".md" reference is
	// the single-task path; bare <set> and the <set>/ synonym open the
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

func TestSkipTasksCmdNonInteractiveRejected(t *testing.T) {
	setupSkipTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, false)

	err := runTaskSkipTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), "demo")
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

func TestSkipTasksCmdEligibilityAndLockRendering(t *testing.T) {
	setupSkipTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)

	var items []ui.MultiSelectItem
	// Cancel so nothing is written; we only inspect the offered rows.
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: false}, &items)

	if err := runTaskSkipTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("skip selection load failed: %v", err)
	}

	if len(items) != 4 {
		t.Fatalf("items = %d, want 4", len(items))
	}
	// Open rows checkable.
	if items[0].Locked || items[1].Locked {
		t.Fatalf("open rows should be checkable: %+v %+v", items[0], items[1])
	}
	// Skipped locked at-target, Done inert locked, with distinct marks.
	if !items[2].Locked || items[2].LockedMark == "" {
		t.Fatalf("skipped row should be locked-at-target with a mark: %+v", items[2])
	}
	if !items[3].Locked {
		t.Fatalf("done row should be inert-locked: %+v", items[3])
	}
	if items[2].LockedMark == items[3].LockedMark {
		t.Fatalf("at-target and inert marks should differ: skipped=%q done=%q", items[2].LockedMark, items[3].LockedMark)
	}
}

func TestSkipTasksCmdConfirmAppliesBatch(t *testing.T) {
	root := setupSkipTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)

	// Check the two open rows (0 and 1).
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: []int{0, 1}}, nil)

	var stdout bytes.Buffer
	if err := runTaskSkipTasksWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("batch skip failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "demo/01-a.md: open→skipped") {
		t.Fatalf("missing open→skipped transition line:\n%s", out)
	}
	if !strings.Contains(out, "demo/02-b.md: open→skipped") {
		t.Fatalf("missing open→skipped transition line:\n%s", out)
	}

	data, err := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Both deferred; no open tasks remain.
	if strings.Contains(string(data), `"open"`) {
		t.Fatalf("skipped tasks should no longer be open:\n%s", data)
	}
}

func TestSkipTasksCmdCancelNoWrites(t *testing.T) {
	root := setupSkipTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: false}, nil)

	before, _ := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))

	var stdout bytes.Buffer
	if err := runTaskSkipTasksWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("cancel should be a clean exit: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("cancel should render nothing, got:\n%s", stdout.String())
	}
	after, _ := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))
	if string(before) != string(after) {
		t.Fatalf("cancel must not write:\nbefore:%s\nafter:%s", before, after)
	}
}

func TestSkipTasksCmdEmptySelectionNoop(t *testing.T) {
	root := setupSkipTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	stubCompleteInteractive(t, true)
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: nil}, nil)

	before, _ := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))

	var stdout bytes.Buffer
	if err := runTaskSkipTasksWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), "demo"); err != nil {
		t.Fatalf("empty selection should be a clean no-op: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("empty selection should render nothing, got:\n%s", stdout.String())
	}
	after, _ := os.ReadFile(filepath.Join(runTaskCmdDemoDir(t, root), "index.json"))
	if string(before) != string(after) {
		t.Fatalf("empty selection must not write:\nbefore:%s\nafter:%s", before, after)
	}
}
