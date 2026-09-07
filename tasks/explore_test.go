package tasks

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// setupExploreFixture writes a "demo" set of two tasks and a spec, with a
// scripted agent process standing in for the Explorer so the pass runs the real
// agent walk — resolution, invocation, and the Captured run each attempt is
// filed as. The clock is fixed so the report's header is predictable.
func setupExploreFixture(t *testing.T, at time.Time, runs ...scriptedRefineRun) (*Deps, *scriptedRefineRunner, string, string) {
	t.Helper()
	d, runner := refineRunnerDeps(t, runs...)
	d.Clock = deps.FixedClock{Instant: at}
	root := t.TempDir()
	defPath := filepath.Join(root, "tasks")
	setDir := filepath.Join(defPath, "demo")
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "Read the seam", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "Sign it off", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
	})
	writeTaskMD(t, setDir, "01-a.md", "## What to build\n\nWiden the seam in tasks/explore.go.\n\n## Acceptance criteria\n\n- [ ] the seam is widened\n")
	writeTaskMD(t, setDir, "02-b.md", "## Review\n\nConfirm the widened seam reads well.\n\n## Acceptance criteria\n\n- [ ] signed off\n")
	writeTaskMD(t, setDir, "spec.md", "# The seam\n\nOne owner for the pass's report.\n")
	return d, runner, defPath, setDir
}

func exploreOpts(defPath string, out *bytes.Buffer) exploreCoreOptions {
	return exploreCoreOptions{
		DefPath:     defPath,
		RuntimePath: "/rt",
		SetID:       "demo",
		Timeout:     time.Minute,
		Output:      out,
	}
}

// runPhases returns the phase of every Captured run filed under the set.
func runPhases(t *testing.T, d *Deps, setDir string) []string {
	t.Helper()
	runs, err := collectAllRuns(d, setDir)
	if err != nil {
		t.Fatalf("collect runs: %v", err)
	}
	phases := make([]string, 0, len(runs))
	for _, run := range runs {
		phases = append(phases, run.meta.Phase)
	}
	return phases
}

// TestExploreWritesTheReportAFreshAgentAnswered drives one hand-run explore
// pass end to end: what the Explorer is handed, what it is asked for, what is
// written, and the run it is filed as.
func TestExploreWritesTheReportAFreshAgentAnswered(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	d, runner, defPath, setDir := setupExploreFixture(t, at,
		scriptedRefineRun{output: claudeRefineStream("## Where things live\\n\\n`tasks/explore.go` owns the pass.")})

	var out bytes.Buffer
	res, err := exploreResolvedSet(d, nil, exploreOpts(defPath, &out))
	if err != nil {
		t.Fatalf("exploreResolvedSet: %v", err)
	}
	if runner.calls != 1 || len(runner.prompts) != 1 {
		t.Fatalf("agent invocations = %d, want one fresh Explorer", runner.calls)
	}

	// The prompt carries pop's own framing, the set's manifest listing, every
	// task body, the spec, and the tree the report describes.
	prompt := runner.prompts[0]
	for _, want := range []string{
		"independent Explorer",
		"Work SHA: sha1",
		"01-a [AFK open] Read the seam",
		filepath.Join(setDir, "01-a.md"),
		"02-b [HITL open] Sign it off",
		"Widen the seam in tasks/explore.go.",
		"Confirm the widened seam reads well.",
		"One owner for the pass's report.",
		"Every claim names a path or a symbol",
		"about a page",
		"Change nothing",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// It asks for nothing about prior art: the composed forms this repository
	// already has are named only as something that stays out (ADR-0262).
	if !strings.Contains(prompt, "No catalogue of the composed forms") {
		t.Fatalf("prompt does not keep the prior-art catalogue out:\n%s", prompt)
	}
	for _, forbidden := range []string{"prior art", "Prior art", "prior-art"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt asks about %q:\n%s", forbidden, prompt)
		}
	}

	// The report is one flat document in the set's own task storage, never in
	// the checkout it describes.
	wantPath := filepath.Join(setDir, ExplorationFileName)
	if res.Path != wantPath {
		t.Fatalf("path = %q, want %q", res.Path, wantPath)
	}
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{
		"# Exploration report — demo",
		"- Explored: 2026-09-07T12:00:00Z",
		"- Work SHA: sha1",
		"- Explorer: claude",
		"`tasks/explore.go` owns the pass.",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), "Prior art") {
		t.Fatalf("the report carries a prior-art section:\n%s", body)
	}
	if !strings.Contains(out.String(), wantPath) {
		t.Fatalf("output does not name the document:\n%s", out.String())
	}
	// The Explorer looks without touching: its whole output is the prose pop
	// files, so it is spawned under the read-only posture.
	if !strings.Contains(out.String(), "Read-only posture: --disallowedTools=Edit,Write,NotebookEdit") {
		t.Fatalf("the Explorer was not spawned read-only:\n%s", out.String())
	}

	// The attempt is filed as a Captured run of its own phase, beside the ones
	// implement, verify and refine file.
	if phases := runPhases(t, d, setDir); len(phases) != 1 || phases[0] != spendPhaseExplore {
		t.Fatalf("captured run phases = %v, want one %q run", phases, spendPhaseExplore)
	}
	// The report sits in the set folder, where every stray .md makes a set
	// MALFORMED — it is the one exempt document, so the set still registers.
	if m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json")); !m.Valid {
		t.Fatalf("an explored set reads malformed: %v", m.Errors)
	}
}

