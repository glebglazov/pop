// Package prompttest is the golden oracle for agent prompts: it captures what a
// Prompt builder renders today and fails when that text moves.
//
// A golden is what makes the template migration verifiable — a prompt whose
// output is byte-identical before and after is a prompt that did not change,
// which no substring assertion can establish. It lives beside the render seam
// because the whitespace invariant it checks is the seam's contract
// (ADR-0208).
package prompttest

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update-prompt-goldens", false, "rewrite prompt golden files from the current builders")

// Assert compares a rendered prompt against its golden file, writing the golden
// instead when -update-prompt-goldens is set. Run with that flag only after
// reading the resulting diff: a changed golden is a changed briefing.
func Assert(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with -update-prompt-goldens)", path, err)
	}
	if got != string(want) {
		t.Fatalf("prompt does not match %s.\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}

// WhitespaceViolations names every way text breaks the invariant the renderer's
// normalizer enforces: no run of three or more consecutive newlines, no line
// with trailing whitespace, exactly one trailing newline. Empty means clean.
func WhitespaceViolations(text string) []string {
	var problems []string
	if strings.Contains(text, "\n\n\n") {
		problems = append(problems, "has a run of three or more consecutive newlines")
	}
	var trailing []string
	for i, line := range strings.Split(text, "\n") {
		if line != "" && strings.TrimRight(line, " \t") != line {
			trailing = append(trailing, strconv.Itoa(i+1))
		}
	}
	if len(trailing) > 0 {
		problems = append(problems, "has trailing whitespace on line(s) "+strings.Join(trailing, ", "))
	}
	switch {
	case text == "":
		problems = append(problems, "is empty")
	case !strings.HasSuffix(text, "\n"):
		problems = append(problems, "has no trailing newline")
	case strings.HasSuffix(text, "\n\n"):
		problems = append(problems, "has more than one trailing newline")
	}
	return problems
}

// AssertGoldenWhitespace checks every golden under dir against the invariant.
// A file named in known is reported as a pre-existing wart rather than failing:
// this slice froze today's output as-is, so a wart is fixed deliberately in the
// slice that owns that prompt's text, never silently here.
func AssertGoldenWhitespace(t *testing.T, dir string, known map[string]string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no goldens under %s", dir)
	}
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read golden %s: %v", file, err)
		}
		problems := WhitespaceViolations(string(data))
		name := filepath.Base(file)
		if len(problems) == 0 {
			if _, listed := known[name]; listed {
				t.Errorf("%s is listed as a known wart but is clean — drop it from the list", name)
			}
			continue
		}
		if why, listed := known[name]; listed {
			t.Logf("known pre-existing wart in %s (%s): %s", name, why, strings.Join(problems, "; "))
			continue
		}
		t.Errorf("%s %s", name, strings.Join(problems, "; "))
	}
}
