package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// verifyProse is the one string no surface may print: the report's body. Every
// surface carries a pointer to the document and nothing else (ADR-0245).
const verifyProse = "SECRET-VERIFY-PROSE"

// seedVerifyReport files one Verify report for the fixture set, as the Verifier
// would have written it, and returns its path.
func seedVerifyReport(t *testing.T, d *Deps, m *Manifest, workSHA string) string {
	t.Helper()
	at := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	body := verifyReport.renderDocument(at, "demo", workSHA, "aaa111^..HEAD", "codex", "## Criterion 1\n\n"+verifyProse)
	path, err := verifyReport.writeDocument(d, m.Dir, at, body)
	if err != nil {
		t.Fatalf("write verify report: %v", err)
	}
	return path
}

// TestVerifyPointerReachesTheHuman drives the four human-facing surfaces the
// report has to arrive on: the gate preamble, the gate's paging entry, the
// detail view, and the artifact list (ADR-0245).
func TestVerifyPointerReachesTheHuman(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	path := seedVerifyReport(t, d, m, "abc123abc123")

	gate, _ := hitlGateOutput(t, d, m, "0\n")

	var detail bytes.Buffer
	RenderTaskSetDetail(d, nil, &detail, "demo", nil, m)

	for _, s := range []struct {
		surface string
		text    string
		wants   []string
	}{
		{"HITL gate", gate, []string{path, "abc123a", "aaa111^..HEAD", "Read the verify report"}},
		{"detail view", detail.String(), []string{path, "abc123a", "aaa111^..HEAD"}},
	} {
		for _, want := range s.wants {
			if !strings.Contains(s.text, want) {
				t.Fatalf("%s missing %q:\n%s", s.surface, want, s.text)
			}
		}
		if strings.Contains(s.text, verifyProse) {
			t.Fatalf("%s inlined the verify report:\n%s", s.surface, s.text)
		}
	}

	// Choosing the paging entry reads the document and changes nothing else; the
	// fixture's runner cannot page, so the entry falls back to printing it.
	if _, action := hitlGateOutput(t, d, m, "5\n"); action != hitlGateReadVerify {
		t.Fatalf("menu entry 5 = %v, want hitlGateReadVerify", action)
	}
	var paged bytes.Buffer
	p, ok := latestVerifyPointer(d, m)
	if !ok {
		t.Fatalf("latestVerifyPointer: not found")
	}
	pageReportDocument(d, strings.NewReader(""), "/rt", &paged, p)
	if !strings.Contains(paged.String(), verifyProse) {
		t.Fatalf("paging entry did not show the document:\n%s", paged.String())
	}
}

// TestVerifyReportOutranksRefineInTheArtifactList pins the tier order: a verdict
// outranks a polish note, whichever was written first (ADR-0245).
func TestVerifyReportOutranksRefineInTheArtifactList(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	refinePath := seedRefineReport(t, d, m)
	verifyPath := seedVerifyReport(t, d, m, "abc123abc123")

	artifacts, err := Artifacts(d, m.Dir)
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	if len(artifacts) < 2 {
		t.Fatalf("artifacts = %v, want the two reports", artifacts)
	}
	if artifacts[0].Type != ArtifactTypeVerify || artifacts[0].Path != verifyPath {
		t.Fatalf("head artifact = %+v, want the verify report at %s", artifacts[0], verifyPath)
	}
	if artifacts[1].Type != ArtifactTypeRefine || artifacts[1].Path != refinePath {
		t.Fatalf("second artifact = %+v, want the refine report at %s", artifacts[1], refinePath)
	}
}

