package routine

import (
	"strings"
	"testing"
)

func TestWrapRoutinePromptIncludesMemoryAndReportPaths(t *testing.T) {
	got := wrapRoutinePrompt("/data/routines/demo/memory", "/data/routines/demo/runs/2026-07-18T10-00-00Z.md", "Check errors.")
	for _, want := range []string{
		"/data/routines/demo/memory",
		"read the routine memory directory",
		"/data/routines/demo/runs/2026-07-18T10-00-00Z.md",
		"write your report to",
		"Check errors.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
}
