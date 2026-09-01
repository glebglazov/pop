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

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

// setupRefineFixture writes a "demo" set holding one done AFK task, with a git
// seam that answers the range detection with a real-looking commit and stat.
// The clock is fixed so the Refine report's name is predictable.
func setupRefineFixture(t *testing.T, at time.Time) (*Deps, string, string) {
	t.Helper()
	d := newTestDeps(t)
	d.Clock = deps.FixedClock{Instant: at}
	d.Git = stubGit("abc123abc123\n", "aaa111\n", " tasks/refine.go | 40 ++++++++\n 1 file changed\n")
	root := t.TempDir()
	defPath := filepath.Join(root, "tasks")
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "Refine on demand", Type: "AFK", Status: "done"},
	})
	return d, defPath, filepath.Join(defPath, "demo")
}

func refineOpts(defPath string, out *bytes.Buffer, run func(prompt string) (string, string, error)) refineCoreOptions {
	return refineCoreOptions{
		DefPath:     defPath,
		RuntimePath: "/rt",
		SetID:       "demo",
		Output:      out,
		runRefiner:  run,
	}
}

func listRefineFiles(t *testing.T, setDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(setDir, RefineDirName))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestRefineWritesReportFromAPromptCarryingEverything drives one on-demand
// refine pass end to end: what the Refiner is handed, what is written, and where.
func TestRefineWritesReportFromAPromptCarryingEverything(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := setupRefineFixture(t, at)

	var out bytes.Buffer
	var gotPrompt string
	opts := refineOpts(defPath, &out, func(prompt string) (string, string, error) {
		gotPrompt = prompt
		return "## Naming\n\nThe helper reads as a noun but does work.", "claude", nil
	})
	opts.Convention = func(string) (string, error) { return "PROSE-CONVENTION-BODY", nil }

	res, err := refineResolvedSet(d, nil, opts)
	if err != nil {
		t.Fatalf("refineResolvedSet: %v", err)
	}

	// The prompt carries pop's own instruction, the convention, the commit range
	// and the changeset's shape — and tells the Refiner to open the files.
	for _, want := range []string{
		"independent Refiner",
		"Reach no verdict",
		"PROSE-CONVENTION-BODY",
		"aaa111^..HEAD",
		"tasks/refine.go | 40",
		"Read the changed files yourself",
		"git diff aaa111^..HEAD -- <path>",
	} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, gotPrompt)
		}
	}
	// It is never handed diff bodies.
	if strings.Contains(gotPrompt, "@@ ") || strings.Contains(gotPrompt, "\n+++ ") {
		t.Fatalf("prompt inlined diff bodies:\n%s", gotPrompt)
	}

	// The report is written under the set's own refine/ directory — task
	// storage, never the checkout it describes.
	wantPath := filepath.Join(setDir, RefineDirName, "refine-20260816T120000Z.md")
	if res.Path != wantPath {
		t.Fatalf("path = %q, want %q", res.Path, wantPath)
	}
	if strings.HasPrefix(res.Path, "/rt") {
		t.Fatalf("report written into the checkout it describes: %s", res.Path)
	}
	body, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{
		"# Refine report — demo",
		"- Refined: 2026-08-16T12:00:00Z",
		"- Commit range: aaa111^..HEAD",
		"- Refiner: claude",
		"The helper reads as a noun but does work.",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document missing %q:\n%s", want, body)
		}
	}
	// The run reaches no verdict and says nothing about what was found.
	for _, forbidden := range []string{"PASS", "FIXABLE", "NEEDS-HUMAN", "Verdict"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("refine output reached a verdict (%q):\n%s", forbidden, out.String())
		}
	}
	if !strings.Contains(out.String(), wantPath) {
		t.Fatalf("output does not name the document:\n%s", out.String())
	}
	// The artifacts accumulate in a sub-directory, so the orphan-markdown check
	// that makes every stray .md in a set folder MALFORMED never sees them.
	if m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json")); !m.Valid {
		t.Fatalf("a refined set reads malformed: %v", m.Errors)
	}
}

