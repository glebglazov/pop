package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// reviewProse is the one string no surface may print: the document's body. Every
// surface carries a pointer to it and nothing else (ADR-0214).
const reviewProse = "SECRET-REVIEW-PROSE"

// seedReviewArtifact files one Review artifact for the fixture set, as the
// Reviewer would have written it, and returns its path.
func seedReviewArtifact(t *testing.T, d *Deps, m *Manifest) string {
	t.Helper()
	at := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	body := renderReviewDocument(at, "demo", "abc123abc123", "aaa111^..HEAD", "claude", "## Naming\n\n"+reviewProse)
	path, err := writeReviewDocument(d, m.Dir, at, body)
	if err != nil {
		t.Fatalf("writeReviewDocument: %v", err)
	}
	return path
}

func hitlFixture(t *testing.T) (*Deps, *Manifest) {
	t.Helper()
	return setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Sign off", Type: "HITL", Status: "open"},
	}, nil)
}

func hitlGateOutput(t *testing.T, d *Deps, m *Manifest, input string) (string, hitlGateAction) {
	t.Helper()
	var out strings.Builder
	in := strings.NewReader(input)
	review, hasReview := latestReviewPointer(d, m)
	action, err := promptHITLGateAction(&out, in, d, nil, "/rt", newPromptReader(in), "demo", m, &m.Tasks[1],
		"## Acceptance criteria\n\n- [ ] ok\n", nil, false, review, hasReview)
	if err != nil {
		t.Fatalf("promptHITLGateAction: %v", err)
	}
	return out.String(), action
}

// TestReviewPointerReachesEverySurface drives the three places a human meets a
// Code review — the HITL sign-off gate, the Task set detail view and the Assist
// prompt — and pins that each names the document and the commit it was written
// against, and none of them inlines a word of it.
func TestReviewPointerReachesEverySurface(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	path := seedReviewArtifact(t, d, m)

	gate, _ := hitlGateOutput(t, d, m, "0\n")

	var detail bytes.Buffer
	RenderTaskSetDetail(d, &detail, "demo", nil, m)

	assist := BuildAssistPrompt(d, "demo", m, StatusAwaitingApproval, "/rt", "")

	for _, s := range []struct {
		surface string
		text    string
		wants   []string
	}{
		{"HITL gate", gate, []string{path, "abc123a", "aaa111^..HEAD", "Read the code review"}},
		{"detail view", detail.String(), []string{"CODE REVIEW", path, "abc123a"}},
		{"assist prompt", assist, []string{path, "abc123a", "read the file yourself"}},
	} {
		for _, want := range s.wants {
			if !strings.Contains(s.text, want) {
				t.Fatalf("%s missing %q:\n%s", s.surface, want, s.text)
			}
		}
		if strings.Contains(s.text, reviewProse) {
			t.Fatalf("%s inlined the review document:\n%s", s.surface, s.text)
		}
	}
}

// TestUnreviewedSetShowsNothingExtra is the other half of the rule: until a set
// has a Review artifact, no surface says the word.
func TestUnreviewedSetShowsNothingExtra(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)

	gate, _ := hitlGateOutput(t, d, m, "0\n")

	var detail bytes.Buffer
	RenderTaskSetDetail(d, &detail, "demo", nil, m)

	assist := BuildAssistPrompt(d, "demo", m, StatusAwaitingApproval, "/rt", "")

	for _, s := range []struct {
		surface string
		text    string
	}{
		{"HITL gate", gate},
		{"detail view", detail.String()},
		{"assist prompt", assist},
	} {
		if strings.Contains(strings.ToLower(s.text), "code review") {
			t.Fatalf("%s mentions a review the set never had:\n%s", s.surface, s.text)
		}
	}
}

// TestHITLGateReadReviewEntryPagesAndSpawnsNoAgent pins the gate's review entry:
// it is the next free key, it resolves to the read action, and taking it runs
// the human's pager over the document — no agent, and no change to the set.
func TestHITLGateReadReviewEntryPagesAndSpawnsNoAgent(t *testing.T) {
	d, m := hitlFixture(t)
	path := seedReviewArtifact(t, d, m)

	// Re-verify is not offered here, so the review takes key 5.
	_, action := hitlGateOutput(t, d, m, "5\n")
	if action != hitlGateReadReview {
		t.Fatalf("expected the review entry at key 5, got action %d", action)
	}

	runner := &fakeAttendedRunner{}
	d.Runner = runner
	t.Setenv("PAGER", "mypager -X")
	review, ok := latestReviewPointer(d, m)
	if !ok {
		t.Fatal("expected a review pointer")
	}
	var out bytes.Buffer
	pageReviewDocument(d, strings.NewReader(""), "/rt", &out, review)

	if runner.attendedCalls != 1 {
		t.Fatalf("expected exactly one attended launch (the pager), got %d", runner.attendedCalls)
	}
	if runner.name != "mypager" || strings.Join(runner.args, " ") != "-X "+path {
		t.Fatalf("expected the pager over the document, got %s %v", runner.name, runner.args)
	}
}

// TestReviewPointerReadsTheArtifactsOwnHeader pins where the commit facts come
// from: the header the artifact carries, not a side-car pop keeps beside it.
func TestReviewPointerReadsTheArtifactsOwnHeader(t *testing.T) {
	t.Parallel()
	d, m := hitlFixture(t)
	seedReviewArtifact(t, d, m)

	p, ok := latestReviewPointer(d, m)
	if !ok {
		t.Fatal("expected a review pointer")
	}
	if p.WorkSHA != ShortSHA("abc123abc123") || p.CommitRange != "aaa111^..HEAD" {
		t.Fatalf("pointer read the wrong header: %+v", p)
	}
	if got := p.CommitPhrase(); got != ShortSHA("abc123abc123")+" (aaa111^..HEAD)" {
		t.Fatalf("commit phrase: %q", got)
	}
}
