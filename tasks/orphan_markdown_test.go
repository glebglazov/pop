package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrphanMarkdownRegistersMalformed drives the whole registration path over a
// set folder holding a slice nobody listed: the fault the check exists for is a
// slice that is silently invisible, so the test asserts the row's status and that
// the diagnostic names the file, not just that validation flagged something.
func TestOrphanMarkdownRegistersMalformed(t *testing.T) {
	d := newTestDeps(t)
	t.Parallel()
	root := t.TempDir()
	taskDir := filepath.Join(root, "orphaned")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] a\n")
	writeTaskMD(t, taskDir, "02-forgotten.md", "## Acceptance criteria\n\n- [ ] b\n")
	writeManifest(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	result, err := RegisterWith(d, root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status != StatusMalformed {
		t.Fatalf("rows = %#v, want one MALFORMED row for the unlisted slice", result.Rows)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	if out := buf.String(); !strings.Contains(out, "02-forgotten.md") {
		t.Fatalf("diagnostics must name the orphan file:\n%s", out)
	}
}

// TestSpecMarkdownIsNotAnOrphan pins the exemptions: the co-located spec, the
// retired name it replaced, and anything that is not markdown at all.
func TestSpecMarkdownIsNotAnOrphan(t *testing.T) {
	d := newTestDeps(t)
	t.Parallel()
	root := t.TempDir()
	taskDir := filepath.Join(root, "with-spec")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] a\n")
	writeManifest(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	for name, body := range map[string]string{
		SpecFileName:       "Source map: 2026-08-03-demo\n",
		legacySpecFileName: "the spec, under its retired name\n",
		"progress.txt":     "not markdown, not a slice\n",
	} {
		if err := os.WriteFile(filepath.Join(taskDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := LoadManifest(d, "with-spec", filepath.Join(taskDir, ManifestFileName))
	if !m.Valid {
		t.Fatalf("spec.md, prd.md and non-markdown must not read as orphans: %v", m.Errors)
	}
}
