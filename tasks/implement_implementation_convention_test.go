package tasks

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// implementationConventionProse is what the `implementation` Convention stack
// hands the seam: notice-free prose, the way conventions.StackProse renders it.
const implementationConventionProse = "----- ANSWER: SHIPPED (pop's own) -----\n" +
	"conventions/shipped/implementation.md\n\nName things after what they are here.\n"

const planningSourcesConventionProse = "----- ANSWER: REPOSITORY -----\n" +
	"docs/agents/planning-sources.md\n\nOpen tracker references with the tracker CLI.\n"

// includeImplementationConventionConfig is the toggle on with the Refine pass
// off — the independence ADR-0246 asks for: a repository may hold its builders
// to the standard long before it switches the pass on.
func includeImplementationConventionConfig() *config.Config {
	return &config.Config{Work: &config.WorkConfig{
		Implement: &config.ImplementConfig{IncludeImplementationConvention: true},
		Refine:    &config.RefineConfig{Enabled: false},
	}}
}

// plannedAndRemediationSet drains two AFK tasks through the one implement
// prompt: a planned task and a Remediation task, which is the pair the toggle
// promises to cover.
func plannedAndRemediationSet() []Task {
	return []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: fix it", Type: "AFK", Status: "open"},
	}
}

// TestImplementPromptsCarryTheImplementationConvention drives a whole drain
// with the toggle set and reads the prompts the agent was actually handed: both
// the planned task's and the Remediation task's carry the convention as a
// labelled block, notice-free, and the seam is asked about the runtime checkout.
func TestImplementPromptsCarryTheImplementationConvention(t *testing.T) {
	t.Parallel()
	env := setupRunTaskSetFixture(t, "demo", plannedAndRemediationSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{changeFile: "work.txt", changeData: "x\n", checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	var asked []string
	opts.ImplementationConvention = func(cwd string) (string, error) {
		asked = append(asked, cwd)
		return implementationConventionProse, nil
	}

	if _, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return includeImplementationConventionConfig(), nil
	}, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}

	prompts := fakeAgentPrompts(agent)
	if len(prompts) != 2 {
		t.Fatalf("agent saw %d prompts, want one per task:\n%s", len(prompts), strings.Join(prompts, "\n---\n"))
	}
	for i, prompt := range prompts {
		if !strings.Contains(prompt, "## This repository's implementation convention") {
			t.Fatalf("prompt %d carries no convention block:\n%s", i+1, prompt)
		}
		if !strings.Contains(prompt, implementationConventionProse) {
			t.Fatalf("prompt %d does not carry the resolved prose:\n%s", i+1, prompt)
		}
		if strings.Contains(prompt, "READ-WHOLE NOTICE") {
			t.Fatalf("prompt %d carries the Read-whole notice, which belongs to command paths:\n%s", i+1, prompt)
		}
	}

	// One resolution for the whole run, against the checkout being drained.
	if len(asked) != 1 || asked[0] == "" {
		t.Fatalf("convention resolved %v, want exactly one resolution against the runtime checkout", asked)
	}
}

