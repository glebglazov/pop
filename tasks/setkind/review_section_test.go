package setkind

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// reviewSectionProse is the one string the summary may never carry.
const reviewSectionProse = "SECRET-REVIEW-PROSE"

// seedReviewedSet files one Review artifact under a fresh set directory and
// hands back the manifest the refresh would carry for it, plus the artifact's
// path.
func seedReviewedSet(t *testing.T, reviewed bool) (*tasks.Manifest, string) {
	t.Helper()
	setDir := t.TempDir()
	path := ""
	if reviewed {
		dir := filepath.Join(setDir, "reviews")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path = filepath.Join(dir, "review-20260816T120000Z.md")
		body := "# Code review — demo\n\n- Work SHA: abc123a\n- Commit range: aaa111^..HEAD\n\n## Naming\n\n" + reviewSectionProse + "\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := &tasks.Manifest{
		Valid: true,
		Dir:   setDir,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}},
	}
	return m, path
}

// containerForReviewedSet builds the single container a set with this manifest
// produces, over a filesystem that can actually list the set's reviews/.
func containerForReviewedSet(t *testing.T, m *tasks.Manifest) work.Container {
	t.Helper()
	rows := []tasks.Row{{ID: "demo", Status: tasks.StatusReady}}
	d := testDeps(t, rows)
	real := deps.NewRealFileSystem()
	d.Tasks.FS = &deps.MockFileSystem{
		EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
		ReadDirFunc:      real.ReadDir,
		ReadFileFunc:     real.ReadFile,
		StatFunc:         func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}
	d.Refresh = func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{Rows: rows, Manifests: map[string]*tasks.Manifest{"demo": m}}, nil
	}
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}
	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("containers = %+v, want one", got)
	}
	return got[0]
}

// TestDetailSectionSummarisesArtifacts pins the dashboard consequence of
// ADR-0217: it gives the Artifact count and newest member, but no path.
func TestDetailSectionSummarisesArtifacts(t *testing.T) {
	reviewedManifest, path := seedReviewedSet(t, true)
	reviewed := containerForReviewedSet(t, reviewedManifest)

	if len(reviewed.DetailSections) != 1 || reviewed.DetailSections[0].Title != tasks.ArtifactSectionTitle {
		t.Fatalf("detail sections = %+v, want one titled %q", reviewed.DetailSections, tasks.ArtifactSectionTitle)
	}
	body := reviewed.DetailSections[0].Body
	for _, want := range []string{"1 artifact", "newest: review", "2026-08-16 12:00Z"} {
		if !strings.Contains(body, want) {
			t.Fatalf("section body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, path) || strings.Contains(body, "abc123a") || strings.Contains(body, reviewSectionProse) {
		t.Fatalf("section retained the Review pointer or prose:\n%s", body)
	}

	unreviewedManifest, _ := seedReviewedSet(t, false)
	unreviewed := containerForReviewedSet(t, unreviewedManifest)
	if len(unreviewed.DetailSections) != 0 {
		t.Fatalf("set with no artifacts authored %+v, want no section", unreviewed.DetailSections)
	}
}
