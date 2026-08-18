package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// A document shown to a human on a terminal is rendered; the same command
// redirected is the file's bytes, so piping a review into a file or a pull request
// is unaffected (ADR-0222). Extension decides: progress.txt keeps its `---`
// separators on a terminal too.
func TestTaskArtifactsShowRendersMarkdownOnlyForATerminal(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	taskDefPath = cmdTasksDir(t, td, root)

	setDir := filepath.Join(taskDefPath, "demo")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "# Spec\n\n- first finding\n- second finding\n"
	progress := "12:00 [01-task.md] done\n\n---\n\n12:30 [02-task.md] done\n"
	for path, body := range map[string]string{
		filepath.Join(setDir, tasks.ManifestFileName): `{"tasks":[]}`,
		filepath.Join(setDir, tasks.SpecFileName):     spec,
		filepath.Join(setDir, tasks.ProgressFileName): progress,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	interactive := true
	originalInteractive, originalWidth := taskStdoutInteractive, taskStdoutWidth
	t.Cleanup(func() { taskStdoutInteractive, taskStdoutWidth = originalInteractive, originalWidth })
	taskStdoutInteractive = func() bool { return interactive }
	taskStdoutWidth = func() int { return 60 }

	show := func(t *testing.T, name string) string {
		t.Helper()
		var out bytes.Buffer
		if err := runTaskArtifactsWith(td, &out, "demo", name); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	rendered := show(t, tasks.SpecFileName)
	if strings.Contains(rendered, "- first finding") || !strings.Contains(rendered, "first finding") {
		t.Fatalf("markdown artifact was not rendered on a terminal:\n%q", rendered)
	}

	if raw := show(t, tasks.ProgressFileName); raw != progress {
		t.Fatalf("progress record was not shown raw on a terminal:\n%q", raw)
	}

	interactive = false
	if redirected := show(t, tasks.SpecFileName); redirected != spec {
		t.Fatalf("redirected --show = %q, want the file's bytes %q", redirected, spec)
	}
}
