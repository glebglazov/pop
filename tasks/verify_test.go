package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

func TestParseVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		raw          string
		wantVerdict  Verdict
		wantFindings string // substring the findings must contain ("" = must be empty)
		wantSummary  string
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
		{name: "with summary", raw: "VERDICT: FIXABLE\nSUMMARY: widget never renders\nFINDINGS: criterion 2 unmet", wantVerdict: VerdictFixable, wantFindings: "criterion 2 unmet", wantSummary: "widget never renders"},
		{name: "summary omitted", raw: "VERDICT: FIXABLE\nFINDINGS: criterion 2 unmet", wantVerdict: VerdictFixable, wantFindings: "criterion 2 unmet", wantSummary: ""},
		{name: "summary not in findings fallback", raw: "VERDICT: FIXABLE\nSUMMARY: one line\ndetails without findings label", wantVerdict: VerdictFixable, wantFindings: "details without findings label", wantSummary: "one line"},
		{name: "summary prose in findings fallback kept", raw: "VERDICT: FIXABLE\nSUMMARY of the retry gap is unclear", wantVerdict: VerdictFixable, wantFindings: "SUMMARY of the retry gap is unclear", wantSummary: ""},
		{name: "summary label inside findings body ignored", raw: "VERDICT: FIXABLE\nFINDINGS: first line\nSUMMARY: nested label", wantVerdict: VerdictFixable, wantFindings: "first line\nSUMMARY: nested label", wantSummary: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotV, gotF, gotS := ParseVerdict(tt.raw)
			if gotV != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q", gotV, tt.wantVerdict)
			}
			if tt.wantFindings == "" {
				if strings.TrimSpace(gotF) != "" {
					t.Fatalf("findings = %q, want empty", gotF)
				}
				if gotS != "" {
					t.Fatalf("summary = %q, want empty", gotS)
				}
				return
			}
			if !strings.Contains(gotF, tt.wantFindings) {
				t.Fatalf("findings = %q, want to contain %q", gotF, tt.wantFindings)
			}
			if gotS != tt.wantSummary {
				t.Fatalf("summary = %q, want %q", gotS, tt.wantSummary)
			}
		})
	}
}

func TestParseVerdictMalformedIncludesRawForHuman(t *testing.T) {
	t.Parallel()
	raw := "I think it is basically fine."
	v, findings, _ := ParseVerdict(raw)
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
	d := newTestDeps(t)
	root := t.TempDir()
	defPath := filepath.Join(root, "tasks")
	setupManifest(t, defPath, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d.Git = git
	return d, defPath
}

// stubGit answers only the git commands the verify core issues, without touching
// a real repository. The common dir is among them: taking the checkout resolves
// the repository the claim is keyed by (ADR-0238).
func stubGit(head, log, diff string) *deps.MockGit {
	return &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			return "/repo/.git", nil
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
	s, err := store.Open(DrainStorePathWith(d), func(int, string) bool { return true })
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
	t.Parallel()
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
	t.Parallel()
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

// TestVerifyResolvedSetForceRunsDespiteCachedPass: the explicit `pop tasks verify`
// path always invokes the Verifier, even when a PASS verdict is already cached
// at the current HEAD, and overwrites it with the new result (ADR-0096).
func TestVerifyResolvedSetForceRunsDespiteCachedPass(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("shaX\n", "", ""))
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaX", Verdict: "PASS"})

	called := false
	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			called = true
			return "VERDICT: NEEDS-HUMAN\nFINDINGS: forced re-run\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if !called {
		t.Fatal("explicit verify path did not force-run despite a cached PASS")
	}
	if res.Verdict != VerdictNeedsHuman {
		t.Fatalf("verdict = %q, want NEEDS-HUMAN", res.Verdict)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaX")
	if stored == nil || stored.Verdict != "NEEDS-HUMAN" || stored.Findings != "forced re-run" {
		t.Fatalf("stored verdict = %+v, want NEEDS-HUMAN forced re-run", stored)
	}
}

func TestVerifyResolvedSetMalformedResponseParksNeedsHuman(t *testing.T) {
	t.Parallel()
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

// TestVerifyResolvedSetCarriesWorkStatAndRangeNotTheDiff: the prompt hands the
// Verifier the commit range and the complete stat, says the stat is complete, and
// names the command that fetches a file's diff — the diff bodies themselves are
// never inlined (they run to megabytes on a large set).
func TestVerifyResolvedSetCarriesWorkStatAndRangeNotTheDiff(t *testing.T) {
	t.Parallel()
	var diffArgs [][]string
	git := &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			return "/repo/.git", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return "sha1\n", nil
		case len(args) >= 1 && args[0] == "log":
			return "commitHash1\n", nil
		case len(args) >= 1 && args[0] == "diff":
			diffArgs = append(diffArgs, args)
			return " tasks/verify.go | 12 ++++---\n 1 file changed\n", nil
		}
		return "", nil
	}}
	d, defPath := setupVerifyFixture(t, git)
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
	for _, want := range []string{
		"Commit range: commitHash1^..HEAD",
		"tasks/verify.go | 12 ++++---",
		"is complete",
		"not evidence of missing work",
		"git diff commitHash1^..HEAD -- <path>",
	} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, gotPrompt)
		}
	}
	if strings.Contains(gotPrompt, "```diff") {
		t.Fatalf("prompt must not inline the diff:\n%s", gotPrompt)
	}
	if len(diffArgs) != 1 || diffArgs[0][1] != "--stat" {
		t.Fatalf("git diff calls = %v, want exactly one `diff --stat`", diffArgs)
	}
}

