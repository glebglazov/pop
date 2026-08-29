package tasks

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
)

// refinedSubject is a subject a Refiner could plausibly render under
// houseConvention for a pass that fixed something.
const refinedSubject = "refactor(auth): name the token helper for what it returns"

// setupRefineCommitFixture is a set whose agent work is done, sitting in a real
// git checkout with its base commit recorded — everything a refine pass needs
// before the only variable left is what the Refiner leaves behind.
func setupRefineCommitFixture(t *testing.T, convention string) *runTaskSetFixture {
	t.Helper()
	env := setupRunTaskSetFixtureWithKeys(t, "demo", signOffSet(), nil)
	_, _, head := runtimeHead(t, env.deps(), env.root)
	keys := map[string]any{"base_commit": head}
	if convention != "" {
		keys["commit_convention"] = convention
	}
	writeManifestWithSetKeys(t, filepath.Join(env.tasksDir, "demo"), signOffSet(), keys)
	return env
}

// fixingRefiner is a Refiner that edits one file in the checkout and answers
// with the reply the contract asks for.
func fixingRefiner(t *testing.T, root, name, reply string) func(string) (string, string, error) {
	t.Helper()
	return func(string) (string, string, error) {
		writeTaskMD(t, root, name, "the refiner's fix\n")
		return reply, "claude", nil
	}
}

func headSubject(t *testing.T, root string) string {
	t.Helper()
	out, err := realGitInDir(root, "log", "-1", "--format=%s")
	if err != nil {
		t.Fatalf("read head subject: %v", err)
	}
	return strings.TrimSpace(out)
}

// readRefineTrailer asks git itself for the commit's Pop-Refine trailer, so the
// test proves git parses the line as a trailer rather than merely that the text
// is somewhere in the message.
func readRefineTrailer(t *testing.T, root, sha string) string {
	t.Helper()
	out, err := realGitInDir(root, "log", "-1", "--format=%(trailers:key=Pop-Refine,valueonly)", sha)
	if err != nil {
		t.Fatalf("read trailer of %s: %v", sha, err)
	}
	return strings.TrimSpace(out)
}

func commitsSince(t *testing.T, root, since string) []string {
	t.Helper()
	out, err := realGitInDir(root, "log", "--format=%H", since+"..HEAD")
	if err != nil {
		t.Fatalf("log %s..HEAD: %v", since, err)
	}
	return strings.Fields(strings.TrimSpace(out))
}

// refinePassOpts drives the resolved core the way `pop tasks refine` does: no
// claim held by a caller, so the pass takes the checkout for itself.
func refinePassOpts(env *runTaskSetFixture, out *bytes.Buffer, run func(string) (string, string, error)) refineCoreOptions {
	return refineCoreOptions{
		DefPath:     env.tasksDir,
		RuntimePath: env.root,
		SetID:       "demo",
		Output:      out,
		runRefiner:  run,
	}
}

