package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

// verifyReportFixture is setupVerifyFixture with a git seam that answers a real
// commit range, so the report's header carries the facts a reader needs, and a
// clock fixed at at, so the document's name is predictable.
func verifyReportFixture(t *testing.T, at time.Time) (*Deps, string, string) {
	t.Helper()
	d, defPath := setupVerifyFixture(t, stubGit("abc123abc123\n", "aaa111\n", " tasks/verify.go | 3 +\n"))
	d.Clock = deps.FixedClock{Instant: at}
	return d, defPath, filepath.Join(defPath, "demo")
}

func listVerifyReports(t *testing.T, setDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(setDir, VerifyDirName))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestVerifyPublishesTheVerifiersReasoning drives one verification end to end:
// the verdict is still the enum pop gates on, and the reasoning behind it is a
// document under the set's own directory (ADR-0245).
func TestVerifyPublishesTheVerifiersReasoning(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := verifyReportFixture(t, at)

	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			return "VERDICT: FIXABLE\nFINDINGS: criterion 2 is unmet: the retry path is never taken\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if res.Verdict != VerdictFixable {
		t.Fatalf("verdict = %q, want FIXABLE", res.Verdict)
	}

	// The document is filed under the set's own directory in Task storage, one
	// per invocation, stamped with the instant.
	path := filepath.Join(setDir, VerifyDirName, "verify-20260816T120000Z.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verify report: %v", err)
	}
	for _, want := range []string{
		"# Verify report — demo",
		"- Verified: 2026-08-16T12:00:00Z",
		"- Commit range: aaa111^..HEAD",
		"- Work SHA: ",
		"criterion 2 is unmet: the retry path is never taken",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document missing %q:\n%s", want, body)
		}
	}
	// The verdict line is pop's channel, not the document's: it is lifted off
	// the front so a reader cannot mistake the report for the verdict.
	if strings.Contains(string(body), "VERDICT:") {
		t.Fatalf("document republished the verdict line:\n%s", body)
	}
	// The reports accumulate beside the set's tasks without making it malformed.
	if m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json")); !m.Valid {
		t.Fatalf("a verified set reads malformed: %v", m.Errors)
	}
}

// TestVerifyPublishesEveryVerdict: PASS is published like the rest. The green
// verdict is the one a reader can least reconstruct, and the old contract told
// the Verifier to record nothing for it.
func TestVerifyPublishesEveryVerdict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		raw   string
		want  Verdict
		prose string
	}{
		{name: "pass", raw: "VERDICT: PASS\nFINDINGS: read the diff of tasks/verify.go; criterion 1 is met by the new guard\n", want: VerdictPass, prose: "criterion 1 is met by the new guard"},
		{name: "fixable", raw: "VERDICT: FIXABLE\nFINDINGS: criterion 2 is unmet\n", want: VerdictFixable, prose: "criterion 2 is unmet"},
		{name: "needs-human", raw: "VERDICT: NEEDS-HUMAN\nFINDINGS: the spec contradicts the criteria\n", want: VerdictNeedsHuman, prose: "the spec contradicts the criteria"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, defPath, setDir := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
			res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
				Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
				Output:      &bytes.Buffer{},
				runVerifier: func(string) (string, error) { return tc.raw, nil },
			})
			if err != nil {
				t.Fatalf("verifyResolvedSet: %v", err)
			}
			if res.Verdict != tc.want {
				t.Fatalf("verdict = %q, want %q", res.Verdict, tc.want)
			}
			docs := listVerifyReports(t, setDir)
			if len(docs) != 1 {
				t.Fatalf("documents = %v, want exactly one", docs)
			}
			body, err := os.ReadFile(filepath.Join(setDir, VerifyDirName, docs[0]))
			if err != nil {
				t.Fatalf("read verify report: %v", err)
			}
			if !strings.Contains(string(body), tc.prose) {
				t.Fatalf("document missing the reasoning %q:\n%s", tc.prose, body)
			}
		})
	}
}

// TestVerifyPublishesAReportPerInvocation: a verify-remediate-verify lap leaves
// the whole trail, not one rewritten answer — even when both laps land inside
// the same second, as they do under a fixed clock.
func TestVerifyPublishesAReportPerInvocation(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	run := func(raw string) {
		t.Helper()
		if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output:      &bytes.Buffer{},
			runVerifier: func(string) (string, error) { return raw, nil },
		}); err != nil {
			t.Fatalf("verifyResolvedSet: %v", err)
		}
	}
	run("VERDICT: FIXABLE\nFINDINGS: the first lap found a missing guard\n")
	run("VERDICT: PASS\nFINDINGS: the guard is in place now\n")

	docs := listVerifyReports(t, setDir)
	if len(docs) != 2 {
		t.Fatalf("documents = %v, want one per invocation", docs)
	}
	first, err := os.ReadFile(filepath.Join(setDir, VerifyDirName, docs[0]))
	if err != nil {
		t.Fatalf("read first report: %v", err)
	}
	if !strings.Contains(string(first), "the first lap found a missing guard") {
		t.Fatalf("the earlier lap's reasoning was overwritten:\n%s", first)
	}
}