func TestVerifyResolvedSetIncludesSpecInPromptWhenPresent(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	specPath := filepath.Join(defPath, "demo", "spec.md")
	if err := os.WriteFile(specPath, []byte("SPEC-BODY-MARKER\n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
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
	if !strings.Contains(gotPrompt, "SPEC-BODY-MARKER") {
		t.Fatalf("prompt missing spec content:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "acceptance criteria above remain authoritative") {
		t.Fatalf("prompt missing spec-is-context-only framing:\n%s", gotPrompt)
	}
}

func TestVerifyResolvedSetOmitsSpecSectionWhenAbsent(t *testing.T) {
	t.Parallel()
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
	if strings.Contains(gotPrompt, "## Spec") {
		t.Fatalf("prompt should omit Spec section when spec.md is absent:\n%s", gotPrompt)
	}
}

func TestVerifyResolvedSetTreatsLegacyPRDAsSpecLess(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	legacyPath := filepath.Join(defPath, "demo", "prd.md")
	if err := os.WriteFile(legacyPath, []byte("LEGACY-PRD-BODY-MARKER\n"), 0o644); err != nil {
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
	if strings.Contains(gotPrompt, "## Spec") || strings.Contains(gotPrompt, "LEGACY-PRD-BODY-MARKER") {
		t.Fatalf("a set with only a legacy prd.md must be treated as spec-less, no fallback read:\n%s", gotPrompt)
	}
}

func TestReadSpecAbsentIsNotError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := &Manifest{Dir: root}
	d := &Deps{FS: deps.NewRealFileSystem()}
	if _, ok := readSpec(d, m); ok {
		t.Fatal("readSpec: expected false for absent spec.md")
	}
}

func TestReadSpecPresentReturnsContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "spec.md"), []byte("  hello spec  \n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	m := &Manifest{Dir: root}
	d := &Deps{FS: deps.NewRealFileSystem()}
	got, ok := readSpec(d, m)
	if !ok || got != "hello spec" {
		t.Fatalf("readSpec = %q, %v, want %q, true", got, ok, "hello spec")
	}
}

func TestReadSpecDoesNotFallBackToLegacyPRD(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prd.md"), []byte("legacy prd\n"), 0o644); err != nil {
		t.Fatalf("write prd.md: %v", err)
	}
	m := &Manifest{Dir: root}
	d := &Deps{FS: deps.NewRealFileSystem()}
	if _, ok := readSpec(d, m); ok {
		t.Fatal("readSpec: expected false when only a legacy prd.md is present, no fallback read")
	}
}

func TestVerifyResolvedSetUnknownSet(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	setID := "2026-07-04-demo"
	prefix := commitSubjectPrefix(setID)
	subject := CommitSubject(setID, "01-a")
	if !strings.HasPrefix(subject, prefix) {
		t.Fatalf("CommitSubject %q does not start with prefix %q", subject, prefix)
	}
}

func TestFormatVerifyCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		setID  string
		agents []string
		effort string
		want   string
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

// scriptedVerifyRunner is a CommandRunner that replays canned claude-stream-json
// output per attempt. It lets tests exercise multi-agent verifier fall-through
// without spawning real agents.
type scriptedVerifyRunner struct {
	calls   int
	scripts []string // raw output to emit for each Start call
	names   []string
}

func (r *scriptedVerifyRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *scriptedVerifyRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.calls++
	r.names = append(r.names, name)
	script := ""
	if r.calls <= len(r.scripts) {
		script = r.scripts[r.calls-1]
	}
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(stdout, line)
	}
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	proc.done <- waitResult{exitCode: 0}
	return proc, nil
}

// StartWithEnv keeps the scripted runner usable for a preset whose invocation
// carries environment entries (kimi, ADR-0164); the env itself is irrelevant to a
// scripted script, so it replays the same way.
func (r *scriptedVerifyRunner) StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return r.Start(ctx, dir, stdout, stderr, name, args...)
}

func (r *probeNeutralRunner) StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return r.Start(ctx, dir, stdout, stderr, name, args...)
}

func TestRunConfiguredVerifierPersistsMultiAgentFallback(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"You've hit your session limit"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(string) (string, error) { return "/bin/claude", nil }
	d.Runner = &probeNeutralRunner{inner: runner}

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"claude", "claude --model opus"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS verdict", raw)
	}

	// Both invocations are persisted: the quota-paused attempt and the parsed one.
	runsDir := capturedRunsDir(taskSetDir)
	pairs := listRunFilePairs(t, runsDir)
	if len(pairs) != 2 {
		t.Fatalf("want 2 verify runs, got %d", len(pairs))
	}

	// First run: quota-paused fall-through, no verdict.
	meta1 := readCapturedRunMeta(t, pairs[0].meta)
	if meta1.Phase != "verify" {
		t.Fatalf("run1 phase = %q, want verify", meta1.Phase)
	}
	if meta1.Outcome != streamOutcomeQuotaPaused {
		t.Fatalf("run1 outcome = %q, want %s", meta1.Outcome, streamOutcomeQuotaPaused)
	}
	if meta1.WorkSHA != "sha1" {
		t.Fatalf("run1 work_sha = %q, want sha1", meta1.WorkSHA)
	}
	if meta1.Verdict != "" {
		t.Fatalf("run1 should have no verdict, got %q", meta1.Verdict)
	}

	// Second run: parsed invocation carries the PASS verdict.
	meta2 := readCapturedRunMeta(t, pairs[1].meta)
	if meta2.Phase != "verify" || meta2.Outcome != streamOutcomeCompleted {
		t.Fatalf("run2 meta = %+v", meta2)
	}
	if meta2.WorkSHA != "sha1" {
		t.Fatalf("run2 work_sha = %q, want sha1", meta2.WorkSHA)
	}
	if meta2.Verdict != "PASS" {
		t.Fatalf("run2 verdict = %q, want PASS", meta2.Verdict)
	}
}