// TestRefinePassCommitsWhatTheRefinerFixed is the manual pass end to end: the
// edits the Refiner left in the working tree land as exactly one commit, under
// the subject the Refiner rendered, carrying the set's trailer — and the tree is
// clean behind it, which is the whole reason pop commits at all.
func TestRefinePassCommitsWhatTheRefinerFixed(t *testing.T) {
	t.Parallel()
	env := setupRefineCommitFixture(t, houseConvention)
	_, _, before := runtimeHead(t, env.deps(), env.root)

	var out bytes.Buffer
	res, err := refineResolvedSet(env.deps(), nil, refinePassOpts(env, &out,
		fixingRefiner(t, env.root, "refined.md",
			"COMMIT-SUBJECT: "+refinedSubject+"\n\n## Fixed\n\n- refined.md: named it properly.\n\n## Left\n\nNothing.")))
	if err != nil {
		t.Fatalf("refineResolvedSet: %v", err)
	}

	made := commitsSince(t, env.root, before)
	if len(made) != 1 {
		t.Fatalf("commits made = %v, want exactly one refine commit", made)
	}
	if res.CommitSHA != made[0] {
		t.Fatalf("result commit = %q, want %q", res.CommitSHA, made[0])
	}
	if got := headSubject(t, env.root); got != refinedSubject {
		t.Fatalf("refine commit subject = %q, want the rendered line verbatim", got)
	}
	if got := readRefineTrailer(t, env.root, made[0]); got != "demo" {
		t.Fatalf("Pop-Refine trailer = %q, want %q", got, "demo")
	}
	if dirty, err := RuntimeIsDirty(env.deps(), env.root); err != nil || dirty {
		t.Fatalf("tree dirty after the pass (err=%v)", err)
	}
	// The subject is an instruction to pop, not a finding: it stays out of the
	// document a human reads.
	if strings.Contains(res.Body, "COMMIT-SUBJECT") {
		t.Fatalf("the report carries the subject line:\n%s", res.Body)
	}
	if !strings.Contains(res.Body, "## Fixed") {
		t.Fatalf("the report lost its body:\n%s", res.Body)
	}
	if !strings.Contains(out.String(), "Refine commit: ") {
		t.Fatalf("output never names the refine commit:\n%s", out.String())
	}
}

// TestRefinePassThatFixedNothingCommitsNothing: a report-only pass leaves
// history exactly where it found it. An empty commit would put a subject
// claiming a refinement on a tree identical to its parent.
func TestRefinePassThatFixedNothingCommitsNothing(t *testing.T) {
	t.Parallel()
	env := setupRefineCommitFixture(t, houseConvention)
	_, _, before := runtimeHead(t, env.deps(), env.root)

	var out bytes.Buffer
	res, err := refineResolvedSet(env.deps(), nil, refinePassOpts(env, &out, func(string) (string, string, error) {
		return "## Fixed\n\nNothing to fix.\n\n## Left\n\nNothing.", "claude", nil
	}))
	if err != nil {
		t.Fatalf("refineResolvedSet: %v", err)
	}
	if res.CommitSHA != "" {
		t.Fatalf("result commit = %q, want none", res.CommitSHA)
	}
	if made := commitsSince(t, env.root, before); len(made) != 0 {
		t.Fatalf("commits made = %v, want none", made)
	}
	if res.Path == "" {
		t.Fatal("the pass wrote no report")
	}
}

// TestRefineCommitFallsBackToPopDefaultSubject covers both ways a rendered
// subject can be missing: a set that records no Commit convention (the Refiner
// is never asked, so anything it volunteers is a guess at a house style nobody
// stated) and a reply that simply carries no subject line.
func TestRefineCommitFallsBackToPopDefaultSubject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, convention, reply string }{
		{"no convention recorded", "", "COMMIT-SUBJECT: " + refinedSubject + "\n\n## Fixed\n\n- one fix."},
		{"no subject rendered", houseConvention, "## Fixed\n\n- one fix."},
		{"unusable rendering", houseConvention, "COMMIT-SUBJECT: <type>(<scope>): <summary>\n\n## Fixed\n\n- one fix."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := setupRefineCommitFixture(t, tc.convention)
			var out bytes.Buffer
			if _, err := refineResolvedSet(env.deps(), nil, refinePassOpts(env, &out,
				fixingRefiner(t, env.root, "refined.md", tc.reply))); err != nil {
				t.Fatalf("refineResolvedSet: %v", err)
			}
			if got := headSubject(t, env.root); got != "tasks(demo): refine" {
				t.Fatalf("refine commit subject = %q, want pop's default format", got)
			}
		})
	}
}