// TestExploreRewritesTheReportOnAnySetAsked covers the two things a hand run is:
// it ignores what the set declared about exploring, and it replaces the report
// the set already had rather than keeping a second copy.
func TestExploreRewritesTheReportOnAnySetAsked(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	d, _, defPath, setDir := setupExploreFixture(t, at,
		scriptedRefineRun{output: claudeRefineStream("## Where things live\\n\\nThe first map.")},
		scriptedRefineRun{output: claudeRefineStream("## Where things live\\n\\nThe second map.")})
	// The set says it does not want exploring; a human asking is not the drain's
	// automatic step, so the pass runs anyway.
	writeManifestWithSetKeys(t, setDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "Read the seam", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "Sign it off", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
	}, map[string]any{"explore": false})

	var out bytes.Buffer
	if _, err := exploreResolvedSet(d, nil, exploreOpts(defPath, &out)); err != nil {
		t.Fatalf("first explore pass: %v", err)
	}
	d.Clock = deps.FixedClock{Instant: at.Add(time.Hour)}
	res, err := exploreResolvedSet(d, nil, exploreOpts(defPath, &out))
	if err != nil {
		t.Fatalf("second explore pass: %v", err)
	}

	body, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if strings.Contains(string(body), "The first map.") || !strings.Contains(string(body), "The second map.") {
		t.Fatalf("the second pass did not replace the report:\n%s", body)
	}
	if !strings.Contains(string(body), "- Explored: 2026-09-07T13:00:00Z") {
		t.Fatalf("the rewritten report keeps the first pass's header:\n%s", body)
	}
	// One document, not a family: nothing accumulates beside it.
	entries, err := os.ReadDir(setDir)
	if err != nil {
		t.Fatal(err)
	}
	var reports []string
	for _, e := range entries {
		if strings.Contains(e.Name(), "exploration") {
			reports = append(reports, e.Name())
		}
	}
	if len(reports) != 1 {
		t.Fatalf("exploration documents = %v, want the one flat report", reports)
	}
}

