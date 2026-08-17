package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

func TestTaskArtifactsListsAndShowsDocuments(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	taskDefPath = cmdTasksDir(t, td, root)

	setDir := filepath.Join(taskDefPath, "demo")
	reviewDir := filepath.Join(setDir, "reviews")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(setDir, tasks.ManifestFileName):          `{"tasks":[]}`,
		filepath.Join(setDir, tasks.SpecFileName):              "spec body\nwithout chrome\n",
		filepath.Join(reviewDir, "review-20260817T120000Z.md"): "review body\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var list bytes.Buffer
	if err := runTaskArtifactsWith(td, &list, "demo", ""); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(list.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "review\t2026-08-17T12:00:00Z\treview-20260817T120000Z.md") || !strings.Contains(lines[1], "\tspec.md") {
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
	if err == nil || !strings.Contains(err.Error(), "available: review-20260817T120000Z.md, spec.md") {
		t.Fatalf("unknown artifact error = %v", err)
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
