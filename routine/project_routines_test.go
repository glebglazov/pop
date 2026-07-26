package routine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
)

// checkoutDeps returns routine Deps whose Project half reports checkout as the
// current git worktree root, so DiscoverProjectRoutines reads
// <checkout>/.pop/routines/. Passing an empty checkout simulates being outside
// any git checkout (git rev-parse fails).
func checkoutDeps(t *testing.T, dataHome, checkout string) *Deps {
	t.Helper()
	d := routineDeps(t, dataHome)
	d.Project = &project.Deps{
		FS: &deps.MockFileSystem{
			GetwdFunc: func() (string, error) {
				if checkout == "" {
					return "/nowhere", nil
				}
				return checkout, nil
			},
		},
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
					if checkout == "" {
						return "", fmt.Errorf("not a git repository")
					}
					return checkout, nil
				}
				return "", fmt.Errorf("unexpected git call: %v", args)
			},
		},
	}
	return d
}

// writeProjectRoutine creates <checkout>/.pop/routines/<name>.md.
func writeProjectRoutine(t *testing.T, checkout, name, content string) {
	t.Helper()
	dir := filepath.Join(checkout, ".pop", "routines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverProjectRoutinesParsesAgentsAndEffort(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := checkoutDeps(t, dataHome, checkout)

	writeProjectRoutine(t, checkout, "newrelic", "---\nagents:\n  - claude\n  - codex\neffort: heavy\n---\nResearch NewRelic bugs.\n")

	routines, warnings := DiscoverProjectRoutines(d)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(routines) != 1 {
		t.Fatalf("routines = %d, want 1", len(routines))
	}
	r := routines[0]
	if r.Name != "newrelic" {
		t.Fatalf("name = %q", r.Name)
	}
	if strings.Join(r.Agents, ",") != "claude,codex" {
		t.Fatalf("agents = %v", r.Agents)
	}
	if r.Effort != "heavy" {
		t.Fatalf("effort = %q", r.Effort)
	}
	if strings.TrimSpace(r.Prompt) != "Research NewRelic bugs." {
		t.Fatalf("prompt = %q", r.Prompt)
	}
	if r.Dir != checkout {
		t.Fatalf("dir = %q, want %q", r.Dir, checkout)
	}
}

func TestDiscoverProjectRoutinesWarnsScheduleAndUnknownKeys(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := checkoutDeps(t, dataHome, checkout)

	writeProjectRoutine(t, checkout, "scheduled", "---\nschedule: every 6h\nagents:\n  - claude\nmystery: 1\n---\nBody here.\n")

	routines, warnings := DiscoverProjectRoutines(d)
	// The routine still lists, minus the dropped keys.
	if len(routines) != 1 {
		t.Fatalf("routines = %d, want 1", len(routines))
	}
	if strings.Join(routines[0].Agents, ",") != "claude" {
		t.Fatalf("agents = %v, want [claude]", routines[0].Agents)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one", warnings)
	}
	msg := warnings[0].Err.Error()
	if !strings.Contains(msg, "schedule") || !strings.Contains(msg, "mystery") {
		t.Fatalf("warning should name both dropped keys, got %q", msg)
	}
}

func TestDiscoverProjectRoutinesInvalidFilenameAndUnparseableWarn(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := checkoutDeps(t, dataHome, checkout)

	// Valid routine that must still survive alongside broken siblings.
	writeProjectRoutine(t, checkout, "good", "---\n---\nDo the thing.\n")
	// Invalid filename (path separator is impossible on disk, but "." is a
	// reserved id the authored-id rules reject).
	writeProjectRoutine(t, checkout, ".", "body\n")
	// Unterminated frontmatter fence → parse error.
	writeProjectRoutine(t, checkout, "broken", "---\nagents: [claude]\nno closing fence\n")

	routines, warnings := DiscoverProjectRoutines(d)
	if len(routines) != 1 || routines[0].Name != "good" {
		t.Fatalf("routines = %v, want just good", routines)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want two (invalid name + unparseable)", warnings)
	}
}

func TestDiscoverProjectRoutinesOutsideCheckoutFindsNothing(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	d := checkoutDeps(t, dataHome, "") // git rev-parse fails ⇒ not in a checkout

	routines, warnings := DiscoverProjectRoutines(d)
	if len(routines) != 0 || len(warnings) != 0 {
		t.Fatalf("outside a checkout expected nothing, got routines=%v warnings=%v", routines, warnings)
	}
}

func TestListWithAppendsProjectRoutines(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := checkoutDeps(t, dataHome, checkout)
	if _, err := AddWith(d, "authored", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}
	writeProjectRoutine(t, checkout, "shared", "---\n---\nShared prompt.\n")

	var out bytes.Buffer
	if err := ListWith(d, &out); err != nil {
		t.Fatalf("ListWith: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "authored") {
		t.Fatalf("output missing authored routine:\n%s", text)
	}
	// project: origin marker, manual schedule, `-` pause column.
	line := projectLine(t, text, "project:shared")
	if !strings.Contains(line, "manual") {
		t.Fatalf("project routine line missing manual schedule: %q", line)
	}
	fields := strings.Fields(line)
	if fields[len(fields)-1] != "-" {
		t.Fatalf("project routine pause column = %q, want -; line %q", fields[len(fields)-1], line)
	}
	if !strings.Contains(line, checkout) {
		t.Fatalf("project routine line missing checkout dir: %q", line)
	}
}

func TestListWithDiscoveryWritesNothingToDataDir(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := checkoutDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "shared", "---\n---\nShared prompt.\n")

	// Discovery + list of a project-only checkout registers nothing.
	var out bytes.Buffer
	if err := ListWith(d, &out); err != nil {
		t.Fatalf("ListWith: %v", err)
	}

	// The data-dir registry — the only thing the Queue daemon discovers via
	// ListRoutines — sees no project routine at all.
	registry, warnings, err := ListRoutines(d)
	if err != nil {
		t.Fatalf("ListRoutines: %v", err)
	}
	if len(registry) != 0 || len(warnings) != 0 {
		t.Fatalf("daemon registry = %v (warnings %v), want empty — project routines must be invisible to it", registry, warnings)
	}
	// Nothing was materialized into the data-dir routines/ registry directory.
	if entries, err := os.ReadDir(filepath.Join(dataHome, "pop", "routines")); err == nil && len(entries) != 0 {
		t.Fatalf("data-dir routines registry was written to by discovery: %v", entries)
	}
}

func TestListWithOutsideCheckoutUnchanged(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// Same authored routine, once discovered outside a checkout and once with the
	// default (no Project) deps: output must be byte-identical.
	d := checkoutDeps(t, dataHome, "")
	if _, err := AddWith(d, "authored", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ListWith(d, &out); err != nil {
		t.Fatalf("ListWith: %v", err)
	}
	if strings.Contains(out.String(), ProjectOrigin) {
		t.Fatalf("outside a checkout, list must not mark any project origin:\n%s", out.String())
	}
}

func projectLine(t *testing.T, text, needle string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line containing %q in:\n%s", needle, text)
	return ""
}
