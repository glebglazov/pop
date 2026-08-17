package setkind

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

func TestArtifactsPublishesClosedListNewestFirst(t *testing.T) {
	defPath := t.TempDir()
	setDir := filepath.Join(defPath, "demo")
	reviewDir := filepath.Join(setDir, reviewsDirName)
	if err := os.MkdirAll(filepath.Join(setDir, "streams", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(setDir, tasks.SpecFileName):                    "spec body\n",
		filepath.Join(setDir, progressFileName):                      "progress body\n",
		filepath.Join(setDir, tasks.ManifestFileName):                "{}",
		filepath.Join(setDir, "01-task.md"):                          "task",
		filepath.Join(setDir, "streams", "runs", "attempt.jsonl.gz"): "run",
		filepath.Join(reviewDir, "review-20260817T120000Z.md"):       "new review",
		filepath.Join(reviewDir, "review-20260815T120000Z.md"):       "old review",
		filepath.Join(reviewDir, "notes.md"):                         "not a review",
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	specAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	progressAt := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(setDir, tasks.SpecFileName), specAt, specAt); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(setDir, progressFileName), progressAt, progressAt); err != nil {
		t.Fatal(err)
	}

	kind := New(&Deps{Tasks: tasks.DefaultDeps()})
	got, err := kind.Artifacts(work.Container{ID: "demo", DefPath: defPath})
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, artifact := range got {
		if !filepath.IsAbs(artifact.Path) {
			t.Fatalf("artifact path is not absolute: %q", artifact.Path)
		}
		rows = append(rows, artifact.Type+":"+artifact.Name)
	}
	want := []string{
		"review:review-20260817T120000Z.md",
		"spec:spec.md",
		"review:review-20260815T120000Z.md",
		"progress:progress.txt",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("artifacts = %v, want %v", rows, want)
	}
}

func TestArtifactsEmptyAndArtifactVerbs(t *testing.T) {
	defPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(defPath, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	kind := New(&Deps{Tasks: tasks.DefaultDeps()})
	got, err := kind.Artifacts(work.Container{ID: "demo", DefPath: defPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("artifacts = %v, want empty", got)
	}

	artifact := work.Artifact{Name: "spec.md", Path: filepath.Join(defPath, "demo", "spec.md")}
	actions := kind.ArtifactActions(work.Container{}, artifact)
	if len(actions) != 2 || actions[0].Verb != work.VerbCopyName || actions[1].Verb != VerbCopyPath {
		t.Fatalf("actions = %+v", actions)
	}
	name, err := kind.PerformArtifact(work.Container{}, artifact, work.VerbCopyName)
	if err != nil || name.Clipboard != artifact.Name {
		t.Fatalf("copy name = %+v, %v", name, err)
	}
	path, err := kind.PerformArtifact(work.Container{}, artifact, VerbCopyPath)
	if err != nil || path.Clipboard != artifact.Path {
		t.Fatalf("copy path = %+v, %v", path, err)
	}
}