// TestVerifyPointerStaysOutOfAgentPrompts pins the asymmetry ADR-0245 draws: an
// agent sent to fix something already has the findings in its task body, and a
// second copy invites it to treat the report as the spec.
func TestVerifyPointerStaysOutOfAgentPrompts(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	path := seedVerifyReport(t, d, m, "abc123abc123")
	hitl := m.Tasks[1]

	work := workDiffView{Range: "aaa111^..HEAD", Stat: " tasks/verify.go | 3 +\n"}
	for _, s := range []struct {
		surface string
		text    string
	}{
		{"HITL assistance", BuildHITLAssistancePrompt(d, "demo", m, hitl, "/rt")},
		{"failed assistance", BuildFailedAssistancePrompt(d, "demo", m, hitl, "/rt")},
		{"verify-failed assistance", BuildVerifyFailedAssistancePrompt(d, "demo", m, "abc123abc123", "findings", "/rt")},
		{"interrupt assistance", BuildInterruptAssistancePrompt(d, "demo", m, hitl, "/rt")},
		{"assist", BuildAssistPrompt(d, nil, "demo", m, StatusAwaitingApproval, "/rt", "")},
		{"verifier", buildVerifierPrompt(d, m, "abc123abc123", work, "", "")},
	} {
		if strings.Contains(s.text, path) || strings.Contains(s.text, VerifyDirName+"/verify-") {
			t.Fatalf("%s prompt gained the verify pointer:\n%s", s.surface, s.text)
		}
	}
}

// TestNeverVerifiedSetShowsNoPointer pins the empty case on every surface: a set
// with no report renders nothing at all rather than an empty block.
func TestNeverVerifiedSetShowsNoPointer(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)

	gate, _ := hitlGateOutput(t, d, m, "0\n")
	var detail bytes.Buffer
	RenderTaskSetDetail(d, nil, &detail, "demo", nil, m)

	for _, s := range []struct {
		surface string
		text    string
	}{{"HITL gate", gate}, {"detail view", detail.String()}} {
		for _, unwanted := range []string{"Read the verify report", "🔍"} {
			if strings.Contains(s.text, unwanted) {
				t.Fatalf("%s showed %q for a never-verified set:\n%s", s.surface, unwanted, s.text)
			}
		}
	}
	artifacts, err := Artifacts(d, m.Dir)
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	for _, a := range artifacts {
		if a.Type == ArtifactTypeVerify {
			t.Fatalf("artifact list carried a verify report for a never-verified set: %+v", a)
		}
	}
}

// TestVerifyReportStalenessIsNotASecondVerdict pins the line ADR-0245 draws
// against the Verified-at badge: a report written against a commit the checkout
// has moved past is still pointed at, in the same words, and says nothing about
// whether the set is verified. Staleness stays the badge's question.
func TestVerifyReportStalenessIsNotASecondVerdict(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)

	var before bytes.Buffer
	RenderTaskSetDetail(d, nil, &before, "demo", nil, m)

	// The fixture's checkout is at sha1; the report was written against a commit
	// that is not it.
	seedVerifyReport(t, d, m, "def456def456")
	var after bytes.Buffer
	RenderTaskSetDetail(d, nil, &after, "demo", nil, m)

	if !strings.Contains(after.String(), "def456d") {
		t.Fatalf("detail view dropped the pointer to a stale report:\n%s", after.String())
	}
	// The pointer's own words, with the document path taken out so the fixture's
	// temp directory cannot spell a verdict the surface did not.
	p, _ := latestVerifyPointer(d, m)
	words := strings.ToLower(strings.ReplaceAll(p.Summary(), p.Path, ""))
	for _, unwanted := range []string{"out of date", "stale", "unverified", "re-verify"} {
		if strings.Contains(words, unwanted) {
			t.Fatalf("the pointer answered staleness with %q: %s", unwanted, words)
		}
	}
	// The status line the detail view opens with is what the badge feeds; the
	// report must not have moved it.
	if headline(before.String()) != headline(after.String()) {
		t.Fatalf("the report changed the set's status line:\n%s\n%s", headline(before.String()), headline(after.String()))
	}

	// The badge derives from the row alone, so a set with a report behind it
	// reads exactly as one without.
	row := Row{ID: "demo", Status: StatusDone, VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abc123a"}
	if badge := DeriveVerifiedAtBadge(row); badge.State != VerifiedAtAtHead || badge.SHA != "abc123a" {
		t.Fatalf("verified-at badge = %+v, want at-head abc123a", badge)
	}
}

func headline(detail string) string {
	return strings.SplitN(strings.TrimSpace(detail), "\n", 2)[0]
}