// TestImplementPromptIsUnchangedWithoutTheToggle: the default is off, and off
// means the builder's prompt reads exactly as it did before the toggle existed,
// with the seam wired and never consulted.
func TestImplementPromptIsUnchangedWithoutTheToggle(t *testing.T) {
	t.Parallel()
	tasks := plannedAndRemediationSet()[:1]
	tasks[0].PlanningSources = []string{"request"}
	env := setupRunTaskSetFixtureWithKeys(t, "demo", tasks, map[string]any{
		"planning_sources": []PlanningSource{{ID: "request", Reference: "PROJ-42"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{changeFile: "work.txt", changeData: "x\n", checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	resolved := false
	opts.ImplementationConvention = func(string) (string, error) {
		resolved = true
		return implementationConventionProse, nil
	}
	planningSourcesResolved := false
	opts.PlanningSourcesConvention = func(string) (string, error) {
		planningSourcesResolved = true
		return planningSourcesConventionProse, nil
	}

	if _, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return &config.Config{}, nil
	}, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}

	prompts := fakeAgentPrompts(agent)
	if len(prompts) != 1 {
		t.Fatalf("agent saw %d prompts, want one", len(prompts))
	}
	if got, want := prompts[0], BuildAgentPrompt(nil, parseFakeAgentTaskPath(prompts[0]), runtimeCheckoutOf(t, prompts[0]), ""); got != want {
		t.Fatalf("prompt differs from the untoggled prompt:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if resolved {
		t.Fatal("the implementation convention seam was consulted with the toggle off")
	}
	if planningSourcesResolved {
		t.Fatal("the planning-sources convention seam was consulted with the toggle off")
	}
}

func TestImplementPromptsCarryClaimedPlanningSources(t *testing.T) {
	t.Parallel()
	tasks := plannedAndRemediationSet()
	tasks[0].PlanningSources = []string{"request"}
	tasks[1].PlanningSources = []string{"follow-up"}
	env := setupRunTaskSetFixtureWithKeys(t, "demo", tasks, map[string]any{
		"planning_sources": []PlanningSource{
			{ID: "request", Reference: "PROJ-42", Title: "Original request"},
			{ID: "follow-up", Reference: "https://tracker.example/PROJ-43"},
		},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{changeFile: "work.txt", changeData: "x\n", checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	var asked []string
	opts.PlanningSourcesConvention = func(cwd string) (string, error) {
		asked = append(asked, cwd)
		return planningSourcesConventionProse, nil
	}

	if _, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return &config.Config{Work: &config.WorkConfig{Implement: &config.ImplementConfig{IncludePlanningSources: true}}}, nil
	}, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}

	prompts := fakeAgentPrompts(agent)
	if len(prompts) != 2 {
		t.Fatalf("agent saw %d prompts, want one per task", len(prompts))
	}
	for i, prompt := range prompts {
		for _, want := range []string{
			"## Planning sources for this task",
			"### This repository's planning-sources convention",
			planningSourcesConventionProse,
			"the sole authority on what to build",
			"do not report the divergence",
			"Verifier's job, not the implementer's",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt %d missing %q:\n%s", i+1, want, prompt)
			}
		}
		if strings.Contains(prompt, "READ-WHOLE NOTICE") {
			t.Fatalf("prompt %d carries the command-only notice:\n%s", i+1, prompt)
		}
	}
	if !strings.Contains(prompts[0], "request — Original request: PROJ-42") || strings.Contains(prompts[0], "PROJ-43") {
		t.Fatalf("planned task prompt does not contain only its claim:\n%s", prompts[0])
	}
	if !strings.Contains(prompts[1], "follow-up: https://tracker.example/PROJ-43") || strings.Contains(prompts[1], "PROJ-42") {
		t.Fatalf("Remediation task prompt does not contain only its claim:\n%s", prompts[1])
	}
	if len(asked) != 1 || asked[0] == "" {
		t.Fatalf("convention resolved %v, want once against the runtime checkout", asked)
	}
}

func TestImplementPromptIsUnchangedWhenSetDeclaresNoPlanningSources(t *testing.T) {
	t.Parallel()
	env := setupRunTaskSetFixture(t, "demo", plannedAndRemediationSet()[:1])
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{changeFile: "work.txt", changeData: "x\n", checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.PlanningSourcesConvention = func(string) (string, error) { return planningSourcesConventionProse, nil }
	if _, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return &config.Config{Work: &config.WorkConfig{Implement: &config.ImplementConfig{IncludePlanningSources: true}}}, nil
	}, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}

	prompts := fakeAgentPrompts(agent)
	if len(prompts) != 1 {
		t.Fatalf("agent saw %d prompts, want one", len(prompts))
	}
	if got, want := prompts[0], BuildAgentPrompt(nil, parseFakeAgentTaskPath(prompts[0]), runtimeCheckoutOf(t, prompts[0]), ""); got != want {
		t.Fatalf("source-free prompt differs from today's prompt:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// runtimeCheckoutOf reads the runtime path back out of a prompt, so the
// comparison prompt is rebuilt from the same two paths the drain used.
func runtimeCheckoutOf(t *testing.T, prompt string) string {
	t.Helper()
	const marker = "Runtime checkout: "
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, marker) {
			return strings.TrimPrefix(line, marker)
		}
	}
	t.Fatalf("prompt names no runtime checkout:\n%s", prompt)
	return ""
}