func TestRunConfiguredVerifierUnparseableOutputPersistsWithNeedsHumanVerdict(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"I think it looks okay"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"I think it looks okay"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"I think it looks okay"}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(string) (string, error) { return "/bin/claude", nil }
	d.Runner = &probeNeutralRunner{inner: runner}

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "I think it looks okay") {
		t.Fatalf("raw = %q", raw)
	}

	pairs := listRunFilePairs(t, capturedRunsDir(taskSetDir))
	if len(pairs) != 3 {
		t.Fatalf("want 3 verify runs (default max tries), got %d", len(pairs))
	}
	meta := readCapturedRunMeta(t, pairs[2].meta)
	if meta.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("verdict = %q, want NEEDS-HUMAN", meta.Verdict)
	}
}

func TestVerifyResolvedSetCacheHitWritesNoRun(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("sha1\n", "", ""))
	// Seed a cached verdict at sha1.
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.PutVerifyVerdict(store.VerifyVerdict{
		Repo:    "/repo/.git",
		SetID:   "demo",
		WorkSHA: "sha1",
		Verdict: "PASS",
	}); err != nil {
		t.Fatalf("seed verdict: %v", err)
	}
	_ = d.CloseStore()

	// ensureVerifyVerdict is the cache-first path used by the drain. A cache hit
	// must not invoke the verifier and therefore must not write a Captured run.
	m, err := loadVerifiableManifest(d, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
	})
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	v, err := ensureVerifyVerdict(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
	}, m, "sha1")
	if err != nil {
		t.Fatalf("ensureVerifyVerdict: %v", err)
	}
	if v == nil || v.Verdict != "PASS" {
		t.Fatalf("cached verdict = %+v", v)
	}

	if _, err := os.Stat(capturedRunsDir(filepath.Join(defPath, "demo"))); !os.IsNotExist(err) {
		t.Fatalf("cache hit wrote a verify run")
	}
}

// TestVerifyResolvedSetAcceptWritesHumanAuthoredPass: `pop tasks verify <set>
// --accept "<note>"` records a human-authored PASS at the current work SHA
// without running the Verifier (ADR-0103). The stored row is a plain PASS
// flagged human-authored, carrying the note and the set's AFK scope — so status
// derivation flips the set to verified with no change to ResolveVerifiedStatus.
func TestVerifyResolvedSetAcceptWritesHumanAuthoredPass(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("shaACC\n", "", ""))
	var out bytes.Buffer
	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:      &out,
		Accept:      true,
		AcceptNote:  "the flaky-looking retry is intentional",
		runVerifier: func(string) (string, error) { t.Fatal("accept must not invoke the Verifier"); return "", nil },
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if res.Verdict != VerdictPass || res.WorkSHA != "shaACC" {
		t.Fatalf("result = %+v, want PASS at shaACC", res)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaACC")
	if stored == nil || stored.Verdict != "PASS" {
		t.Fatalf("stored verdict = %+v, want PASS", stored)
	}
	if !stored.HumanAuthored || stored.Note != "the flaky-looking retry is intentional" {
		t.Fatalf("stored verdict = %+v, want human-authored PASS carrying the note", stored)
	}
	// Scope is the set's AFK count (one done AFK task), so the accept behaves like
	// any PASS under scope-growth invalidation (ADR-0101).
	if stored.Scope != 1 {
		t.Fatalf("stored scope = %d, want 1 (the set's AFK count)", stored.Scope)
	}
	if !strings.Contains(out.String(), "Accepted") || !strings.Contains(out.String(), "the flaky-looking retry is intentional") {
		t.Fatalf("output missing accepted verdict/note:\n%s", out.String())
	}
}

// TestVerifyResolvedSetAcceptOverridesNonPassAtSameSHA: accepting overwrites a
// non-PASS verdict already recorded at the current SHA (PASS idempotency on the
// (repo, set, work_sha) key), so the human override wins.
func TestVerifyResolvedSetAcceptOverridesNonPassAtSameSHA(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("shaX\n", "", ""))
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaX", Verdict: "NEEDS-HUMAN", Findings: "needs a human decision"})

	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true, AcceptNote: "reviewed — non-blocking",
	}); err != nil {
		t.Fatalf("verifyResolvedSet accept: %v", err)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaX")
	if stored == nil || stored.Verdict != "PASS" || !stored.HumanAuthored || stored.Findings != "" {
		t.Fatalf("stored verdict = %+v, want the accept to overwrite NEEDS-HUMAN with a clean human PASS", stored)
	}
}

// setupVerifyFixtureTasks is setupVerifyFixture with a caller-supplied task list,
// so a test can stand up a set that already carries Remediation tasks (to
// exercise the human over-cap path).
func setupVerifyFixtureTasks(t *testing.T, git *deps.MockGit, tasks []Task) (*Deps, string) {
	t.Helper()
	d := newTestDeps(t)
	root := t.TempDir()
	defPath := filepath.Join(root, "tasks")
	setupManifest(t, defPath, "demo", tasks)
	d.Git = git
	return d, defPath
}

