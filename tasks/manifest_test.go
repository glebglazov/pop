package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestManifestValidation(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "thoughts/issues/demo")
	writeTaskMD(t, taskDir, "01-one.md", "## Acceptance criteria\n\n- [ ] one\n")

	tests := []struct {
		name     string
		manifest string
		valid    bool
		contains string
	}{
		{
			name: "valid manifest",
			manifest: `{
				"tasks": [{
					"id": "01-one",
					"file": "01-one.md",
					"title": "One",
					"type": "AFK",
					"status": "open",
					"blocked_by": []
				}],
				"custom_field": "kept"
			}`,
			valid: true,
		},
		{
			name: "duplicate id",
			manifest: `{"tasks":[
				{"id":"01-one","file":"01-one.md","title":"One","type":"AFK","status":"open","blocked_by":[]},
				{"id":"01-one","file":"01-one.md","title":"Dup","type":"AFK","status":"open","blocked_by":[]}
			]}`,
			valid:    false,
			contains: "duplicate task id",
		},
		{
			name: "in_progress malformed",
			manifest: `{"tasks":[
				{"id":"01-one","file":"01-one.md","title":"One","type":"AFK","status":"in_progress","blocked_by":[]}
			]}`,
			valid:    false,
			contains: "in_progress",
		},
		{
			name: "unresolved blocker",
			manifest: `{"tasks":[
				{"id":"01-one","file":"01-one.md","title":"One","type":"AFK","status":"open","blocked_by":["missing"]}
			]}`,
			valid:    false,
			contains: "unresolved blocker",
		},
	}

	d := DefaultDeps()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(taskDir, "index.json")
			if err := os.WriteFile(path, []byte(tt.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			m := LoadManifest(d, "demo", path)
			if m.Valid != tt.valid {
				t.Fatalf("valid = %v, errors = %v", m.Valid, m.Errors)
			}
			if tt.contains != "" {
				found := false
				for _, e := range m.Errors {
					if strings.Contains(e, tt.contains) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("errors %v missing %q", m.Errors, tt.contains)
				}
			}
		})
	}
}

func TestManifestPreservesUnknownFields(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "thoughts/issues/demo")
	writeTaskMD(t, taskDir, "01-one.md", "## Acceptance criteria\n\n- [ ] one\n")

	path := filepath.Join(taskDir, "index.json")
	original := `{
		"tasks": [{
			"id": "01-one",
			"file": "01-one.md",
			"title": "One",
			"type": "AFK",
			"status": "open",
			"blocked_by": []
		}],
		"generator": "test-suite"
	}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	m := LoadManifest(d, "demo", path)
	if !m.Valid {
		t.Fatalf("unexpected invalid: %v", m.Errors)
	}
	if _, ok := m.Unknown["generator"]; !ok {
		t.Fatal("expected unknown field preserved in memory")
	}

	m.Tasks[0].Status = "done"
	if err := WriteManifestAtomic(d, m); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if string(out["generator"]) != `"test-suite"` {
		t.Fatalf("generator field lost: %s", out["generator"])
	}
}

func TestAcceptanceCriteriaValidation(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "thoughts/issues/demo")

	writeTaskMD(t, taskDir, "no-section.md", "# Task\n")
	writeTaskMD(t, taskDir, "no-boxes.md", "## Acceptance criteria\n\nNothing here\n")
	writeTaskMD(t, taskDir, "good.md", "## Acceptance criteria\n\n- [ ] ok\n")

	cases := map[string]bool{
		"no-section.md": false,
		"no-boxes.md":   false,
		"good.md":       true,
	}

	d := DefaultDeps()
	for file, wantValid := range cases {
		manifest := `{"tasks":[{"id":"x","file":"` + file + `","title":"T","type":"AFK","status":"open","blocked_by":[]}]}`
		path := filepath.Join(taskDir, "index-"+file+".json")
		if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		m := LoadManifest(d, "demo", path)
		if m.Valid != wantValid {
			t.Errorf("%s: valid=%v errors=%v", file, m.Valid, m.Errors)
		}
	}
}

func writeTaskMD(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultStatePathXDG(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return "/xdg/data"
			}
			return ""
		},
	}}
	got := DefaultStatePathWith(d)
	want := "/xdg/data/pop/workloads-state.json"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestDefaultStatePathHomeFallback(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return "/home/me", nil },
	}}
	got := DefaultStatePathWith(d)
	want := "/home/me/.local/share/pop/workloads-state.json"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestCorruptStateRefused(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"version":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadGlobalState(statePath)
	if err == nil {
		t.Fatal("expected corrupt state error")
	}
}

func TestAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	d := DefaultDeps()

	if err := WriteAtomicWith(d, target, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomicWith(d, target, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("data = %q", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".task-tmp-") {
			t.Fatalf("left temp file %s", e.Name())
		}
	}
}