// TestRefineSupersedesTheLastAndKeepsIt: the second pass is told about the
// first, the first stays on disk, and the newest timestamp is what --show reads.
func TestRefineSupersedesTheLastAndKeepsIt(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := setupRefineFixture(t, first)

	var out bytes.Buffer
	if _, err := refineResolvedSet(d, nil, refineOpts(defPath, &out, func(string) (string, string, error) {
		return "## First\n\nthe first finding", "claude", nil
	})); err != nil {
		t.Fatalf("first refine pass: %v", err)
	}

	d.Clock = deps.FixedClock{Instant: first.Add(time.Hour)}
	out.Reset()
	var second string
	res, err := refineResolvedSet(d, nil, refineOpts(defPath, &out, func(prompt string) (string, string, error) {
		second = prompt
		return "## Second\n\nthe first finding is fixed", "claude", nil
	}))
	if err != nil {
		t.Fatalf("second refine pass: %v", err)
	}

	if !strings.Contains(second, "the first finding") {
		t.Fatalf("second prompt does not carry the previous document:\n%s", second)
	}
	if !strings.Contains(second, "replaces the one below") {
		t.Fatalf("second prompt does not state the supersede rule:\n%s", second)
	}
	if !strings.Contains(out.String(), "Supersedes the previous report") {
		t.Fatalf("output does not report the supersede:\n%s", out.String())
	}
	if got := listRefineFiles(t, setDir); len(got) != 2 {
		t.Fatalf("refine files = %v, want both retained", got)
	}

	// --show resolves the newest by timestamp and prints it verbatim.
	var shown bytes.Buffer
	showOpts := refineOpts(defPath, &shown, func(string) (string, string, error) {
		t.Fatal("--show must spawn no agent")
		return "", "", nil
	})
	showOpts.Show = true
	shownRes, err := refineResolvedSet(d, nil, showOpts)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if shownRes.Path != res.Path {
		t.Fatalf("show read %q, want the latest %q", shownRes.Path, res.Path)
	}
	if !strings.Contains(shown.String(), "the first finding is fixed") || strings.Contains(shown.String(), "━━") {
		t.Fatalf("--show must print the document alone:\n%s", shown.String())
	}
}

// TestRefineRefusesWhatItCannotRefine: the three states with nothing to judge,
// each refused by name rather than refined against a guess.
func TestRefineRefusesWhatItCannotRefine(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		tasks   []Task
		git     *deps.MockGit
		show    bool
		wantErr string
	}{
		{
			name:    "no done AFK task",
			tasks:   []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "done"}},
			wantErr: "no done AFK task to refine",
		},
		{
			name:    "no committed work",
			tasks:   []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}},
			git:     stubGit("abc\n", "", ""),
			wantErr: "no committed work to refine",
		},
		{
			name:    "show with no document",
			tasks:   []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}},
			show:    true,
			wantErr: "has no refine report yet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, defPath, _ := setupRefineFixture(t, at)
			if tt.git != nil {
				d.Git = tt.git
			}
			setupManifest(t, defPath, "demo", tt.tasks)
			opts := refineOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
				t.Fatal("a refused refine pass must spawn no agent")
				return "", "", nil
			})
			opts.Show = tt.show
			_, err := refineResolvedSet(d, nil, opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one naming %q", err, tt.wantErr)
			}
		})
	}
}

// TestRefineRefusesAnUndeterminedRange: the set demonstrably committed
// something, but none of it is in this checkout's history. Refining a guessed
// range would judge other people's commits, so the pass refuses instead.
func TestRefineRefusesAnUndeterminedRange(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := setupRefineFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	writeManifestWithSetKeys(t, setDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	}, map[string]any{"base_commit": "deadbeef"})
	// Nothing recorded is reachable and no subject matches — but the checkout
	// still resolves to a repository, since the refine pass takes it before it
	// looks for a range.
	d.Git = &deps.MockGit{CommandInDirFunc: func(_ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir" {
			return "/repo/.git", nil
		}
		return "", errStubGitUnreachable
	}}

	_, err := refineResolvedSet(d, nil, refineOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
		t.Fatal("an undetermined range must spawn no agent")
		return "", "", nil
	}))
	if err == nil || !strings.Contains(err.Error(), "commit range could not be determined") {
		t.Fatalf("err = %v, want the undetermined-range refusal", err)
	}
}