// TestVerifyResolvedSetRemediateFromNeedsHumanSpawnsTask: `pop tasks verify <set>
// --remediate "<note>"` spawns a Remediation task from a NEEDS-HUMAN verdict —
// where the auto path would park — carrying both the recorded findings and the
// human note, and invalidates the set's cached verdicts (ADR-0103). The Verifier
// is not run.
func TestVerifyResolvedSetRemediateFromNeedsHumanSpawnsTask(t *testing.T) {
	t.Parallel()
	d, defPath := setupVerifyFixture(t, stubGit("shaNH\n", "", ""))
	// The auto path parks a NEEDS-HUMAN; a human authorises a remediation instead.
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaNH", Verdict: "NEEDS-HUMAN", Findings: "the retry policy needs a human call"})

	var out bytes.Buffer
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:        &out,
		Remediate:     true,
		RemediateNote: "cap the retries at 3 and log the drop",
		runVerifier:   func(string) (string, error) { t.Fatal("remediate must not invoke the Verifier"); return "", nil },
	}); err != nil {
		t.Fatalf("verifyResolvedSet remediate: %v", err)
	}

	// The Remediation task body carries the findings and the human note.
	body, err := os.ReadFile(filepath.Join(defPath, "demo", "02-remediation.md"))
	if err != nil {
		t.Fatalf("read remediation body: %v", err)
	}
	for _, want := range []string{"the retry policy needs a human call", "cap the retries at 3 and log the drop", "## Human note", "## Acceptance criteria"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("remediation body missing %q:\n%s", want, body)
		}
	}

	// The manifest reloads valid with a new open AFK task — the set is drainable.
	reloaded := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))
	if !reloaded.Valid {
		t.Fatalf("reloaded manifest invalid: %v", reloaded.Errors)
	}
	var rem *Task
	for i := range reloaded.Tasks {
		if reloaded.Tasks[i].ID == "02-remediation" {
			rem = &reloaded.Tasks[i]
		}
	}
	if rem == nil || rem.Type != "AFK" || rem.Status != "open" {
		t.Fatalf("remediation task = %+v, want a new open AFK task", rem)
	}

	// Spawning invalidated the set's cached verdicts.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaNH"); stored != nil {
		t.Fatalf("cached verdict = %+v, want nil after remediation invalidated the episode", stored)
	}
	if !strings.Contains(out.String(), "Spawned remediation") || !strings.Contains(out.String(), "cap the retries at 3 and log the drop") {
		t.Fatalf("output missing spawned-remediation summary/note:\n%s", out.String())
	}
}

// TestHumanRemediateRequiresVerifyFailedMark: `pop tasks verify --remediate` is
// refused unless the set's Verification mark is verify-failed (ADR-0217). Each
// non-failed mark is refused by name with a pointer at the authoring guide, and
// a refused call writes nothing; a verify-failed set still remediates.
func TestHumanRemediateRequiresVerifyFailedMark(t *testing.T) {
	t.Parallel()

	assertNoRemediationWrite := func(t *testing.T, d *Deps, defPath string, before *Manifest, verdictSHA string, wantBlockedBy map[string][]string) {
		t.Helper()
		entries, err := os.ReadDir(filepath.Join(defPath, "demo"))
		if err != nil {
			t.Fatalf("readdir set folder: %v", err)
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), "remediation") && strings.HasSuffix(e.Name(), ".md") {
				t.Fatalf("no remediation markdown must be written on refusal, found %s", e.Name())
			}
		}
		reloaded := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))
		if !reloaded.Valid {
			t.Fatalf("reloaded manifest invalid: %v", reloaded.Errors)
		}
		if len(reloaded.Tasks) != len(before.Tasks) {
			t.Fatalf("manifest task count = %d, want %d (no append on refusal)", len(reloaded.Tasks), len(before.Tasks))
		}
		for id, want := range wantBlockedBy {
			assertHITLBlockedBy(t, reloaded, id, want)
		}
		if verdictSHA != "" {
			if got := readStoredVerdict(t, d, "/repo/.git", "demo", verdictSHA); got == nil {
				t.Fatalf("verdict at %s must survive a refused remediation", verdictSHA)
			}
		}
	}

	t.Run("refuses verified", func(t *testing.T) {
		d, defPath := setupVerifyFixtureTasks(t, stubGit("shaV\n", "", ""), hitlGateRemediationSet())
		seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaV", Verdict: "PASS"})
		before := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))
		wantBlocked := map[string][]string{
			"03-mid-gate": {"01-a"},
			"04-approval": {"02-b"},
		}

		_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output: &bytes.Buffer{}, Remediate: true, RemediateNote: "follow-up",
			runVerifier: func(string) (string, error) { t.Fatal("must not verify"); return "", nil },
		})
		if err == nil {
			t.Fatal("remediate must refuse a verified set")
		}
		msg := err.Error()
		if !strings.Contains(msg, "verified") || !strings.Contains(msg, "not verify-failed") {
			t.Fatalf("refusal must name the verified mark: %v", err)
		}
		if !strings.Contains(msg, "pop tasks authoring-guide") {
			t.Fatalf("refusal must point at authoring-guide: %v", err)
		}
		assertNoRemediationWrite(t, d, defPath, before, "shaV", wantBlocked)
	})

	t.Run("refuses unverified", func(t *testing.T) {
		d, defPath := setupVerifyFixture(t, stubGit("shaU\n", "", ""))
		before := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))

		_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output: &bytes.Buffer{}, Remediate: true, RemediateNote: "follow-up",
			runVerifier: func(string) (string, error) { t.Fatal("must not verify"); return "", nil },
		})
		if err == nil {
			t.Fatal("remediate must refuse an unverified set")
		}
		msg := err.Error()
		if !strings.Contains(msg, "unverified") || !strings.Contains(msg, "pop tasks authoring-guide") {
			t.Fatalf("refusal must name unverified and point at authoring-guide: %v", err)
		}
		assertNoRemediationWrite(t, d, defPath, before, "", nil)
	})

	t.Run("refuses absent mark", func(t *testing.T) {
		d, defPath := setupVerifyFixtureTasks(t, stubGit("shaN\n", "", ""), []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		})
		before := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))

		_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output: &bytes.Buffer{}, Remediate: true, RemediateNote: "follow-up",
			runVerifier: func(string) (string, error) { t.Fatal("must not verify"); return "", nil },
		})
		if err == nil {
			t.Fatal("remediate must refuse a non-terminal set (absent mark)")
		}
		msg := err.Error()
		if !strings.Contains(msg, "none") || !strings.Contains(msg, "pop tasks authoring-guide") {
			t.Fatalf("refusal must name the absent mark and point at authoring-guide: %v", err)
		}
		assertNoRemediationWrite(t, d, defPath, before, "", nil)
	})

	t.Run("succeeds on verify-failed", func(t *testing.T) {
		d, defPath := setupVerifyFixture(t, stubGit("shaF\n", "", ""))
		seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaF", Verdict: "NEEDS-HUMAN", Findings: "gap"})

		if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output: &bytes.Buffer{}, Remediate: true, RemediateNote: "fix the gap",
			runVerifier: func(string) (string, error) { t.Fatal("must not verify"); return "", nil },
		}); err != nil {
			t.Fatalf("remediate on verify-failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(defPath, "demo", "02-remediation.md")); err != nil {
			t.Fatalf("remediation markdown must be written: %v", err)
		}
	})
}

