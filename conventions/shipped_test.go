package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// TestEveryKindHasAShippedAnswer is the enum's whole justification: a kind pop
// admits is a kind pop can answer without anybody having written anything.
func TestEveryKindHasAShippedAnswer(t *testing.T) {
	for _, kind := range Kinds() {
		if strings.TrimSpace(Shipped(kind)) == "" {
			t.Errorf("kind %s has no shipped answer", kind)
		}
	}
}

// TestRenderShippedIsAnAnswer: `default <kind>` and a fallthrough hand over one
// body, and neither of them announces a method for the reader to work
// (ADR-0226 decision 1).
func TestRenderShippedIsAnAnswer(t *testing.T) {
	var out bytes.Buffer
	if err := RenderShipped(&out, KindCommits); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "SHIPPED CONVENTION commits") {
		t.Errorf("shipped output is not labelled:\n%s", got)
	}
	if !strings.Contains(got, shippedLayer(KindCommits).Body) {
		t.Errorf("`default` does not print the body the last rank carries:\n%s", got)
	}
	if strings.Contains(got, "METHOD") {
		t.Errorf("shipped output announces itself as a method:\n%s", got)
	}
}

// TestNoShippedAnswerSendsTheReaderBackToWriteALayer: an answer that closes by
// telling an agent to record a convention is the derivation step ADR-0226
// deleted, wearing an answer's label.
func TestNoShippedAnswerSendsTheReaderBackToWriteALayer(t *testing.T) {
	for _, kind := range Kinds() {
		body := Shipped(kind)
		for _, gone := range []string{"pop memory", "pop conventions set", "Write the result down"} {
			if strings.Contains(body, gone) {
				t.Errorf("the %s shipped answer still instructs a write-back (%q)", kind, gone)
			}
		}
	}
}

// TestCommitsShippedAnswerSamplesTheLog pins the prose that was triplicated
// across skills until ADR-0211 — the guard above all, which is load-bearing in
// any repository pop has drained.
func TestCommitsShippedAnswerSamplesTheLog(t *testing.T) {
	answer := Shipped(KindCommits)
	for what, want := range map[string]string{
		"the five-commit sample":        "last five commits",
		"the discard-pop-commits guard": "Discard pop-generated commits",
		"pop's own subject shape":       "tasks(...)",
		"the walk-further-back rule":    "Walk further back",
		"the no-convention result":      "No discernible convention is a real result",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("commits shipped answer does not carry %s (%q):\n%s", what, want, answer)
		}
	}
	// Subject grammar and body style are one kind, one document and one sample.
	// An answer that mentions only subjects invites a second, half-read
	// convention for bodies.
	for _, want := range []string{"subject grammar", "body style"} {
		if !strings.Contains(answer, want) {
			t.Errorf("commits shipped answer does not say it covers %q:\n%s", want, answer)
		}
	}
}

// TestRefineIsAKindWithAnAnswer: refine is configurable only through this
// stack, so the kind's presence in the enum is what makes a repository able to
// state its own standard at all (ADR-0214).
func TestRefineIsAKindWithAnAnswer(t *testing.T) {
	var found bool
	for _, kind := range Kinds() {
		found = found || kind == KindRefine
	}
	if !found {
		t.Fatalf("refine is not a Convention kind; Kinds() = %v", Kinds())
	}
	if strings.TrimSpace(Shipped(KindRefine)) == "" {
		t.Error("the refine kind has no shipped standard to hold a changeset against")
	}
}

// TestRefineShippedAnswerIsTheSmellBaseline: the baseline is the standards
// content now, not a floor beneath a derivation method, and the two rules that
// keep it from overriding a repository travel with it (ADR-0226 decision 2).
func TestRefineShippedAnswerIsTheSmellBaseline(t *testing.T) {
	answer := Shipped(KindRefine)
	for what, want := range map[string]string{
		"the smell baseline's source":    "Fowler",
		"a named smell":                  "Feature Envy",
		"the repository-overrides rule":  "The repository overrides.",
		"the judgement-call rule":        "Always a judgement call.",
		"the repository's own documents": "AGENTS.md",
		"its architectural decisions":    "docs/adr/",
		"its linter configuration":       ".golangci.yml",
		"its formatter and build":        "pre-commit",
		"the idiom of the code itself":   "idiom",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("refine shipped answer does not carry %s (%q):\n%s", what, want, answer)
		}
	}
	// All twelve smells are the standard; a baseline missing one holds a
	// changeset against less than it says it does.
	for _, smell := range []string{
		"Mysterious Name", "Duplicated Code", "Feature Envy", "Data Clumps",
		"Primitive Obsession", "Repeated Switches", "Shotgun Surgery",
		"Divergent Change", "Speculative Generality", "Message Chains",
		"Middle Man", "Refused Bequest",
	} {
		if !strings.Contains(answer, smell) {
			t.Errorf("refine shipped answer is missing the smell %q", smell)
		}
	}
}

