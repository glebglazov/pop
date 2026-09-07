package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// exploreParkFixture is a registered "demo" set that declares its tasks
// interrelated, with a scripted agent process standing in for the Explorer so a
// pass runs the real agent walk — its retry cap, and the Captured run each
// attempt is filed as, which is what the park is derived from.
func exploreParkFixture(t *testing.T, runs ...scriptedRefineRun) (*runTaskSetFixture, *Deps, *scriptedRefineRunner) {
	t.Helper()
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	runner := &scriptedRefineRunner{runs: runs}
	d := env.deps()
	d.LookPath = func(string) (string, error) { return "/bin/claude", nil }
	d.Runner = &probeNeutralRunner{inner: runner}
	return env, d, runner
}

// exploreParkRows refreshes the fixture's status table the way every read
// surface does.
func exploreParkRows(t *testing.T, env *runTaskSetFixture) *RefreshResult {
	t.Helper()
	refresh, err := RefreshWith(env.deps(), env.tasksDir, DefaultStatePathWith(env.deps()))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	return refresh
}

func exploreParkStatus(t *testing.T, env *runTaskSetFixture) TaskSetStatus {
	t.Helper()
	row := findRow(exploreParkRows(t, env), "demo")
	if row == nil {
		t.Fatal("no row for demo")
	}
	return row.Status
}