var errStubGitUnreachable = errors.New("not in this history")

// TestRefineRunsOnAnUnfinishedSetAndChangesNoStatus: an open task beside the
// done one — a set whose drain has not reached the end — still refines, and the
// manifest is left exactly as it was found.
func TestRefineRunsOnAnUnfinishedSetAndChangesNoStatus(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := setupRefineFixture(t, at)
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	before, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := refineResolvedSet(d, nil, refineOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
		return "## Mid-drain\n\nfine so far", "claude", nil
	})); err != nil {
		t.Fatalf("refineResolvedSet: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("refine pass rewrote the manifest:\n%s", after)
	}
}

// manifestWithRefiner builds a bare manifest carrying a per-set `refiner`
// override object under Unknown, so RefinerOverride() parses it exactly as it
// would from a loaded index.json.
func manifestWithRefiner(t *testing.T, agents []string, effort string) *Manifest {
	t.Helper()
	raw, err := json.Marshal(AgentDirective{Agents: agents, Effort: effort})
	if err != nil {
		t.Fatal(err)
	}
	return &Manifest{Unknown: map[string]json.RawMessage{"refiner": raw}}
}

// TestResolveRefinerPrecedence covers the Refiner chain (ADR-0252), highest
// first: CLI flags → the per-set `refiner` override → [work.refine] →
// [work.implement].agents / heavy, with agents and effort resolving
// independently of one another.
func TestResolveRefinerPrecedence(t *testing.T) {
	t.Parallel()
	refineCfg := func(agents []string, effort string) *config.Config {
		return &config.Config{Work: &config.WorkConfig{
			Refine: &config.RefineConfig{Enabled: true, Agents: config.AgentEntriesFromCommands(agents...), Effort: effort},
		}}
	}

	tests := []struct {
		name       string
		cliAgents  []string
		cliEffort  string
		manifest   *Manifest
		cfg        *config.Config
		wantAgents []string
		wantEffort string
	}{
		{
			name:       "default when nothing configured",
			wantAgents: []string{DefaultAgentPreset},
			wantEffort: DefaultRefineEffort,
		},
		{
			name:       "config drives agents and effort",
			cfg:        refineCfg([]string{"codex", "claude"}, "standard"),
			wantAgents: []string{"codex", "claude"},
			wantEffort: "standard",
		},
		{
			name: "omitted refine agents fall back to the implement list",
			cfg: &config.Config{Work: &config.WorkConfig{
				Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("cursor")},
				Refine:    &config.RefineConfig{Enabled: true},
			}},
			wantAgents: []string{"cursor"},
			wantEffort: DefaultRefineEffort,
		},
		{
			name:       "CLI overrides config",
			cliAgents:  []string{"opencode"},
			cliEffort:  "light",
			cfg:        refineCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"opencode"},
			wantEffort: "light",
		},
		{
			name:       "per-set refiner overrides config",
			manifest:   manifestWithRefiner(t, []string{"pi"}, "light"),
			cfg:        refineCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"pi"},
			wantEffort: "light",
		},
		{
			name:       "CLI overrides the per-set refiner",
			cliAgents:  []string{"opencode"},
			cliEffort:  "heavy",
			manifest:   manifestWithRefiner(t, []string{"pi"}, "light"),
			cfg:        refineCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"opencode"},
			wantEffort: "heavy",
		},
		{
			name:       "per-set agents and effort resolve independently",
			cliAgents:  []string{"opencode"},
			manifest:   manifestWithRefiner(t, nil, "light"),
			cfg:        refineCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"opencode"}, // CLI agents win
			wantEffort: "light",              // per-set effort wins (no CLI effort)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sel, err := resolveRefiner(tt.cliAgents, tt.cliEffort, tt.manifest, tt.cfg)
			if err != nil {
				t.Fatalf("resolveRefiner: %v", err)
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