// TestIssueTrackerShippedAnswerIsPopsTrackerDoc: the kind resolves to the
// document itself rather than to instructions for finding it, and it is the
// same bytes integration publishes as an asset.
func TestIssueTrackerShippedAnswerIsPopsTrackerDoc(t *testing.T) {
	answer := Shipped(KindIssueTracker)
	for _, want := range []string{"pop Work store", "pop tasks", "docs/agents/issue-tracker.md"} {
		if !strings.Contains(answer, want) {
			t.Errorf("issue-tracker shipped answer does not read as pop's tracker doc (%q missing)", want)
		}
	}
	if strings.Contains(answer, "pop integrate <agent>") {
		t.Errorf("issue-tracker shipped answer still tells the reader to refresh integration:\n%s", answer)
	}
}

// TestVerificationShippedAnswerIsHowWorkIsChecked: the kind's whole point is to
// hold a fact about a repository's toolchain that every pop-spawned agent used
// to rediscover — so its shipped answer has to say how to find the invocation,
// which gate is which, and what counts as having run one (ADR-0227 decision 4).
func TestVerificationShippedAnswerIsHowWorkIsChecked(t *testing.T) {
	answer := Shipped(KindVerification)
	for what, want := range map[string]string{
		"where the invocation is written down": "AGENTS.md",
		"the repository's task runner":         "Makefile",
		"the scoped gate":                      "Scoped",
		"the whole-tree gate":                  "Whole-tree",
		"what evidence is":                     "Evidence is the output of a command you ran",
		"reading the diff":                     "diff",
		"an unrunnable gate":                   "unrunnable gate is a finding",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("verification shipped answer does not carry %s (%q):\n%s", what, want, answer)
		}
	}
}

// TestVerificationShippedAnswerCarriesNoneOfPopsMachinery: the answer is a
// standard a team could replace wholesale, so pop's reply format and the scope
// rule naming which tasks are under judgement stay in the frame pop owns
// (ADR-0227 decisions 2 and 4).
func TestVerificationShippedAnswerCarriesNoneOfPopsMachinery(t *testing.T) {
	answer := Shipped(KindVerification)
	for what, gone := range map[string]string{
		"the verdict vocabulary":     "VERDICT",
		"a verdict value":            "NEEDS-HUMAN",
		"the summary line":           "SUMMARY:",
		"the commit-subject line":    "COMMIT-SUBJECT",
		"the findings line":          "FINDINGS",
		"the done-AFK scope rule":    "done AFK",
		"the authoritative criteria": "acceptance criteria",
	} {
		if strings.Contains(answer, gone) {
			t.Errorf("verification shipped answer carries %s (%q), which is pop's frame:\n%s", what, gone, answer)
		}
	}
}

// TestRefineShippedAnswerReviewsOnTwoAxes: pop's own answer weighs how the
// code is written and whether it does what was asked (ADR-0227 consequence). The
// second axis knowingly overlaps the Verifier's, which is tolerable only because
// a refine pass reaches no verdict — so the answer says the overlap is a second
// opinion rather than claiming the last word.
func TestRefineShippedAnswerReviewsOnTwoAxes(t *testing.T) {
	answer := Shipped(KindRefine)
	for what, want := range map[string]string{
		"the standards axis":          "Axis 1 — Standards: how the code is written",
		"the spec axis":               "Axis 2 — Spec: whether the code does what was asked",
		"work the request never got":  "Missing",
		"work nobody asked for":       "Extra",
		"work that answers otherwise": "Different",
		"the diff over the report":    "Read the diff for this axis",
		"the overlap it admits":       "second opinion",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("refine shipped answer does not carry %s (%q):\n%s", what, want, answer)
		}
	}
}

// TestRefineShippedAnswerInstructsNoWrites: the Refiner runs under a
// read-only posture (ADR-0221), so an answer that told it to record or fix
// anything would be an instruction it cannot obey.
func TestRefineShippedAnswerInstructsNoWrites(t *testing.T) {
	answer := Shipped(KindRefine)
	if !strings.Contains(answer, "Change no files") {
		t.Errorf("refine shipped answer does not hold the reader to reading only:\n%s", answer)
	}
	for _, gone := range []string{"write the result", "Write the result", "commit the", "apply the fix"} {
		if strings.Contains(answer, gone) {
			t.Errorf("refine shipped answer instructs a write (%q):\n%s", gone, answer)
		}
	}
}