// TestVerifyResolvedSetRemediateOverCapSpawnsTask: a human-triggered remediation
// spawns a task even when the set has already exhausted the auto remediation
// depth cap (ADR-0103) — the human authorises the fix the auto path refuses. The
// spawned task is human-origin, so the derived depth resets to zero and the auto
// budget is re-enabled (ADR-0105): the human push does not itself count.
func TestVerifyResolvedSetRemediateOverCapSpawnsTask(t *testing.T) {
	t.Parallel()
	// The set already carries DefaultMaxRemediationDepth auto remediation tasks:
	// the auto FIXABLE-under-cap path would spawn nothing.
	d, defPath := setupVerifyFixtureTasks(t, stubGit("shaCap\n", "", ""), remediationSet(DefaultMaxRemediationDepth))
	// Human --remediate requires a verify-failed mark (ADR-0217).
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaCap", Verdict: "FIXABLE", Findings: "still one gap"})

	var out bytes.Buffer
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:        &out,
		Remediate:     true,
		RemediateNote: "the last gap is small; push past the cap",
	}); err != nil {
		t.Fatalf("verifyResolvedSet remediate over cap: %v", err)
	}

	reloaded := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))
	if !reloaded.Valid {
		t.Fatalf("reloaded manifest invalid: %v", reloaded.Errors)
	}
	// A human remediation resets the auto budget: depth counts only consecutive
	// auto-origin tasks since the last human one, so it drops to zero (ADR-0105).
	if got := remediationDepth(reloaded); got != 0 {
		t.Fatalf("remediation depth = %d, want 0 (human remediation resets the auto budget)", got)
	}
	// The new task is numbered one past the highest existing ordinal and tagged
	// human-origin.
	nextID := fmt.Sprintf("%02d-remediation", DefaultMaxRemediationDepth+2)
	var spawned *Task
	for i := range reloaded.Tasks {
		if reloaded.Tasks[i].ID == nextID {
			spawned = &reloaded.Tasks[i]
		}
	}
	if spawned == nil {
		t.Fatalf("reloaded manifest missing spawned task %q", nextID)
	}
	if spawned.Origin != RemediationOriginHuman {
		t.Fatalf("spawned task origin = %q, want human", spawned.Origin)
	}
	body, err := os.ReadFile(filepath.Join(defPath, "demo", nextID+".md"))
	if err != nil {
		t.Fatalf("read remediation body %q: %v", nextID, err)
	}
	if !strings.Contains(string(body), "the last gap is small; push past the cap") {
		t.Fatalf("remediation body missing human note:\n%s", body)
	}
}

// TestBuildVerifierPromptForwardFeedsAcceptedNote: a non-empty prior human note
// is folded into the Verifier prompt as context (ADR-0103), explicitly framed as
// non-suppressing; an empty note adds no such section.
func TestBuildVerifierPromptForwardFeedsAcceptedNote(t *testing.T) {
	t.Parallel()
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)

	withNote := buildVerifierPrompt(d, m, "sha1", workDiffView{}, "the retry cap is deliberate", "")
	if !strings.Contains(withNote, "Prior human note") {
		t.Fatalf("prompt must carry a prior-human-note section:\n%s", withNote)
	}
	if !strings.Contains(withNote, "the retry cap is deliberate") {
		t.Fatalf("prompt must include the note text:\n%s", withNote)
	}
	if !strings.Contains(withNote, "still") { // "a real regression here still fails" — context, not suppression
		t.Fatalf("prompt must frame the note as context, not suppression:\n%s", withNote)
	}

	withoutNote := buildVerifierPrompt(d, m, "sha1", workDiffView{}, "", "")
	if strings.Contains(withoutNote, "Prior human note") {
		t.Fatalf("prompt must omit the note section when no note is given:\n%s", withoutNote)
	}
}

func TestBuildVerifierPromptAsksForOptionalSummary(t *testing.T) {
	t.Parallel()
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	prompt := buildVerifierPrompt(d, m, "sha1", workDiffView{}, "", "")
	for _, want := range []string{
		"SUMMARY:",
		"what needs fixing",
		"optional",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRunConfiguredVerifierAllAgentsQuotaPausedReturnsQuotaPause(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"error","message":"You've hit your usage limit. try again at 11:59 PM."}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(string) (string, error) { return "/bin/codex", nil }
	d.Runner = &probeNeutralRunner{inner: runner}

	_, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"codex"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, time.Minute, nil)
	if err == nil {
		t.Fatal("expected verifier quota pause error")
	}
	qp, ok := AsVerifyQuotaPause(err)
	if !ok {
		t.Fatalf("expected VerifyQuotaPause, got %v", err)
	}
	if qp.Preset != "codex" {
		t.Fatalf("preset = %q, want codex", qp.Preset)
	}
	if qp.ResetAt.IsZero() {
		t.Fatal("expected non-zero reset time")
	}

	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = d.CloseStore() }()
	if v, err := s.GetVerifyVerdict("/repo/.git", "demo", "sha1"); err != nil || v != nil {
		t.Fatalf("quota pause must not persist a verdict: v=%+v err=%v", v, err)
	}
}

// verifyRetryDeps is a verify fixture whose data dir is isolated (newTestDeps),
// so a run that reads or records an Effort model skip touches a temp store
// rather than the developer's.
func verifyRetryDeps(t *testing.T, runner CommandRunner) *Deps {
	t.Helper()
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(string) (string, error) { return "/bin/claude", nil }
	d.Runner = &probeNeutralRunner{inner: runner}
	return d
}

