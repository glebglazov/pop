package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantVerdict  Verdict
		wantFindings string // substring the findings must contain ("" = must be empty)
	}{
		{name: "pass", raw: "VERDICT: PASS\nFINDINGS:\n", wantVerdict: VerdictPass, wantFindings: ""},
		{name: "fixable", raw: "VERDICT: FIXABLE\nFINDINGS: criterion 3 is unmet", wantVerdict: VerdictFixable, wantFindings: "criterion 3 is unmet"},
		{name: "needs-human", raw: "VERDICT: NEEDS-HUMAN\nFINDINGS: the spec is ambiguous", wantVerdict: VerdictNeedsHuman, wantFindings: "the spec is ambiguous"},
		{name: "needs_human underscore spelling", raw: "VERDICT: NEEDS_HUMAN\nFINDINGS: x", wantVerdict: VerdictNeedsHuman, wantFindings: "x"},
		{name: "markdown bold decoration", raw: "**VERDICT: PASS**\n", wantVerdict: VerdictPass, wantFindings: ""},
		{name: "verdict amid prose", raw: "Here is my judgment.\nVERDICT: FIXABLE\nFINDINGS: missing test coverage", wantVerdict: VerdictFixable, wantFindings: "missing test coverage"},
		{name: "findings without label falls back to remainder", raw: "VERDICT: FIXABLE\nthe error handling is wrong", wantVerdict: VerdictFixable, wantFindings: "the error handling is wrong"},
		{name: "trailing text after token", raw: "VERDICT: NEEDS-HUMAN — cannot decide\nFINDINGS: needs a call", wantVerdict: VerdictNeedsHuman, wantFindings: "needs a call"},
		{name: "empty response", raw: "", wantVerdict: VerdictNeedsHuman, wantFindings: "no output"},
		{name: "whitespace-only response", raw: "   \n\t\n", wantVerdict: VerdictNeedsHuman, wantFindings: "no output"},
		{name: "no verdict marker", raw: "Looks good to me, everything passes.", wantVerdict: VerdictNeedsHuman, wantFindings: "could not be parsed"},
		{name: "unrecognized verdict token", raw: "VERDICT: MAYBE\nFINDINGS: unsure", wantVerdict: VerdictNeedsHuman, wantFindings: "could not be parsed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotV, gotF := ParseVerdict(tt.raw)
			if gotV != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q", gotV, tt.wantVerdict)
			}
			if tt.wantFindings == "" {
				if strings.TrimSpace(gotF) != "" {
					t.Fatalf("findings = %q, want empty", gotF)
				}
				return
			}
			if !strings.Contains(gotF, tt.wantFindings) {
				t.Fatalf("findings = %q, want to contain %q", gotF, tt.wantFindings)
			}
		})
	}
}

func TestParseVerdictMalformedIncludesRawForHuman(t *testing.T) {
	raw := "I think it is basically fine."
	v, findings := ParseVerdict(raw)
	if v != VerdictNeedsHuman {
		t.Fatalf("verdict = %q, want NEEDS-HUMAN", v)
	}
	if !strings.Contains(findings, raw) {
		t.Fatalf("findings %q should surface the raw response for the human", findings)
	}
}

// setupVerifyFixture writes a "demo" set under a temp definition path with the
// data dir isolated to a temp location, so the Drain store never touches the
// developer's real store.
func setupVerifyFixture(t *testing.T, git *deps.MockGit) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	defPath := filepath.Join(root, "tasks")
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	return &Deps{FS: deps.NewRealFileSystem(), Git: git}, defPath
}

// stubGit answers only the git commands the verify core issues, without touching
// a real repository.
func stubGit(head, log, diff string) *deps.MockGit {
	return &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return head, nil
		case len(args) >= 1 && args[0] == "log":
			return log, nil
		case len(args) >= 1 && args[0] == "diff":
			return diff, nil
		}
		return "", nil
	}}
}

func readStoredVerdict(t *testing.T, d *Deps, repo, setID, sha string) *store.VerifyVerdict {
	t.Helper()
	s, err := store.Open(DrainStorePathWith(d))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = s.Close() }()
	v, err := s.GetVerifyVerdict(repo, setID, sha)
	if err != nil {
		t.Fatalf("GetVerifyVerdict: %v", err)
	}
	return v
}

func TestVerifyResolvedSetRunsPrintsAndPersists(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("abc123abc123\n", "", ""))
	var out bytes.Buffer
	var gotPrompt string
	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo:        "/repo/.git",
		DefPath:     defPath,
		RuntimePath: "/rt",
		SetID:       "demo",
		Output:      &out,
		runVerifier: func(prompt string) (string, error) {
			gotPrompt = prompt
			return "VERDICT: FIXABLE\nFINDINGS: criterion 2 unmet\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}

	if res.Verdict != VerdictFixable || res.Findings != "criterion 2 unmet" {
		t.Fatalf("result = %+v, want FIXABLE / criterion 2 unmet", res)
	}
	if res.WorkSHA != "abc123abc123" {
		t.Fatalf("WorkSHA = %q, want abc123abc123", res.WorkSHA)
	}
	// Prompt carries the criteria and the task body.
	if !strings.Contains(gotPrompt, "Acceptance criteria") || !strings.Contains(gotPrompt, "01-a") {
		t.Fatalf("prompt missing criteria/task body:\n%s", gotPrompt)
	}
	// Verdict is printed.
	if !strings.Contains(out.String(), "FIXABLE") || !strings.Contains(out.String(), "criterion 2 unmet") {
		t.Fatalf("output missing verdict/findings:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Re-run: pop tasks verify demo") {
		t.Fatalf("output missing re-run command:\n%s", out.String())
	}
	// Verdict is persisted keyed by set + work SHA.
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "abc123abc123")
	if stored == nil || stored.Verdict != "FIXABLE" || stored.Findings != "criterion 2 unmet" {
		t.Fatalf("stored verdict = %+v, want FIXABLE / criterion 2 unmet", stored)
	}
}

