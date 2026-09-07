package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// exploreDrainSet is one open AFK task: draining it selects that task, so an
// Explore pass that saw it still open ran before the drain selected anything.
func exploreDrainSet() []Task {
	return []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}}
}

// setupDrainExploreFixture is setupRunTaskSetFixture with the set-level keys the
// Explore step reads written into the registered manifest.
func setupDrainExploreFixture(t *testing.T, setKeys map[string]any) *runTaskSetFixture {
	t.Helper()
	env := setupRunTaskSetFixture(t, "demo", exploreDrainSet())
	if len(setKeys) > 0 {
		writeManifestWithSetKeys(t, filepath.Join(env.tasksDir, "demo"), exploreDrainSet(), setKeys)
	}
	return env
}

func explorationReportOf(t *testing.T, env *runTaskSetFixture) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(env.tasksDir, "demo", ExplorationFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read exploration report: %v", err)
	}
	return string(body)
}

// exploreRefusingRunner fails the test if the drain spawns an Explorer at all.
func exploreRefusingRunner(t *testing.T, why string) func(string) (string, string, error) {
	t.Helper()
	return func(string) (string, string, error) {
		t.Fatalf("%s must spawn no Explorer", why)
		return "", "", nil
	}
}

// TestDrainExploresADeclaredSetThatHasNoReportYet drives the whole step: a set
// carrying the Explore directive and no report is explored at the head of the
// drain — while its first task is still open — and the drain then builds that
// task with the report on disk. No configuration is loaded at all, which is the
// group's default-on arm: absent configuration explores.
func TestDrainExploresADeclaredSetThatHasNoReportYet(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	calls := 0
	var statusAtExplore []string
	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = func(string) (string, string, error) {
		calls++
		m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
		for _, task := range m.Tasks {
			statusAtExplore = append(statusAtExplore, task.ID+"="+string(task.Status))
		}
		return "## Where things live\n\n`tasks/explore.go` owns the pass.", "claude", nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Explorer invocations = %d, want exactly one for the whole drain", calls)
	}
	// Placement: the pass ran while the set's only AFK task was still open, so
	// no builder started before the map it is handed existed.
	if strings.Join(statusAtExplore, ",") != "01-a=open" {
		t.Fatalf("task states when the Explorer ran = %v, want the set untouched", statusAtExplore)
	}

	report := explorationReportOf(t, env)
	for _, want := range []string{"# Exploration report — demo", "Explorer: claude", "`tasks/explore.go` owns the pass."} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the explored set drained to DONE", result)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

// TestDrainReusesTheReportADeclaredSetAlreadyHas: present means reuse, and the
// report's recorded tree is prose nobody reads back — the SHA it names is not
// this checkout's, and the drain still spends nothing on exploring.
func TestDrainReusesTheReportADeclaredSetAlreadyHas(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	existing := "# Exploration report — demo\n\n- Explored: 2020-01-01T00:00:00Z\n- Work SHA: 0000000000000000000000000000000000000000\n\n## Where things live\n\nThe map of an older tree.\n"
	writeTaskMD(t, filepath.Join(env.tasksDir, "demo"), ExplorationFileName, existing)
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = exploreRefusingRunner(t, "a set that already has a report")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if got := explorationReportOf(t, env); got != existing {
		t.Fatalf("the reused report was rewritten:\n%s", got)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the set drained to DONE", result)
	}
}

// TestDrainNeverExploresASetThatDidNotAskForIt: the directive is the whole
// participation trigger, so a set carrying none is untouched by a step whose
// group is on.
func TestDrainNeverExploresASetThatDidNotAskForIt(t *testing.T) {
	env := setupDrainExploreFixture(t, nil)
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = exploreRefusingRunner(t, "a set that never declared exploration")

	if _, err := RunTaskSetWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if got := explorationReportOf(t, env); got != "" {
		t.Fatalf("an undeclared set was explored:\n%s", got)
	}
}

// TestDrainSkipsExploreForAHumanCompletedSet: the drain does not explore a set
// a human declared done, as it does not refine one — the pass exists to shape
// work about to be built, and there is none.
func TestDrainSkipsExploreForAHumanCompletedSet(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true, "human_completed": true})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = exploreRefusingRunner(t, "a human-completed set")

	if _, err := RunTaskSetWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if got := explorationReportOf(t, env); got != "" {
		t.Fatalf("a human-completed set was explored:\n%s", got)
	}
}

// TestExploreGroupOffStopsTheDrainStepButNotTheHandRun: `enabled = false` is the
// whole-machine switch, and it gates only the automatic step — a human asking
// for the same set by hand still gets a report, the way a disabled Refine group
// still answers `pop tasks refine`.
func TestExploreGroupOffStopsTheDrainStepButNotTheHandRun(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})
	off := false
	cfg := &config.Config{Work: &config.WorkConfig{Explore: &config.ExploreConfig{Enabled: &off}}}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = exploreRefusingRunner(t, "a drain with [work.explore] switched off")

	if _, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) { return cfg, nil }, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if got := explorationReportOf(t, env); got != "" {
		t.Fatalf("a disabled group explored:\n%s", got)
	}

	if _, err := exploreResolvedSet(env.deps(), cfg, exploreCoreOptions{
		DefPath:     env.tasksDir,
		RuntimePath: env.root,
		SetID:       "demo",
		Output:      &buf,
		runExplorer: func(string) (string, string, error) { return "## Where things live\n\nAsked for by hand.", "claude", nil },
	}); err != nil {
		t.Fatalf("exploreResolvedSet: %v", err)
	}
	if !strings.Contains(explorationReportOf(t, env), "Asked for by hand.") {
		t.Fatal("a hand run must write the report whatever the group says")
	}
}
