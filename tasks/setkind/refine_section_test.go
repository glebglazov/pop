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

// refineSectionProse is the one string the summary may never carry.
const refineSectionProse = "SECRET-REFINE-PROSE"

// seedRefinedSet files one Refine report under a fresh set directory and
// hands back the manifest the refresh would carry for it, plus the report's
// path.
func seedRefinedSet(t *testing.T, refined bool) (*tasks.Manifest, string) {
	t.Helper()
	setDir := t.TempDir()
	path := ""
	if refined {
		dir := filepath.Join(setDir, "refine")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path = filepath.Join(dir, "refine-20260816T120000Z.md")
		body := "# Refine report — demo\n\n- Work SHA: abc123a\n- Commit range: aaa111^..HEAD\n\n## Naming\n\n" + refineSectionProse + "\n"
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

// containerForRefinedSet builds the single container a set with this manifest
// produces, over a filesystem that can actually list the set's refine/.
func containerForRefinedSet(t *testing.T, m *tasks.Manifest) work.Container {
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
	refinedManifest, path := seedRefinedSet(t, true)
	refined := containerForRefinedSet(t, refinedManifest)

	if len(refined.DetailSections) != 1 || refined.DetailSections[0].Title != tasks.ArtifactSectionTitle {
		t.Fatalf("detail sections = %+v, want one titled %q", refined.DetailSections, tasks.ArtifactSectionTitle)
	}
	body := refined.DetailSections[0].Body
	for _, want := range []string{"1 artifact", "newest: refine", "2026-08-16 12:00Z"} {
		if !strings.Contains(body, want) {
			t.Fatalf("section body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, path) || strings.Contains(body, "abc123a") || strings.Contains(body, refineSectionProse) {
		t.Fatalf("section retained the Refine pointer or prose:\n%s", body)
	}

	unrefinedManifest, _ := seedRefinedSet(t, false)
	unrefined := containerForRefinedSet(t, unrefinedManifest)
	if len(unrefined.DetailSections) != 0 {
		t.Fatalf("set with no artifacts authored %+v, want no section", unrefined.DetailSections)
	}
}