func TestVerifyResolvedSetForceOverwritesForSHA(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaX\n", "", ""))
	run := func(output string) {
		if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output:      &bytes.Buffer{},
			runVerifier: func(string) (string, error) { return output, nil },
		}); err != nil {
			t.Fatalf("verifyResolvedSet: %v", err)
		}
	}
	run("VERDICT: FIXABLE\nFINDINGS: first pass\n")
	run("VERDICT: PASS\nFINDINGS:\n") // re-run at the same SHA overwrites

	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaX")
	if stored == nil || stored.Verdict != "PASS" || stored.Findings != "" {
		t.Fatalf("stored verdict = %+v, want overwritten PASS with no findings", stored)
	}
}

func TestVerifyResolvedSetMalformedResponseParksNeedsHuman(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:      &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "the code looks alright", nil },
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if res.Verdict != VerdictNeedsHuman {
		t.Fatalf("verdict = %q, want NEEDS-HUMAN", res.Verdict)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "sha1")
	if stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("stored verdict = %+v, want NEEDS-HUMAN", stored)
	}
	if !strings.Contains(stored.Findings, "could not be parsed") {
		t.Fatalf("stored findings = %q, want a human-facing explanation", stored.Findings)
	}
}

func TestVerifyResolvedSetIncludesWorkDiffInPrompt(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "commitHash1\n", "DIFF-BODY-MARKER"))
	var gotPrompt string
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{},
		runVerifier: func(prompt string) (string, error) {
			gotPrompt = prompt
			return "VERDICT: PASS\n", nil
		},
	}); err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if !strings.Contains(gotPrompt, "DIFF-BODY-MARKER") || !strings.Contains(gotPrompt, "```diff") {
		t.Fatalf("prompt missing work diff:\n%s", gotPrompt)
	}
}

func TestVerifyResolvedSetIncludesPRDInPromptWhenPresent(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	prdPath := filepath.Join(defPath, "demo", "prd.md")
	if err := os.WriteFile(prdPath, []byte("PRD-BODY-MARKER\n"), 0o644); err != nil {
		t.Fatalf("write prd.md: %v", err)
	}
	var gotPrompt string
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{},
		runVerifier: func(prompt string) (string, error) {
			gotPrompt = prompt
			return "VERDICT: PASS\n", nil
		},
	}); err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if !strings.Contains(gotPrompt, "PRD-BODY-MARKER") {
		t.Fatalf("prompt missing PRD content:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "acceptance criteria above remain authoritative") {
		t.Fatalf("prompt missing PRD-is-context-only framing:\n%s", gotPrompt)
	}
}

func TestVerifyResolvedSetOmitsPRDSectionWhenAbsent(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	var gotPrompt string
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{},
		runVerifier: func(prompt string) (string, error) {
			gotPrompt = prompt
			return "VERDICT: PASS\n", nil
		},
	}); err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if strings.Contains(gotPrompt, "## PRD") {
		t.Fatalf("prompt should omit PRD section when prd.md is absent:\n%s", gotPrompt)
	}
}

func TestReadPRDAbsentIsNotError(t *testing.T) {
	root := t.TempDir()
	m := &Manifest{Dir: root}
	d := &Deps{FS: deps.NewRealFileSystem()}
	if _, ok := readPRD(d, m); ok {
		t.Fatal("readPRD: expected false for absent prd.md")
	}
}

func TestReadPRDPresentReturnsContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prd.md"), []byte("  hello prd  \n"), 0o644); err != nil {
		t.Fatalf("write prd.md: %v", err)
	}
	m := &Manifest{Dir: root}
	d := &Deps{FS: deps.NewRealFileSystem()}
	got, ok := readPRD(d, m)
	if !ok || got != "hello prd" {
		t.Fatalf("readPRD = %q, %v, want %q, true", got, ok, "hello prd")
	}
}

func TestVerifyResolvedSetUnknownSet(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "nope",
		Output:      &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "VERDICT: PASS\n", nil },
	})
	if err == nil {
		t.Fatal("expected an error for an unknown set")
	}
	if !strings.Contains(err.Error(), "unknown task set") {
		t.Fatalf("err = %v, want unknown task set", err)
	}
}

func TestCommitSubjectPrefixMatchesCommitSubject(t *testing.T) {
	setID := "2026-07-04-demo"
	prefix := commitSubjectPrefix(setID)
	subject := CommitSubject(setID, "01-a")
	if !strings.HasPrefix(subject, prefix) {
		t.Fatalf("CommitSubject %q does not start with prefix %q", subject, prefix)
	}
}

func TestFormatVerifyCommand(t *testing.T) {
	tests := []struct {
		name     string
		setID    string
		agents   []string
		effort   string
		want     string
	}{
		{
			name:  "set id only",
			setID: "2026-07-04-resume-context",
			want:  "pop tasks verify 2026-07-04-resume-context",
		},
		{
			name:   "with agent and effort",
			setID:  "demo",
			agents: []string{"claude", "codex"},
			effort: "high",
			want:   "pop tasks verify demo --agent claude --agent codex --effort high",
		},
		{
			name:  "quoted set id",
			setID: "my set",
			want:  "pop tasks verify 'my set'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatVerifyCommand(tt.setID, tt.agents, tt.effort)
			if got != tt.want {
				t.Fatalf("FormatVerifyCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}