// TestRefineCommitLeavesACachedPassAlone: a refine commit is an ordinary commit
// past a PASS (ADR-0240). The verdict stands within its episode and the badge
// drifts; nothing on this path invalidates it.
func TestRefineCommitLeavesACachedPassAlone(t *testing.T) {
	t.Parallel()
	env := setupRefineCommitFixture(t, houseConvention)
	d := env.deps()
	repo, _, before := runtimeHead(t, d, env.root)
	if _, err := storeVerdict(d, store.VerifyVerdict{
		Repo: repo, SetID: "demo", WorkSHA: before, Verdict: "PASS", Scope: 1, ComputedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store verdict: %v", err)
	}

	var out bytes.Buffer
	opts := refinePassOpts(env, &out, fixingRefiner(t, env.root, "refined.md",
		"COMMIT-SUBJECT: "+refinedSubject+"\n\n## Fixed\n\n- one fix."))
	opts.Repo = repo
	res, err := refineResolvedSet(d, nil, opts)
	if err != nil {
		t.Fatalf("refineResolvedSet: %v", err)
	}
	if res.CommitSHA == "" {
		t.Fatal("the pass committed nothing")
	}
	if v := readStoredVerdict(t, d, repo, "demo", before); v == nil || v.Verdict != "PASS" {
		t.Fatalf("cached verdict = %+v, want the PASS untouched", v)
	}
}

// TestDrainCommitsTheRefinePass: the drain's own refine phase commits through
// the same helper the manual command does, so an automatic pass leaves the same
// single, trailered commit and the same clean tree.
func TestDrainCommitsTheRefinePass(t *testing.T) {
	t.Parallel()
	env := setupRefineCommitFixture(t, houseConvention)
	_, _, before := runtimeHead(t, env.deps(), env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("2\n") // Complete the sign-off at the gate.
	opts.ConfirmOut = &buf
	opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }
	opts.refineRunner = fixingRefiner(t, env.root, "refined.md",
		"COMMIT-SUBJECT: "+refinedSubject+"\n\n## Fixed\n\n- refined.md: named it properly.")

	if _, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return refineEnabledConfig(), nil
	}, opts); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}

	made := commitsSince(t, env.root, before)
	if len(made) != 1 {
		t.Fatalf("commits made = %v, want exactly one refine commit", made)
	}
	if got := headSubject(t, env.root); got != refinedSubject {
		t.Fatalf("refine commit subject = %q, want the rendered line verbatim", got)
	}
	if got := readRefineTrailer(t, env.root, made[0]); got != "demo" {
		t.Fatalf("Pop-Refine trailer = %q, want %q", got, "demo")
	}
	if dirty, err := RuntimeIsDirty(env.deps(), env.root); err != nil || dirty {
		t.Fatalf("tree dirty after the drain's refine phase (err=%v)", err)
	}
}

// TestSplitRefinerReply pins what pop reads as the pass's subject and what it
// leaves in the report. Everything not read as a subject falls back to pop's
// default format rather than failing the pass.
func TestSplitRefinerReply(t *testing.T) {
	t.Parallel()
	report := "## Fixed\n\n- a.go: renamed it."
	for _, tc := range []struct{ name, raw, subject, body string }{
		{"first line", "COMMIT-SUBJECT: " + refinedSubject + "\n\n" + report, refinedSubject, report},
		{"backtick wrapped", "COMMIT-SUBJECT: `" + refinedSubject + "`\n" + report, refinedSubject, report},
		{"bolded label", "**COMMIT SUBJECT:** " + refinedSubject + "\n\n" + report, refinedSubject, report},
		{"after a preamble", "Here is the report.\nCOMMIT-SUBJECT: " + refinedSubject + "\n\n" + report, refinedSubject, "Here is the report.\n\n" + report},
		{"absent", report, "", report},
		{"inside the report body", "## Fixed\n\nCOMMIT-SUBJECT: " + refinedSubject, "", "## Fixed\n\nCOMMIT-SUBJECT: " + refinedSubject},
		{"the whole reply", "COMMIT-SUBJECT: " + refinedSubject, "", "COMMIT-SUBJECT: " + refinedSubject},
	} {
		subject, body := splitRefinerReply(tc.raw)
		if subject != tc.subject || body != tc.body {
			t.Fatalf("%s: subject = %q / body = %q, want %q / %q", tc.name, subject, body, tc.subject, tc.body)
		}
	}
}