// probeNeutralRunner satisfies availability probes without forwarding them to the
// wrapped runner, so test scripts are not consumed by probe calls.
type probeNeutralRunner struct {
	inner CommandRunner
}

func (r *probeNeutralRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *probeNeutralRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	if IsAgentAvailabilityProbeCommand(name, args) {
		proc := &ManagedProcess{done: make(chan waitResult, 1)}
		proc.done <- waitResult{exitCode: 0}
		return proc, nil
	}
	return r.inner.Start(ctx, dir, stdout, stderr, name, args...)
}

func instantVerifyRetryConfig(maxTries int) *config.Config {
	v := maxTries
	return &config.Config{Work: &config.WorkConfig{
		Verify: &config.VerifyConfig{MaxTries: &v, AttemptRetryDelays: []string{}},
	}}
}

func TestRunConfiguredVerifierDoesNotRetryCleanNeedsHuman(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: NEEDS-HUMAN\nFINDINGS: ambiguous spec"}`,
		},
	}
	d := verifyRetryDeps(t, runner)

	raw, _, err := runConfiguredVerifier(d, instantVerifyRetryConfig(3), verifierSelection{
		Agents: []string{"claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "NEEDS-HUMAN") {
		t.Fatalf("raw = %q", raw)
	}
	if runner.calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on clean NEEDS-HUMAN)", runner.calls)
	}
}

func TestRunConfiguredVerifierRetriesUnparseableWithDelayNotice(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"garbled"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\n"}`,
		},
	}
	d := verifyRetryDeps(t, runner)
	var out bytes.Buffer

	raw, _, err := runConfiguredVerifier(d, instantVerifyRetryConfig(2), verifierSelection{
		Agents: []string{"claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS on retry", raw)
	}
	if runner.calls != 2 {
		t.Fatalf("calls = %d, want 2", runner.calls)
	}
	if !strings.Contains(out.String(), "Retrying with preserved changes") {
		t.Fatalf("missing retry notice:\n%s", out.String())
	}
}

