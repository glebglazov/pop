package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Drain header is what tells a human who ran a drain where it went. It
// prints on every whole-set run, before anything else — the old report only
// spoke up when the drain surprised it, which meant the common case said nothing
// at all. The runs below drive real drains and read the top of their output.

// TestDrainOpensWithItsHeader: draining from inside the bound checkout still
// states the set, the Runtime path, and the binding kind, and states them ahead
// of the status table.
func TestDrainOpensWithItsHeader(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.DrainHeader = DrainHeader{Binding: DrainHeaderAdopted}

	if _, err := RunTaskSetWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	runtimePath, err := ResolveRuntimePathWith(env.deps(), env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	want := "drain demo at " + runtimePath + " (adopted)"
	if !strings.HasPrefix(out, want+"\n") {
		t.Fatalf("drain output does not open with %q:\n%s", want, out)
	}
	if i, j := strings.Index(out, want), strings.Index(out, "STATUS"); j >= 0 && i > j {
		t.Fatalf("header printed after the status table:\n%s", out)
	}
	if strings.Contains(out, "invoked from") {
		t.Fatalf("header names an invocation directory it ran in:\n%s", out)
	}
	if strings.Contains(out, "\033[") {
		t.Fatalf("redirected output carries color escapes:\n%q", out)
	}
}

// TestDrainHeaderNamesWhereItWasInvokedAndWhatItBound: run from a directory that
// is not the Runtime path, and having just recorded the set's Default binding,
// the header adds both facts under the opening line.
func TestDrainHeaderNamesWhereItWasInvokedAndWhatItBound(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	sub := filepath.Join(env.root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.CWD = sub
	opts.DrainHeader = DrainHeader{Binding: DrainHeaderManaged, RecordedDefaultBinding: true}

	if _, err := RunTaskSetWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"(managed)", "invoked from " + sub, "binding recorded to current checkout"} {
		if !strings.Contains(out, want) {
			t.Fatalf("header missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "invoked from") > strings.Index(out, "STATUS") {
		t.Fatalf("header lines printed after the status table:\n%s", out)
	}
}

// TestSingleTaskRunSaysItRunsInTheCurrentCheckout: a single task-file run has no
// drain to route, so it states the current checkout in the header's place and
// claims no binding.
func TestSingleTaskRunSaysItRunsInTheCurrentCheckout(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runOpts(true, agent)
	opts.Output = &buf

	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	runtimePath, err := ResolveRuntimePathWith(env.deps(), env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "running in current checkout "+runtimePath+"\n") {
		t.Fatalf("single-task run does not open with its current-checkout line:\n%s", out)
	}
	if strings.Contains(out, "drain demo at ") {
		t.Fatalf("single-task run printed a Drain header:\n%s", out)
	}
}

// TestDrainHeaderColorsOnlyOnATerminal: the header goes through the drain output
// layer, so a colored writer styles it and NO_COLOR turns that off — the same
// rule every other pop-owned line follows.
func TestDrainHeaderColorsOnlyOnATerminal(t *testing.T) {
	var colored bytes.Buffer
	renderDrainHeader(&output{Writer: &colored, color: true}, "demo", "/repo", "/elsewhere", DrainHeader{Binding: DrainHeaderAdopted})
	if !strings.Contains(colored.String(), ansiBold) || !strings.Contains(colored.String(), ansiDim) {
		t.Fatalf("terminal output is unstyled:\n%q", colored.String())
	}

	t.Setenv("NO_COLOR", "1")
	if colorEnabled(true) {
		t.Fatal("NO_COLOR left color enabled on a terminal")
	}
}