// TestVerifyReportKeepsAnUnparsedReply: a reply pop cannot understand still
// yields today's NEEDS-HUMAN verdict, and its raw text survives in the document
// as well as in the findings.
func TestVerifyReportKeepsAnUnparsedReply(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:      &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "VERDICT: MAYBE\nit is basically fine", nil },
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if res.Verdict != VerdictNeedsHuman || !strings.Contains(res.Findings, "could not be parsed") {
		t.Fatalf("result = %+v, want the unparsed NEEDS-HUMAN verdict", res)
	}
	docs := listVerifyReports(t, setDir)
	if len(docs) != 1 {
		t.Fatalf("documents = %v, want exactly one", docs)
	}
	body, err := os.ReadFile(filepath.Join(setDir, VerifyDirName, docs[0]))
	if err != nil {
		t.Fatalf("read verify report: %v", err)
	}
	// Nothing was lifted, because nothing parsed: the whole reply is the record.
	for _, want := range []string{"VERDICT: MAYBE", "it is basically fine"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document lost the raw reply %q:\n%s", want, body)
		}
	}
}

// TestVerifyReportOfASilentVerifier: a Verifier that said nothing still leaves a
// document, and the document says so — that silence is why the verdict is
// NEEDS-HUMAN.
func TestVerifyReportOfASilentVerifier(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:      &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "", nil },
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if res.Verdict != VerdictNeedsHuman {
		t.Fatalf("verdict = %q, want NEEDS-HUMAN", res.Verdict)
	}
	docs := listVerifyReports(t, setDir)
	if len(docs) != 1 {
		t.Fatalf("documents = %v, want exactly one", docs)
	}
	body, err := os.ReadFile(filepath.Join(setDir, VerifyDirName, docs[0]))
	if err != nil {
		t.Fatalf("read verify report: %v", err)
	}
	if !strings.Contains(string(body), "produced no output") {
		t.Fatalf("document does not record the silence:\n%s", body)
	}
}

// TestSplitVerifierReply pins the two replies that keep their whole text.
func TestSplitVerifierReply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "verdict lifted", raw: "VERDICT: PASS\nFINDINGS: every criterion is met", want: "FINDINGS: every criterion is met"},
		{name: "decorated verdict lifted", raw: "**VERDICT: FIXABLE**\nFINDINGS: one is unmet", want: "FINDINGS: one is unmet"},
		{name: "prose before the verdict is kept", raw: "I read the diff.\nVERDICT: PASS\nFINDINGS: met", want: "I read the diff.\nFINDINGS: met"},
		{name: "verdict alone is kept", raw: "VERDICT: PASS\n", want: "VERDICT: PASS"},
		{name: "unparsed reply is kept whole", raw: "looks fine to me", want: "looks fine to me"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := splitVerifierReply(tt.raw); got != tt.want {
				t.Fatalf("body = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestVerifyCachedVerdictPublishesNothing: the reports count invocations, the
// cache counts work SHAs. A verdict read back from the cache invokes no agent
// and so has no new reasoning to publish.
func TestVerifyCachedVerdictPublishesNothing(t *testing.T) {
	t.Parallel()
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "aaa111\n", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})

	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { t.Fatal("a cached verdict must not invoke the Verifier"); return "", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" {
		t.Fatalf("status = %q, verdict = %+v, want DONE / PASS from the cache", status, verdict)
	}
	if docs := listVerifyReports(t, m.Dir); len(docs) != 0 {
		t.Fatalf("documents = %v, want none for a cached verdict", docs)
	}
}

// TestVerifyReportOutlivesInvalidation: invalidation deletes the verdict cache —
// it fires the moment findings become actionable — and the report is what is
// left to answer why the set was judged as it was.
func TestVerifyReportOutlivesInvalidation(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output:      &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "VERDICT: FIXABLE\nFINDINGS: the guard is missing\n", nil },
	}); err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	docs := listVerifyReports(t, setDir)

	invalidateVerifyVerdicts(d, "/repo/.git", "demo")

	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "abc123abc123"); stored != nil {
		t.Fatalf("verdict cache survived invalidation: %+v", stored)
	}
	if got := listVerifyReports(t, setDir); len(got) != len(docs) || len(got) == 0 {
		t.Fatalf("documents = %v, want the %v that were there before invalidation", got, docs)
	}
	body, err := os.ReadFile(filepath.Join(setDir, VerifyDirName, docs[0]))
	if err != nil {
		t.Fatalf("read verify report: %v", err)
	}
	if !strings.Contains(string(body), "the guard is missing") {
		t.Fatalf("the surviving document lost its reasoning:\n%s", body)
	}
}