// writeExploreRunMeta files one Captured run of phase `explore` with the given
// ending, standing in for a pass whose walk ended that way.
func writeExploreRunMeta(t *testing.T, setDir, outcome string, at time.Time) {
	t.Helper()
	dir := capturedRunsDir(setDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runID := uuid.New().String()
	meta := capturedRunMeta{
		RunID:     runID,
		Phase:     spendPhaseExplore,
		TaskSetID: "demo",
		StartTime: at.UTC(),
		EndTime:   at.Add(time.Minute).UTC(),
		Outcome:   outcome,
		Agent:     "claude",
		Attempt:   1,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestASetWhoseExplorePassGaveUpParksAndIsLeftAlone drives the whole park: a
// declared set is explored by a walk that spends its cap without answering, the
// status table reads the park rather than READY, automatic selection passes over
// it, and a second hand pass that answers clears it — with no store row anywhere
// in between, only the runs the walk filed and the document it did not write.
func TestASetWhoseExplorePassGaveUpParksAndIsLeftAlone(t *testing.T) {
	crashed := scriptedRefineRun{output: claudeRefineStream("## Where things"), runErr: errors.New("agent crashed")}
	env, d, runner := exploreParkFixture(t, crashed, crashed, crashed,
		scriptedRefineRun{output: claudeRefineStream("## Where things live\\n\\n`tasks/explore.go` owns the pass.")})

	if got := exploreParkStatus(t, env); got != StatusReady {
		t.Fatalf("status before any pass = %s, want %s", got, StatusReady)
	}

	var out bytes.Buffer
	opts := exploreCoreOptions{DefPath: env.tasksDir, RuntimePath: env.root, SetID: "demo", Timeout: time.Minute, Output: &out}
	if _, err := exploreResolvedSet(d, nil, opts); err == nil {
		t.Fatal("a pass that produced no report returned no error")
	}
	// A park is a considered outcome: the pass spent the whole per-phase cap on
	// it before it gave up.
	if runner.calls != 3 {
		t.Fatalf("Explorer invocations = %d, want the whole retry cap spent first", runner.calls)
	}
	if got := explorationReportOf(t, env); got != "" {
		t.Fatalf("a pass that gave up wrote a report:\n%s", got)
	}

	refresh := exploreParkRows(t, env)
	row := findRow(refresh, "demo")
	// The park is its own word, not the one that means a human owes a decision.
	if row.Status != StatusExploreFailed {
		t.Fatalf("status after the pass gave up = %s, want %s", row.Status, StatusExploreFailed)
	}
	if _, _, err := selectAutomaticTaskSet(refresh); err == nil {
		t.Fatal("automatic selection picked a parked set")
	}

	// Door one: a hand pass that answers. Nothing clears the park but the report.
	if _, err := exploreResolvedSet(d, nil, opts); err != nil {
		t.Fatalf("hand-run explore: %v", err)
	}
	if got := exploreParkStatus(t, env); got != StatusReady {
		t.Fatalf("status after a successful hand pass = %s, want %s", got, StatusReady)
	}
}

// TestRetractingTheExploreDeclarationClearsThePark: door three. The directive is
// the whole participation trigger, so withdrawing it withdraws the gate with it,
// and the runs the failed pass left behind stop meaning anything.
func TestRetractingTheExploreDeclarationClearsThePark(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	setDir := filepath.Join(env.tasksDir, "demo")
	writeExploreRunMeta(t, setDir, streamOutcomeFailed, time.Now())

	if got := exploreParkStatus(t, env); got != StatusExploreFailed {
		t.Fatalf("status = %s, want %s", got, StatusExploreFailed)
	}

	writeManifestWithSetKeys(t, setDir, exploreDrainSet(), map[string]any{"explore": false})
	if got := exploreParkStatus(t, env); got != StatusReady {
		t.Fatalf("status after retracting the declaration = %s, want %s", got, StatusReady)
	}
}

// TestOnlyAnExplorePassThatAnsweredForItselfParks pins the one distinction the
// park turns on: an Explorer that reached an ending of its own and left no
// report gave up, while a run the machinery stopped is a pass that never
// happened and a condition a later drain finds gone.
func TestOnlyAnExplorePassThatAnsweredForItselfParks(t *testing.T) {
	tests := []struct {
		outcome string
		want    TaskSetStatus
	}{
		{streamOutcomeCompleted, StatusExploreFailed},
		{streamOutcomeFailed, StatusExploreFailed},
		{streamOutcomeTimedOut, StatusExploreFailed},
		{streamOutcomeTurnCapExhausted, StatusExploreFailed},
		{streamOutcomeInterrupted, StatusReady},
		{streamOutcomeQuotaPaused, StatusReady},
		{streamOutcomeAgentUnusable, StatusReady},
		{streamOutcomeModelSkipped, StatusReady},
	}
	for _, tt := range tests {
		t.Run(tt.outcome, func(t *testing.T) {
			env := setupDrainExploreFixture(t, map[string]any{"explore": true})
			writeExploreRunMeta(t, filepath.Join(env.tasksDir, "demo"), tt.outcome, time.Now())
			if got := exploreParkStatus(t, env); got != tt.want {
				t.Fatalf("status after an %s explore run = %s, want %s", tt.outcome, got, tt.want)
			}
		})
	}
}

// TestAnUndeclaredSetNeverParks: a set that never asked for exploration is
// outside the gate, whatever its explore runs say — which is why every set that
// predates the directive costs nothing.
func TestAnUndeclaredSetNeverParks(t *testing.T) {
	env := setupDrainExploreFixture(t, nil)
	writeExploreRunMeta(t, filepath.Join(env.tasksDir, "demo"), streamOutcomeFailed, time.Now())
	if got := exploreParkStatus(t, env); got != StatusReady {
		t.Fatalf("status = %s, want %s", got, StatusReady)
	}
}

// TestDrainParksADeclaredSetItCouldNotExplore drives the drain's own stop: the
// pass gives up at the head of the run, the drain exits before it selects a
// task, and the one set-level Progress record it leaves says which phase stopped
// the work and why — readable without opening a Captured run.
func TestDrainParksADeclaredSetItCouldNotExplore(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	setDir := filepath.Join(env.tasksDir, "demo")
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = func(string) (string, string, error) {
		// What the real walk leaves behind when it spends its cap: every attempt
		// filed, and no report.
		writeExploreRunMeta(t, setDir, streamOutcomeFailed, time.Now())
		return "", "", errors.New("the Explorer produced no report")
	}

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err == nil {
		t.Fatal("the drain built a declared set with no exploration report")
	}
	if !strings.Contains(err.Error(), "parked") {
		t.Fatalf("drain error = %v, want the explore park", err)
	}
	assertTaskOpen(t, env.execFixture(), "01-a")

	m := LoadManifest(env.deps(), "demo", filepath.Join(setDir, "index.json"))
	progress := readSetProgress(t, m)
	if n := strings.Count(progress, "[set] "+string(StatusExploreFailed)); n != 1 {
		t.Fatalf("set-level park records = %d, want exactly one:\n%s", n, progress)
	}
	if !strings.Contains(progress, "Explore phase:") {
		t.Fatalf("the park record does not name the phase:\n%s", progress)
	}
}

// TestDrainingAParkedSetRetriesTheExplorePass: a park stops a drain, it does not
// wedge one. An explicitly targeted parked set is let through so the drain's own
// Explore step can ask again, and a pass that answers this time writes the
// report and the set builds — the same door as a hand run, reached from the
// drain.
func TestDrainingAParkedSetRetriesTheExplorePass(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	setDir := filepath.Join(env.tasksDir, "demo")
	writeExploreRunMeta(t, setDir, streamOutcomeFailed, time.Now())
	if got := exploreParkStatus(t, env); got != StatusExploreFailed {
		t.Fatalf("status before the drain = %s, want %s", got, StatusExploreFailed)
	}
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.exploreRunner = func(string) (string, string, error) {
		return "## Where things live\n\n`tasks/explore.go` owns the pass.", "claude", nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the re-explored set drained to DONE", result)
	}
	if got := exploreParkStatus(t, env); got != StatusDone {
		t.Fatalf("status after the retry = %s, want %s", got, StatusDone)
	}
}

// TestSkipExploreDrainsADeclaredSetUnexplored: door two. The flag drains a
// parked set once without exploring it and without parking it again, and leaves
// the set as it found it — no report, so the next drain asks the question again.
func TestSkipExploreDrainsADeclaredSetUnexplored(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	setDir := filepath.Join(env.tasksDir, "demo")
	writeExploreRunMeta(t, setDir, streamOutcomeFailed, time.Now())
	if got := exploreParkStatus(t, env); got != StatusExploreFailed {
		t.Fatalf("status before the drain = %s, want %s", got, StatusExploreFailed)
	}
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{summary: "built"}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.SkipExplore = true
	opts.exploreRunner = exploreRefusingRunner(t, "a drain asked to skip exploring")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the set drained to DONE unexplored", result)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
	if got := explorationReportOf(t, env); got != "" {
		t.Fatalf("--skip-explore wrote a report:\n%s", got)
	}
	m := LoadManifest(env.deps(), "demo", filepath.Join(setDir, "index.json"))
	if progress := readSetProgress(t, m); strings.Contains(progress, string(StatusExploreFailed)) {
		t.Fatalf("--skip-explore parked the set:\n%s", progress)
	}
}
