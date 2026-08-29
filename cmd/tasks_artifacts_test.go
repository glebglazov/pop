package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/work"
)

func TestTaskArtifactsListsAndShowsDocuments(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	taskDefPath = cmdTasksDir(t, td, root)

	setDir := filepath.Join(taskDefPath, "demo")
	refineDir := filepath.Join(setDir, "refine")
	if err := os.MkdirAll(refineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(setDir, tasks.ManifestFileName):          `{"tasks":[]}`,
		filepath.Join(setDir, tasks.SpecFileName):              "spec body\nwithout chrome\n",
		filepath.Join(refineDir, "refine-20260817T120000Z.md"): "refine body\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	specAt := time.Date(2026, 8, 17, 11, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(setDir, tasks.SpecFileName), specAt, specAt); err != nil {
		t.Fatal(err)
	}

	var list bytes.Buffer
	if err := runTaskArtifactsWith(td, &list, "demo", ""); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(list.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "refine\t2026-08-17T12:00:00Z\trefine-20260817T120000Z.md") || !strings.Contains(lines[1], "\tspec.md") {
		t.Fatalf("artifact list = %q", list.String())
	}

	var shown bytes.Buffer
	if err := runTaskArtifactsWith(td, &shown, "demo", tasks.SpecFileName); err != nil {
		t.Fatal(err)
	}
	if got, want := shown.String(), "spec body\nwithout chrome\n"; got != want {
		t.Fatalf("shown artifact = %q, want %q", got, want)
	}

	err := runTaskArtifactsWith(td, &bytes.Buffer{}, "demo", "missing.md")
	if err == nil || !strings.Contains(err.Error(), "available: refine-20260817T120000Z.md, spec.md") {
		t.Fatalf("unknown artifact error = %v", err)
	}
}

// The CLI listing and the dashboard's Artifact view read one order out of the
// Task-set kind (ADR-0220), so a progress record rewritten seconds ago cannot
// take the top row from a refine report on either surface.
func TestTaskArtifactsListsTypeTierOrderTheDashboardAlsoReads(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	taskDefPath = cmdTasksDir(t, td, root)

	setDir := filepath.Join(taskDefPath, "demo")
	refineDir := filepath.Join(setDir, "refine")
	if err := os.MkdirAll(refineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(setDir, tasks.ManifestFileName):          `{"tasks":[]}`,
		filepath.Join(setDir, tasks.SpecFileName):              "spec body\n",
		filepath.Join(setDir, "progress.txt"):                  "just drained\n",
		filepath.Join(refineDir, "refine-20260817T120000Z.md"): "new report\n",
		filepath.Join(refineDir, "refine-20260815T120000Z.md"): "old report\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Both singletons are more recently modified than either refine report.
	specAt := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	progressAt := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(setDir, tasks.SpecFileName), specAt, specAt); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(setDir, "progress.txt"), progressAt, progressAt); err != nil {
		t.Fatal(err)
	}

	var list bytes.Buffer
	if err := runTaskArtifactsWith(td, &list, "demo", ""); err != nil {
		t.Fatal(err)
	}
	printed := make([]string, 0, 4)
	for _, line := range strings.Split(strings.TrimSpace(list.String()), "\n") {
		fields := strings.Split(line, "\t")
		printed = append(printed, fields[0]+":"+fields[len(fields)-1])
	}
	want := []string{
		"refine:refine-20260817T120000Z.md",
		"refine:refine-20260815T120000Z.md",
		"spec:spec.md",
		"progress:progress.txt",
	}
	if !reflect.DeepEqual(printed, want) {
		t.Fatalf("pop tasks artifacts order = %v, want %v", printed, want)
	}

	// The same seam the dashboard's Artifact view reads for a Task-set row.
	var source work.ArtifactSource = setkind.New(&setkind.Deps{Tasks: td})
	rows, err := source.Artifacts(work.Container{ID: "demo", DefPath: taskDefPath})
	if err != nil {
		t.Fatal(err)
	}
	viewed := make([]string, 0, len(rows))
	for _, artifact := range rows {
		viewed = append(viewed, artifact.Type+":"+artifact.Name)
	}
	if !reflect.DeepEqual(viewed, printed) {
		t.Fatalf("Artifact view order = %v, want the CLI order %v", viewed, printed)
	}
}

func TestTaskArtifactsReportsEmptySet(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	taskDefPath = cmdTasksDir(t, td, root)
	setDir := filepath.Join(taskDefPath, "empty")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, tasks.ManifestFileName), []byte(`{"tasks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runTaskArtifactsWith(td, &out, "empty", ""); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "Task set empty has no artifacts.\n"; got != want {
		t.Fatalf("empty output = %q, want %q", got, want)
	}
}