// TestVerifierPromptCarriesNoPreviousReport: the Verifier is never shown its own
// homework. That is a decision, not an omission — the verdict gates a drain, and
// a judge reading its last verdict anchors the enum (ADR-0245).
func TestVerifierPromptCarriesNoPreviousReport(t *testing.T) {
	t.Parallel()
	d, defPath, _ := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	verify := func(raw string, capture *string) {
		t.Helper()
		if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
			Output: &bytes.Buffer{},
			runVerifier: func(prompt string) (string, error) {
				if capture != nil {
					*capture = prompt
				}
				return raw, nil
			},
		}); err != nil {
			t.Fatalf("verifyResolvedSet: %v", err)
		}
	}
	verify("VERDICT: FIXABLE\nFINDINGS: the retry path is never taken\n", nil)

	second := ""
	verify("VERDICT: PASS\nFINDINGS: the retry path is covered now\n", &second)
	// The response contract names the report the reply will become, which is the
	// only mention the prompt is allowed: no previous document, none of its
	// header, and none of the reasoning of the lap before.
	for _, forbidden := range []string{
		"the retry path is never taken",
		"# Verify report — demo",
		"- Verified: ",
		VerifyDirName + string(filepath.Separator),
	} {
		if strings.Contains(second, forbidden) {
			t.Fatalf("the second Verifier prompt carries %q:\n%s", forbidden, second)
		}
	}
}

// TestAcceptedVerdictPublishesTheHumansRationale: a human overriding a non-PASS
// judgment leaves the same durable record a Verifier does (ADR-0245), so the set
// that most puzzles a later reader — green with damning findings behind it —
// answers the question itself.
func TestAcceptedVerdictPublishesTheHumansRationale(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	d, defPath, setDir := verifyReportFixture(t, at)
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "abc123abc123",
		Verdict: "NEEDS-HUMAN", Findings: "criterion 3 rests on a timeout the Verifier could not judge",
	})

	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true,
		AcceptNote:  "the timeout is intentional; it matches the upstream deadline",
		runVerifier: func(string) (string, error) { t.Fatal("accept must not invoke an agent"); return "", nil },
	}); err != nil {
		t.Fatalf("verifyResolvedSet accept: %v", err)
	}

	// Same directory, same stamp, same header of facts as a Verifier's report.
	path := filepath.Join(setDir, VerifyDirName, "verify-20260816T120000Z.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read accepted verdict report: %v", err)
	}
	for _, want := range []string{
		"# Verify report — demo",
		"- Verified: 2026-08-16T12:00:00Z",
		"- Commit range: aaa111^..HEAD",
		"- Work SHA: ",
		"- Accepted by: human",
		"NEEDS-HUMAN",
		"criterion 3 rests on a timeout the Verifier could not judge",
		"the timeout is intentional; it matches the upstream deadline",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document missing %q:\n%s", want, body)
		}
	}
	// A reader must be able to tell this apart from a Verifier's report without
	// reading the prose.
	if strings.Contains(string(body), "- Verifier:") {
		t.Fatalf("the human's report credits a Verifier:\n%s", body)
	}

	// The latest-report readers take it like any other document.
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
	p, ok := verifyReport.latestPointer(d, m)
	if !ok || p.Path != path {
		t.Fatalf("latest pointer = %+v (ok=%v), want the accepted verdict report at %s", p, ok, path)
	}
	if p.CommitRange != "aaa111^..HEAD" || p.WorkSHA == "" {
		t.Fatalf("pointer lost the header facts: %+v", p)
	}
}

// TestAcceptedVerdictReportWithNothingToOverride: an Accept recorded ahead of any
// judgment still leaves a document, and it says plainly that there was no verdict
// to override rather than implying one.
func TestAcceptedVerdictReportWithNothingToOverride(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := verifyReportFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Accept: true, AcceptNote: "reviewed by hand",
	}); err != nil {
		t.Fatalf("verifyResolvedSet accept: %v", err)
	}
	docs := listVerifyReports(t, setDir)
	if len(docs) != 1 {
		t.Fatalf("documents = %v, want exactly one", docs)
	}
	body, err := os.ReadFile(filepath.Join(setDir, VerifyDirName, docs[0]))
	if err != nil {
		t.Fatalf("read accepted verdict report: %v", err)
	}
	for _, want := range []string{"- Accepted by: human", "None recorded at this work SHA", "reviewed by hand"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("document missing %q:\n%s", want, body)
		}
	}
}