func TestRunConfiguredVerifierFallsThroughAfterRetryExhausted(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"garbled one"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"garbled two"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
		},
	}
	d := verifyRetryDeps(t, runner)
	two := 2
	cfg := &config.Config{Work: &config.WorkConfig{
		Verify: &config.VerifyConfig{MaxTries: &two, AttemptRetryDelays: []string{}},
	}}

	raw, _, err := runConfiguredVerifier(d, cfg, verifierSelection{
		Agents: []string{"claude", "claude --model opus"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS from fallback agent", raw)
	}
	if runner.calls != 3 {
		t.Fatalf("calls = %d, want 2 retries on first agent + 1 on second", runner.calls)
	}
}

func TestRunConfiguredVerifierTimeoutRetriesThenFallsThrough(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	// The fake process only has to outlast the 50ms attempt deadline below so
	// every attempt times out; a small hang keeps the four-attempt run fast
	// instead of paying 2s of real sleep per call.
	runner := &slowVerifyRunner{delay: 150 * time.Millisecond}
	d := verifyRetryDeps(t, runner)

	raw, _, err := runConfiguredVerifier(d, instantVerifyRetryConfig(2), verifierSelection{
		Agents: []string{"claude", "claude --model opus"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, 50*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	// Every attempt times out: 2 tries on the first agent (retry consumes the
	// verify cap), then a fall-through to the second agent for 2 more.
	if runner.calls != 4 {
		t.Fatalf("calls = %d, want 4 (2 tries per agent, timeout retries)", runner.calls)
	}
	if strings.TrimSpace(raw) != "" {
		t.Fatalf("raw = %q, want empty output when every attempt times out", raw)
	}
}

func TestRunConfiguredVerifierTimeoutRetriesThenParses(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	// First attempt hangs past the deadline (timeout); the retry returns a
	// clean PASS, proving a verify timeout is retry-eligible and consumes a slot.
	runner := &timeoutThenScriptRunner{
		timeoutBefore: 1,
		script:        `{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
	}
	d := verifyRetryDeps(t, runner)

	raw, _, err := runConfiguredVerifier(d, instantVerifyRetryConfig(3), verifierSelection{
		Agents: []string{"claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, 50*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS on retry after timeout", raw)
	}
	if runner.calls != 2 {
		t.Fatalf("calls = %d, want 2 (timeout then parsed pass)", runner.calls)
	}
}

func TestRunConfiguredVerifierUsesVerifyMaxTriesOverride(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"garbled"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"garbled"}`,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\n"}`,
		},
	}
	d := verifyRetryDeps(t, runner)
	verify := 2
	cfg := &config.Config{Work: &config.WorkConfig{
		Verify: &config.VerifyConfig{MaxTries: &verify, AttemptRetryDelays: []string{}},
	}}

	_, _, err := runConfiguredVerifier(d, cfg, verifierSelection{
		Agents: []string{"claude", "claude --model opus"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if runner.calls != 3 {
		t.Fatalf("calls = %d, want 2 on first agent + 1 on fallback (verify cap 2)", runner.calls)
	}
}

type slowVerifyRunner struct {
	delay time.Duration
	calls int
}

func (r *slowVerifyRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *slowVerifyRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.calls++
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	go func() {
		time.Sleep(r.delay)
		proc.done <- waitResult{exitCode: 0}
	}()
	return proc, nil
}

// timeoutThenScriptRunner hangs past the deadline for the first timeoutBefore
// calls, then emits script and returns immediately on subsequent calls.
type timeoutThenScriptRunner struct {
	timeoutBefore int
	script        string
	calls         int
}

func (r *timeoutThenScriptRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *timeoutThenScriptRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.calls++
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	if r.calls <= r.timeoutBefore {
		go func() {
			// Only outlast the caller's deadline (50ms in the sole caller) so the
			// attempt times out; the timeout path cannot SIGKILL this in-process
			// fake, so waitForDone pays this hang in full — keep it small (mirrors
			// slowVerifyRunner's 150ms) instead of a multi-second real sleep.
			time.Sleep(150 * time.Millisecond)
			proc.done <- waitResult{exitCode: 0}
		}()
		return proc, nil
	}
	for _, line := range strings.Split(r.script, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(stdout, line)
	}
	proc.done <- waitResult{exitCode: 0}
	return proc, nil
}

func TestRunConfiguredVerifierSkipsUnauthenticatedPresetByProbe(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(file string) (string, error) {
		if file == "cursor-agent" || file == "claude" {
			return "/bin/" + file, nil
		}
		return "", exec.ErrNotFound
	}
	d.Runner = &probeAwareVerifyRunner{
		inner: runner,
		probeOutputs: map[string]string{
			"cursor-agent": `{"isAuthenticated":false}`,
		},
	}

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"cursor", "claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS from fallback agent", raw)
	}
	if runner.calls != 1 {
		t.Fatalf("headless calls = %d, want 1 (cursor skipped by probe)", runner.calls)
	}
	if !strings.Contains(out.String(), "Verifier agent cursor unauthenticated; trying next") {
		t.Fatalf("missing probe skip line:\n%s", out.String())
	}
}

func TestRunConfiguredVerifierSkipsUnauthenticatedPresetByPassiveDetection(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	authLine := "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable."
	runner := &scriptedVerifyRunner{
		scripts: []string{
			authLine,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(file string) (string, error) {
		if file == "cursor-agent" || file == "claude" {
			return "/bin/" + file, nil
		}
		return "", exec.ErrNotFound
	}
	d.Runner = &probeAwareVerifyRunner{
		inner: runner,
		probeOutputs: map[string]string{
			"cursor-agent": "unparseable status output",
		},
	}

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"cursor", "claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS from fallback agent", raw)
	}
	if runner.calls != 2 {
		t.Fatalf("headless calls = %d, want 2 (cursor invoked then claude)", runner.calls)
	}
	if !strings.Contains(out.String(), "Verifier agent cursor unauthenticated; trying next") {
		t.Fatalf("missing passive auth skip line:\n%s", out.String())
	}

	pairs := listRunFilePairs(t, capturedRunsDir(taskSetDir))
	if len(pairs) != 2 {
		t.Fatalf("want 2 verify runs (auth skip + PASS), got %d", len(pairs))
	}
	meta1 := readCapturedRunMeta(t, pairs[0].meta)
	if meta1.Outcome != streamOutcomeAgentUnusable {
		t.Fatalf("run1 outcome = %q, want %s", meta1.Outcome, streamOutcomeAgentUnusable)
	}
}

func TestRunConfiguredVerifierFallsThroughKimiSubscriptionGate(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			kimiSubscriptionGateLine,
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d.Runner = &probeNeutralRunner{inner: runner}

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"kimi", "claude"}, Effort: "light",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS from the fallback agent", raw)
	}
	if runner.calls != 2 {
		t.Fatalf("headless calls = %d, want 2 (one kimi probe then claude)", runner.calls)
	}
	if !strings.Contains(out.String(), "Verifier agent kimi cannot run moonshot-ai/kimi-k2.7-code-highspeed and has no effort tier entry left; trying next") {
		t.Fatalf("missing verifier subscription-gate fall-through line:\n%s", out.String())
	}

	pairs := listRunFilePairs(t, capturedRunsDir(taskSetDir))
	if len(pairs) != 2 {
		t.Fatalf("want 2 verify runs (model refusal + PASS), got %d", len(pairs))
	}
	meta1 := readCapturedRunMeta(t, pairs[0].meta)
	if meta1.Outcome != streamOutcomeAgentUnusable {
		t.Fatalf("run1 outcome = %q, want %s", meta1.Outcome, streamOutcomeAgentUnusable)
	}
	if meta1.Reason != kimiSubscriptionGateLine {
		t.Fatalf("run1 reason = %q, want %q", meta1.Reason, kimiSubscriptionGateLine)
	}
	if meta1.Verdict != "" {
		t.Fatalf("run1 should have no verdict, got %q", meta1.Verdict)
	}

	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		t.Fatalf("read cooldowns: %v", err)
	}
	if _, ok := cooldowns["kimi"]; ok {
		t.Fatalf("a model refusal wrote a preset cooldown: %#v", cooldowns)
	}
}

// kimiVerifyLadderConfig gives kimi a multi-entry light tier and a verify cap,
// so a verify test can watch the tier being walked inside one try.
func kimiVerifyLadderConfig(maxTries int, models ...string) *config.Config {
	entries := make([]config.EffortModel, 0, len(models))
	for _, model := range models {
		entries = append(entries, config.EffortModel{Model: model})
	}
	tries := maxTries
	return &config.Config{
		Effort: map[string]config.EffortConfig{"kimi": {Light: entries}},
		Work: &config.WorkConfig{
			Verify: &config.VerifyConfig{MaxTries: &tries, AttemptRetryDelays: []string{}},
		},
	}
}

// A model refusal condemns the tier entry, not the verifier preset: the same
// preset restarts on the tier's next model, and with the verify cap at one try
// the PASS below can only come from a restart that spent no try (ADR-0168).
func TestRunConfiguredVerifierWalksTheEffortTierOnAModelRefusal(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			kimiSubscriptionGateLine,
			"VERDICT: PASS\nFINDINGS:\n",
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d.Runner = &probeNeutralRunner{inner: runner}
	cfg := kimiVerifyLadderConfig(1, "moonshot-ai/kimi-k2.7-code-highspeed", "moonshot-ai/kimi-k3")

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, cfg, verifierSelection{
		Agents: []string{"kimi"}, Effort: "light",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS from the tier's next model", raw)
	}
	if runner.calls != 2 {
		t.Fatalf("headless calls = %d, want 2 (refused model then its tier successor)", runner.calls)
	}
	if !strings.Contains(out.String(), "Verifier agent kimi cannot run moonshot-ai/kimi-k2.7-code-highspeed; trying moonshot-ai/kimi-k3 instead") {
		t.Fatalf("missing in-tier fall-through line:\n%s", out.String())
	}

	skips, err := ActiveAgentModelCooldownsWith(d, time.Now())
	if err != nil {
		t.Fatalf("read model skips: %v", err)
	}
	if len(skips) != 1 || skips[0].Preset != "kimi" || skips[0].Model != "moonshot-ai/kimi-k2.7-code-highspeed" {
		t.Fatalf("model skips = %#v, want only the refused entry recorded", skips)
	}

	runs, err := listSetRuns(d, taskSetDir)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var skipped int
	for _, run := range runs {
		if run.meta.Outcome != streamOutcomeModelSkipped {
			continue
		}
		skipped++
		if run.meta.Model != "moonshot-ai/kimi-k2.7-code-highspeed" {
			t.Fatalf("skipped run model = %q, want the refused entry", run.meta.Model)
		}
	}
	if skipped != 1 {
		t.Fatalf("skipped verify runs = %d, want 1:\n%#v", skipped, runs)
	}

	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		t.Fatalf("read cooldowns: %v", err)
	}
	if _, ok := cooldowns["kimi"]; ok {
		t.Fatalf("a walked model refusal wrote a preset cooldown: %#v", cooldowns)
	}
}

