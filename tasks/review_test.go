package tasks

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

// setupReviewFixture writes a "demo" set holding one done AFK task, with a git
// seam that answers the range detection with a real-looking commit and stat.
// The clock is fixed so the Review artifact's name is predictable.
func setupReviewFixture(t *testing.T, at time.Time) (*Deps, string, string) {
	t.Helper()
	d := newTestDeps(t)
	d.Clock = deps.FixedClock{Instant: at}
	d.Git = stubGit("abc123abc123\n", "aaa111\n", " tasks/review.go | 40 ++++++++\n 1 file changed\n")
	root := t.TempDir()
	defPath := filepath.Join(root, "tasks")
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "Review on demand", Type: "AFK", Status: "done"},
	})
	return d, defPath, filepath.Join(defPath, "demo")
}

func reviewOpts(defPath string, out *bytes.Buffer, run func(prompt string) (string, string, error)) reviewCoreOptions {
	return reviewCoreOptions{
		DefPath:     defPath,
		RuntimePath: "/rt",
		SetID:       "demo",
		Output:      out,
		runReviewer: run,
	}
}

func listReviewFiles(t *testing.T, setDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(setDir, reviewsDirName))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestReviewWritesDocumentFromAPromptCarryingEverything drives one on-demand
// review end to end: what the Reviewer is handed, what is written, and where.
func TestReviewWritesDocumentFromAPromptCarryingEverything(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := setupReviewFixture(t, at)

	var out bytes.Buffer
	var gotPrompt string
	opts := reviewOpts(defPath, &out, func(prompt string) (string, string, error) {
		gotPrompt = prompt
		return "## Naming\n\nThe helper reads as a noun but does work.", "claude", nil
	})
	opts.Convention = func(string) (string, error) { return "PROSE-CONVENTION-BODY", nil }

	res, err := reviewResolvedSet(d, nil, opts)
	if err != nil {
		t.Fatalf("reviewResolvedSet: %v", err)
	}

	// The prompt carries pop's own instruction, the convention, the commit range
	// and the changeset's shape — and tells the Reviewer to open the files.
	for _, want := range []string{
		"independent Reviewer",
		"Reach no verdict",
		"PROSE-CONVENTION-BODY",
		"aaa111^..HEAD",
		"tasks/review.go | 40",
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

	// The document is written under the set's own reviews/ directory — task
	// storage, never the checkout under review.
	wantPath := filepath.Join(setDir, reviewsDirName, "review-20260816T120000Z.md")
	if res.Path != wantPath {
		t.Fatalf("path = %q, want %q", res.Path, wantPath)
	}
	if strings.HasPrefix(res.Path, "/rt") {
		t.Fatalf("review written into the checkout under review: %s", res.Path)
	}
	body, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read review: %v", err)
	}
	for _, want := range []string{
		"# Code review — demo",
		"- Reviewed: 2026-08-16T12:00:00Z",
		"- Commit range: aaa111^..HEAD",
		"- Reviewer: claude",
		"The helper reads as a noun but does work.",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document missing %q:\n%s", want, body)
		}
	}
	// The run reaches no verdict and says nothing about what was found.
	for _, forbidden := range []string{"PASS", "FIXABLE", "NEEDS-HUMAN", "Verdict"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("review output reached a verdict (%q):\n%s", forbidden, out.String())
		}
	}
	if !strings.Contains(out.String(), wantPath) {
		t.Fatalf("output does not name the document:\n%s", out.String())
	}
	// The artifacts accumulate in a sub-directory, so the orphan-markdown check
	// that makes every stray .md in a set folder MALFORMED never sees them.
	if m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json")); !m.Valid {
		t.Fatalf("a reviewed set reads malformed: %v", m.Errors)
	}
}

// TestReviewSupersedesTheLastAndKeepsIt: the second review is told about the
// first, the first stays on disk, and the newest timestamp is what --show reads.
func TestReviewSupersedesTheLastAndKeepsIt(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := setupReviewFixture(t, first)

	var out bytes.Buffer
	if _, err := reviewResolvedSet(d, nil, reviewOpts(defPath, &out, func(string) (string, string, error) {
		return "## First\n\nthe first finding", "claude", nil
	})); err != nil {
		t.Fatalf("first review: %v", err)
	}

	d.Clock = deps.FixedClock{Instant: first.Add(time.Hour)}
	out.Reset()
	var second string
	res, err := reviewResolvedSet(d, nil, reviewOpts(defPath, &out, func(prompt string) (string, string, error) {
		second = prompt
		return "## Second\n\nthe first finding is fixed", "claude", nil
	}))
	if err != nil {
		t.Fatalf("second review: %v", err)
	}

	if !strings.Contains(second, "the first finding") {
		t.Fatalf("second prompt does not carry the previous document:\n%s", second)
	}
	if !strings.Contains(second, "replaces the one below") {
		t.Fatalf("second prompt does not state the supersede rule:\n%s", second)
	}
	if !strings.Contains(out.String(), "Supersedes the previous review") {
		t.Fatalf("output does not report the supersede:\n%s", out.String())
	}
	if got := listReviewFiles(t, setDir); len(got) != 2 {
		t.Fatalf("review files = %v, want both retained", got)
	}

	// --show resolves the newest by timestamp and prints it verbatim.
	var shown bytes.Buffer
	showOpts := reviewOpts(defPath, &shown, func(string) (string, string, error) {
		t.Fatal("--show must spawn no agent")
		return "", "", nil
	})
	showOpts.Show = true
	shownRes, err := reviewResolvedSet(d, nil, showOpts)
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

// TestReviewRefusesWhatItCannotReview: the three states with nothing to judge,
// each refused by name rather than reviewed against a guess.
func TestReviewRefusesWhatItCannotReview(t *testing.T) {
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
			wantErr: "no done AFK task to review",
		},
		{
			name:    "no committed work",
			tasks:   []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}},
			git:     stubGit("abc\n", "", ""),
			wantErr: "no committed work to review",
		},
		{
			name:    "show with no document",
			tasks:   []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}},
			show:    true,
			wantErr: "has no review yet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, defPath, _ := setupReviewFixture(t, at)
			if tt.git != nil {
				d.Git = tt.git
			}
			setupManifest(t, defPath, "demo", tt.tasks)
			opts := reviewOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
				t.Fatal("a refused review must spawn no agent")
				return "", "", nil
			})
			opts.Show = tt.show
			_, err := reviewResolvedSet(d, nil, opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one naming %q", err, tt.wantErr)
			}
		})
	}
}