// TestExploreThatCannotProduceAReportWritesNone: a pass whose agent never
// reaches an ending of its own spends its cap, files every attempt, and leaves
// the report the set already had exactly as it was — half a map is worse than
// the older whole one. An interrupted pass leaves by this same door: the write
// sits after the walk's error return, so it is reached only by a pass that
// answered.
func TestExploreThatCannotProduceAReportWritesNone(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	crashed := scriptedRefineRun{output: claudeRefineStream("## Where things"), runErr: errors.New("agent crashed")}
	d, runner, defPath, setDir := setupExploreFixture(t, at, crashed, crashed, crashed)

	reportPath := filepath.Join(setDir, ExplorationFileName)
	if err := os.WriteFile(reportPath, []byte("# Exploration report — demo\n\nthe map from last week\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if _, err := exploreResolvedSet(d, nil, exploreOpts(defPath, &out)); err == nil {
		t.Fatal("a pass that produced no report returned no error")
	}
	if runner.calls != 3 {
		t.Fatalf("agent invocations = %d, want the whole retry cap spent", runner.calls)
	}

	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(body), "the map from last week") {
		t.Fatalf("a failed pass overwrote the set's report:\n%s", body)
	}
	if phases := runPhases(t, d, setDir); len(phases) != 3 {
		t.Fatalf("captured run phases = %v, want every attempt filed", phases)
	}
	// The burn is spent against a cap of its own, named by the same phase word
	// the runs are filed under.
	caps, err := AllSpentRetryCaps(d)
	if err != nil {
		t.Fatalf("AllSpentRetryCaps: %v", err)
	}
	if len(caps) != 1 || caps[0].Phase != spendPhaseExplore || caps[0].SetID != "demo" {
		t.Fatalf("spent caps = %#v, want one %q burn for demo", caps, spendPhaseExplore)
	}
}

// TestExploreRefusesASetItCannotResolve keeps the two states with nothing to
// explore refused by name, before any agent is spawned.
func TestExploreRefusesASetItCannotResolve(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		setID   string
		wantErr string
	}{
		{name: "unknown set", setID: "absent", wantErr: `unknown task set "absent"`},
		{name: "no set named", setID: "", wantErr: "a task set identifier is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, runner, defPath, _ := setupExploreFixture(t, at)
			opts := exploreOpts(defPath, &bytes.Buffer{})
			opts.SetID = tt.setID
			if _, err := exploreResolvedSet(d, nil, opts); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one naming %q", err, tt.wantErr)
			}
			if runner.calls != 0 {
				t.Fatalf("agent invocations = %d, want none for a set that cannot be explored", runner.calls)
			}
		})
	}
}

// TestResolveExplorerPrecedence covers the Explorer chain (ADR-0262), highest
// first: CLI flags → the per-set `explorer` object → the implement agents /
// heavy, with agents and effort resolving independently of one another.
func TestResolveExplorerPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cliAgents  []string
		cliEffort  string
		manifest   *Manifest
		wantAgents []string
		wantEffort string
	}{
		{
			name:       "default when nothing steers the pass",
			wantAgents: []string{DefaultAgentPreset},
			wantEffort: DefaultExploreEffort,
		},
		{
			name:       "the per-set explorer object steers both",
			manifest:   manifestWithAgentDirective(t, "explorer", []string{"pi"}, "light"),
			wantAgents: []string{"pi"},
			wantEffort: "light",
		},
		{
			name:       "CLI overrides the per-set object",
			cliAgents:  []string{"opencode"},
			cliEffort:  "standard",
			manifest:   manifestWithAgentDirective(t, "explorer", []string{"pi"}, "light"),
			wantAgents: []string{"opencode"},
			wantEffort: "standard",
		},
		{
			name:       "agents and effort resolve independently",
			cliAgents:  []string{"opencode"},
			manifest:   manifestWithAgentDirective(t, "explorer", nil, "light"),
			wantAgents: []string{"opencode"},
			wantEffort: "light",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sel, err := resolveExplorer(tt.cliAgents, tt.cliEffort, tt.manifest, nil)
			if err != nil {
				t.Fatalf("resolveExplorer: %v", err)
			}
			if strings.Join(sel.Agents, ",") != strings.Join(tt.wantAgents, ",") {
				t.Fatalf("agents = %v, want %v", sel.Agents, tt.wantAgents)
			}
			if sel.Effort != tt.wantEffort {
				t.Fatalf("effort = %q, want %q", sel.Effort, tt.wantEffort)
			}
		})
	}
}