// A tier every entry of which an earlier run recorded as skipped is a
// preset-scoped stop: the verifier never spawns it and the agent list advances.
func TestRunConfiguredVerifierExhaustedTierAdvancesTheAgentList(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	runner := &scriptedVerifyRunner{
		scripts: []string{
			`{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","subtype":"success","result":"VERDICT: PASS\nFINDINGS:\n"}`,
		},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d.Runner = &probeNeutralRunner{inner: runner}
	if err := updateAgentModelCooldown(d, "kimi", "moonshot-ai/kimi-k2.7-code-highspeed", time.Time{}, true); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	var out bytes.Buffer
	raw, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"kimi", "claude"}, Effort: "light",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, &out, time.Minute, nil)
	if err != nil {
		t.Fatalf("runConfiguredVerifier: %v", err)
	}
	if !strings.Contains(raw, "VERDICT: PASS") {
		t.Fatalf("raw = %q, want PASS from the fallback agent", raw)
	}
	if runner.calls != 1 {
		t.Fatalf("headless calls = %d, want 1 (kimi never spawned)", runner.calls)
	}
	if !strings.Contains(out.String(), "Verifier agent kimi has no runnable model left in its effort tier; trying next") {
		t.Fatalf("missing exhausted-tier fall-through line:\n%s", out.String())
	}
}

// An exhausted tier is the whole diagnostic when it is the only agent: the run
// hard-errors rather than fabricating a verdict.
func TestRunConfiguredVerifierExhaustedTierAloneHardErrors(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d.Runner = &probeNeutralRunner{inner: &scriptedVerifyRunner{}}
	if err := updateAgentModelCooldown(d, "kimi", "moonshot-ai/kimi-k2.7-code-highspeed", time.Time{}, true); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	_, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"kimi"}, Effort: "light",
	}, t.TempDir(), "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, time.Minute, nil)
	assertExitCode(t, err, ExitSetup)
	if !strings.Contains(err.Error(), "every effort tier model is skipped: moonshot-ai/kimi-k2.7-code-highspeed") {
		t.Fatalf("error = %q, want the exhausted-tier diagnostic", err.Error())
	}
}

func TestRunConfiguredVerifierAllHumanHealingUnavailableHardErrors(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	authLine := "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable."
	runner := &scriptedVerifyRunner{
		scripts: []string{authLine},
	}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	realLookPath := exec.LookPath
	d.LookPath = func(file string) (string, error) {
		if file == "codex" {
			return "", exec.ErrNotFound
		}
		return realLookPath(file)
	}
	d.Runner = &probeAwareVerifyRunner{
		inner: runner,
		probeOutputs: map[string]string{
			"cursor-agent": "unparseable status output",
		},
	}

	_, _, err := runConfiguredVerifier(d, nil, verifierSelection{
		Agents: []string{"cursor", "codex"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", io.Discard, io.Discard, time.Minute, nil)
	assertExitCode(t, err, ExitSetup)
	errText := err.Error()
	if !strings.Contains(errText, "cursor:") || !strings.Contains(errText, authLine) {
		t.Fatalf("error = %q, want cursor auth diagnostic", errText)
	}
	if !strings.Contains(errText, "codex: \"binary not found on PATH\"") {
		t.Fatalf("error = %q, want codex missing-binary diagnostic", errText)
	}
}

// probeAwareVerifyRunner delegates headless invocations to inner and serves
// canned availability-probe output for cursor-agent status / claude auth status.
type probeAwareVerifyRunner struct {
	inner        *scriptedVerifyRunner
	probeOutputs map[string]string
	probeCalls   int
}

func (r *probeAwareVerifyRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *probeAwareVerifyRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	if IsAgentAvailabilityProbeCommand(name, args) {
		r.probeCalls++
		out, ok := r.probeOutputs[name]
		if !ok {
			proc := &ManagedProcess{done: make(chan waitResult, 1)}
			proc.done <- waitResult{exitCode: 0}
			return proc, nil
		}
		fmt.Fprint(stdout, out)
		proc := &ManagedProcess{done: make(chan waitResult, 1)}
		exitCode := 0
		if name == "cursor-agent" && out == "unparseable status output" {
			exitCode = 1
		}
		proc.done <- waitResult{exitCode: exitCode}
		return proc, nil
	}
	return r.inner.Start(ctx, dir, stdout, stderr, name, args...)
}
