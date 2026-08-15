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

// Both authoring prompts teach the framework contract by rendering the real
// wrapper, so a change to the wrapper's text must reach them without either
// template being touched. The test proves that the only way it can be proved:
// by swapping the wrapper's text and re-rendering.
func TestAuthoringPromptsDeriveContractFromLiveWrapper(t *testing.T) {
	authoringPrompts := func() map[string]string {
		d := routineGoldenDeps(t, "", promptStub)
		return map[string]string{
			"authoring": buildAuthoringPrompt(d, "triage",
				&Routine{ID: "triage", Manifest: Manifest{BoundDirectory: goldenCheckout}}),
			"project authoring": buildProjectAuthoringPrompt(d,
				&ProjectRoutine{Name: "triage", Dir: goldenCheckout, Prompt: promptStub}),
		}
	}

	for name, got := range authoringPrompts() {
		if !strings.Contains(got, "read the routine memory directory") {
			t.Fatalf("%s prompt missing the current wrapper text:\n%s", name, got)
		}
	}

	replaceWrapTemplate(t, "SWAPPED WRAPPER for {{.MemoryDir}}\n")

	for name, got := range authoringPrompts() {
		if !strings.Contains(got, "SWAPPED WRAPPER for ") {
			t.Fatalf("%s prompt did not follow the swapped wrapper:\n%s", name, got)
		}
		if strings.Contains(got, "read the routine memory directory") {
			t.Fatalf("%s prompt still paraphrases the old wrapper:\n%s", name, got)
		}
	}
}

// replaceWrapTemplate redefines the run wrapper's template for one test and
// restores the embedded original afterwards.
func replaceWrapTemplate(t *testing.T, text string) {
	t.Helper()
	original, err := promptTemplateFS.ReadFile("prompts/wrap.tmpl.md")
	if err != nil {
		t.Fatalf("read embedded wrap template: %v", err)
	}
	parse := func(text string) {
		if _, err := promptTemplates.New("wrap.tmpl.md").Parse(text); err != nil {
			t.Fatalf("parse wrap template: %v", err)
		}
	}
	t.Cleanup(func() { parse(string(original)) })
	parse(text)
}