// TestReviewRefusesAnUndeterminedRange: the set demonstrably committed
// something, but none of it is in this checkout's history. Reviewing a guessed
// range would judge other people's commits, so the review refuses instead.
func TestReviewRefusesAnUndeterminedRange(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := setupReviewFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	writeManifestWithSetKeys(t, setDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	}, map[string]any{"base_commit": "deadbeef"})
	// Nothing recorded is reachable and no subject matches.
	d.Git = &deps.MockGit{CommandInDirFunc: func(string, ...string) (string, error) {
		return "", errStubGitUnreachable
	}}

	_, err := reviewResolvedSet(d, nil, reviewOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
		t.Fatal("an undetermined range must spawn no agent")
		return "", "", nil
	}))
	if err == nil || !strings.Contains(err.Error(), "commit range could not be determined") {
		t.Fatalf("err = %v, want the undetermined-range refusal", err)
	}
}

var errStubGitUnreachable = errors.New("not in this history")

// TestReviewRunsMidDrainAndChangesNoStatus: an open task beside the done one —
// a set the drain has not finished — still reviews, and the manifest is left
// exactly as it was found.
func TestReviewRunsMidDrainAndChangesNoStatus(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := setupReviewFixture(t, at)
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	before, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := reviewResolvedSet(d, nil, reviewOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
		return "## Mid-drain\n\nfine so far", "claude", nil
	})); err != nil {
		t.Fatalf("reviewResolvedSet: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(setDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("review rewrote the manifest:\n%s", after)
	}
}

// TestResolveReviewerPrecedence covers the Reviewer chain (ADR-0214), highest
// first: CLI flags → [work.review] → [work.implement].agents / heavy, with
// agents and effort resolving independently of one another.
func TestResolveReviewerPrecedence(t *testing.T) {
	t.Parallel()
	reviewCfg := func(agents []string, effort string) *config.Config {
		return &config.Config{Work: &config.WorkConfig{
			Review: &config.ReviewConfig{Enabled: true, Agents: config.AgentEntriesFromCommands(agents...), Effort: effort},
		}}
	}

	tests := []struct {
		name       string
		cliAgents  []string
		cliEffort  string
		cfg        *config.Config
		wantAgents []string
		wantEffort string
	}{
		{
			name:       "default when nothing configured",
			wantAgents: []string{DefaultAgentPreset},
			wantEffort: DefaultReviewEffort,
		},
		{
			name:       "config drives agents and effort",
			cfg:        reviewCfg([]string{"codex", "claude"}, "standard"),
			wantAgents: []string{"codex", "claude"},
			wantEffort: "standard",
		},
		{
			name: "omitted review agents fall back to the implement list",
			cfg: &config.Config{Work: &config.WorkConfig{
				Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("cursor")},
				Review:    &config.ReviewConfig{Enabled: true},
			}},
			wantAgents: []string{"cursor"},
			wantEffort: DefaultReviewEffort,
		},
		{
			name:       "CLI overrides config",
			cliAgents:  []string{"opencode"},
			cliEffort:  "light",
			cfg:        reviewCfg([]string{"codex"}, "standard"),
			wantAgents: []string{"opencode"},
			wantEffort: "light",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sel := resolveReviewer(tt.cliAgents, tt.cliEffort, tt.cfg)
			if strings.Join(sel.Agents, ",") != strings.Join(tt.wantAgents, ",") {
				t.Fatalf("agents = %v, want %v", sel.Agents, tt.wantAgents)
			}
			if sel.Effort != tt.wantEffort {
				t.Fatalf("effort = %q, want %q", sel.Effort, tt.wantEffort)
			}
		})
	}
}

// TestReviewWithoutAConventionSaysSo: pop ships no house standard, so a
// repository that has derived none is told to read its own idiom instead.
func TestReviewWithoutAConventionSaysSo(t *testing.T) {
	t.Parallel()
	d, defPath, _ := setupReviewFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	var gotPrompt string
	if _, err := reviewResolvedSet(d, nil, reviewOpts(defPath, &bytes.Buffer{}, func(prompt string) (string, string, error) {
		gotPrompt = prompt
		return "## Fine", "claude", nil
	})); err != nil {
		t.Fatalf("reviewResolvedSet: %v", err)
	}
	if !strings.Contains(gotPrompt, "No code-review convention is recorded") {
		t.Fatalf("prompt does not admit the missing convention:\n%s", gotPrompt)
	}
}
