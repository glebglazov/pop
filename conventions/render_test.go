package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// The four written paths a test authors, in rank order. They are spelled out
// rather than derived so a test reads as the stack it is asserting about.
const (
	projectDoc = "/h/.agents/docs/projects/github.com-tripledot-pop/commits.md"
	globalDoc  = "/h/.agents/docs/commits.md"
	repoDoc    = "/r/docs/agents/commits.md"
	overlayDoc = "/h/.agents/docs/commits.overlay.md"
)

// TestUnwrittenKindResolvesToTheShippedRank pins the last rank: a kind nobody
// has written a document for still resolves, to pop's own answer, rendered as an
// answer like any other rank's (ADR-0226 decision 1).
func TestUnwrittenKindResolvesToTheShippedRank(t *testing.T) {
	stack := Stack{Kind: KindCommits, Layers: []Layer{
		{Origin: OriginProject, Path: projectDoc},
		{Origin: OriginGlobal, Path: globalDoc},
		{Origin: OriginRepository, Path: repoDoc},
		{Origin: OriginOverlay, Path: overlayDoc},
	}}

	if got := stack.Answer(); got.Origin != OriginShipped || got.Body == "" {
		t.Fatalf("Answer() = %s / %q, want the shipped rank", got.Origin, got.Body)
	}
	// Nothing is written anywhere, so nothing is losing to anything.
	if stack.Contested() {
		t.Error("a kind resolving to pop's own answer is reported as contested")
	}

	var out bytes.Buffer
	if err := RenderStack(&out, stack); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"ANSWER: SHIPPED",
		"pop's own, displaced by any above",
		Shipped(KindCommits),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shipped rank rendering missing %q:\n%s", want, got)
		}
	}
	// The banner ADR-0226 deleted taught readers to stop at the first line; no
	// surface may reintroduce it.
	if strings.Contains(got, "METHOD") {
		t.Errorf("the shipped rank is rendered under a method banner:\n%s", got)
	}
	// `default <kind>` and a fallthrough hand over the same body.
	var direct bytes.Buffer
	if err := RenderShipped(&direct, KindCommits); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, strings.TrimPrefix(direct.String(), "SHIPPED CONVENTION commits\n\n")) {
		t.Errorf("the resolved answer is not the body `default` prints:\n%s\n----\n%s", got, direct.String())
	}
}

// A written rank of any kind outranks pop's own answer, which is the whole
// reason the shipped rank sits beneath all three.
func TestAnyWrittenRankOutranksTheShippedRank(t *testing.T) {
	for _, origin := range writtenRanks {
		s := Stack{Kind: KindCommits, Layers: []Layer{
			{Origin: origin, Path: "/p", Present: true, Body: "WRITTEN"},
		}}
		if got := s.Answer(); got.Origin != origin {
			t.Errorf("%s answers with %s", origin, got.Origin)
		}
		if prose := StackProse(s); strings.Contains(prose, "SHIPPED") {
			t.Errorf("%s answered and pop's own answer still printed:\n%s", origin, prose)
		}
	}
}

// TestResolutionPicksOneAnswer walks the rank order that decides everything
// else: the human's document for this project over their document for every
// repository, either over the team's, and the overlay riding on whichever won
// rather than competing with it.
func TestResolutionPicksOneAnswer(t *testing.T) {
	project := Layer{Origin: OriginProject, Path: projectDoc, Present: true, Body: "MINE-HERE"}
	global := Layer{Origin: OriginGlobal, Path: globalDoc, Present: true, Body: "MINE-EVERYWHERE"}
	repo := Layer{Origin: OriginRepository, Path: repoDoc, Present: true, Body: "TEAM"}
	overlay := Layer{Origin: OriginOverlay, Path: overlayDoc, Present: true, Body: "EXTRA"}

	tests := []struct {
		name string
		// answer is the body expected in force; empty means the kind falls
		// through to the shipped rank.
		layers     []Layer
		answer     string
		hasOverlay bool
		contested  bool
	}{
		{name: "nothing anywhere falls through to the shipped rank"},
		{name: "the team's document alone", layers: []Layer{repo}, answer: "TEAM"},
		{name: "the human's global document stands the team's down",
			layers: []Layer{global, repo}, answer: "MINE-EVERYWHERE", contested: true},
		{name: "the human's project document stands their global one down",
			layers: []Layer{project, global}, answer: "MINE-HERE", contested: true},
		{name: "the human's project document stands both others down",
			layers: []Layer{project, global, repo}, answer: "MINE-HERE", contested: true},
		{name: "the overlay rides on the answer",
			layers: []Layer{repo, overlay}, answer: "TEAM", hasOverlay: true},
		{name: "the overlay alone rides on the shipped rank",
			layers: []Layer{overlay}, hasOverlay: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Stack{Kind: KindCommits, Layers: tt.layers}
			answer := s.Answer()
			wantBody, wantOrigin := tt.answer, Origin("")
			if wantBody == "" {
				wantBody, wantOrigin = shippedLayer(KindCommits).Body, OriginShipped
			}
			if answer.Body != wantBody {
				t.Errorf("Answer() = %q, want %q", answer.Body, wantBody)
			}
			if wantOrigin != "" && answer.Origin != wantOrigin {
				t.Errorf("Answer() origin = %s, want %s", answer.Origin, wantOrigin)
			}
			if _, ok := s.Overlay(); ok != tt.hasOverlay {
				t.Errorf("Overlay() present = %v, want %v", ok, tt.hasOverlay)
			}
			if got := s.Contested(); got != tt.contested {
				t.Errorf("Contested() = %v, want %v", got, tt.contested)
			}

			// The rendered surfaces carry the answer and the overlay, and never a
			// rank the answer stood down.
			prose := StackProse(s)
			for _, l := range tt.layers {
				shown := strings.Contains(prose, l.Body)
				inForce := (tt.answer != "" && l.Body == tt.answer) || l.Origin == OriginOverlay
				if shown != inForce {
					t.Errorf("layer %s shown = %v, want %v:\n%s", l.Origin, shown, inForce, prose)
				}
			}
		})
	}
}

// TestProvenanceDisclosesTheAnswerAndTheOverlay pins the one line every skill
// surfaces verbatim, which is the whole reason pop emits it rather than letting
// each skill phrase it. It names the answering rank and its path, the overlay
// clause where one applies, and nothing else — there is no rank left whose
// origin pop would have to account for (ADR-0226 decision 5).
func TestProvenanceDisclosesTheAnswerAndTheOverlay(t *testing.T) {
	tests := []struct {
		name   string
		layers []Layer
		want   []string
		absent []string
	}{
		{
			name: "the human's project document answers",
			layers: []Layer{
				{Origin: OriginProject, Path: projectDoc, Present: true, Body: "b"},
			},
			want: []string{"resolved to user project", projectDoc},
		},
		{
			name: "the project document stands the others down",
			layers: []Layer{
				{Origin: OriginProject, Path: projectDoc, Present: true, Body: "b"},
				{Origin: OriginGlobal, Path: globalDoc, Present: true, Body: "b"},
				{Origin: OriginRepository, Path: repoDoc, Present: true, Body: "b"},
			},
			want: []string{"resolved to user project", projectDoc},
			// A rank nobody is being handed is not disclosed as a source.
			absent: []string{globalDoc, repoDoc},
		},
		{
			name: "the overlay is named as appended, not as the answer",
			layers: []Layer{
				{Origin: OriginRepository, Path: repoDoc, Present: true, Body: "b"},
				{Origin: OriginOverlay, Path: overlayDoc, Present: true, Body: "b"},
			},
			want: []string{"resolved to repository", "appended", overlayDoc},
		},
		{
			name: "an unwritten kind names pop's own answer and claims nothing more",
			want: []string{"resolved to shipped", "shipped/commits.md"},
			// The clause that told a reader what is in force is a method, not
			// rules, went with the recipe rank; the clause that quoted pop's own
			// derivation went with the memory rank (ADR-0226).
			absent: []string{"method", "not rules to follow", "derived"},
		},
		{
			name: "an overlay rides on the shipped rank when nothing is written",
			layers: []Layer{
				{Origin: OriginOverlay, Path: overlayDoc, Present: true, Body: "b"},
			},
			want: []string{"shipped", "appended", overlayDoc},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := Stack{Kind: KindCommits, Layers: tt.layers}.Provenance()
			if strings.Contains(line, "\n") {
				t.Fatalf("provenance is not one line: %q", line)
			}
			for _, want := range tt.want {
				if !strings.Contains(line, want) {
					t.Errorf("provenance %q missing %q", line, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(line, absent) {
					t.Errorf("provenance %q should not mention %q", line, absent)
				}
			}
		})
	}
}

// TestStackPreviewShowsTheAnswerAndTheOverlay pins what a surface showing a
// convention beside values of another sort gets: the same answer, overlay and
// provenance `get` prints, closed by both documents the human could write. A
// rank the answer stood down is not shown — what is in force is the question the
// pane exists to answer.
func TestStackPreviewShowsTheAnswerAndTheOverlay(t *testing.T) {
	stack := Stack{Kind: KindCommits, Layers: []Layer{
		{Origin: OriginProject, Path: projectDoc},
		{Origin: OriginGlobal, Path: globalDoc, Present: true, Body: "Imperative subjects."},
		{Origin: OriginRepository, Path: repoDoc, Present: true, Body: "Conventional commits."},
		{Origin: OriginOverlay, Path: overlayDoc},
	}}

	got := StackPreview(stack)

	for _, want := range []string{
		"ANSWER: USER GLOBAL",
		"yours, every repository",
		"Imperative subjects.",
		"Provenance:",
		// Both documents that are the human's to write, each named whether or
		// not it holds anything: the pane writes neither, so the paths and the
		// verb are what a reader leaves with.
		"not written yet",
		projectDoc,
		overlayDoc,
		"pop conventions set commits --project",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"Conventional commits.", repoDoc} {
		if strings.Contains(got, absent) {
			t.Errorf("a layer the answer stood down is shown as in force (%q):\n%s", absent, got)
		}
	}

	// The pane and `pop conventions get` render one thing, so they cannot
	// describe the same convention differently.
	var printed bytes.Buffer
	if err := RenderStack(&printed, stack); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(printed.String(), stack.inForceProse()) {
		t.Errorf("get does not print what the preview shows:\n%s", printed.String())
	}
	if !strings.Contains(got, stack.inForceProse()) {
		t.Errorf("the preview does not show what get prints:\n%s", got)
	}
}

// The pane shows pop's own answer when that is what answers: the ADR's rule is
// that `get` and the pane cannot describe one convention differently, and the
// rank in force is as much the pane's business as any other.
func TestStackPreviewOfAnUnwrittenKindShowsTheShippedRank(t *testing.T) {
	stack := Stack{Kind: KindIssueTracker, Layers: []Layer{
		{Origin: OriginProject, Path: "/h/.agents/docs/projects/github.com-tripledot-pop/issue-tracker.md"},
		{Origin: OriginGlobal, Path: "/h/.agents/docs/issue-tracker.md"},
		{Origin: OriginRepository, Path: "/r/docs/agents/issue-tracker.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/issue-tracker.overlay.md"},
	}}

	got := StackPreview(stack)

	for _, want := range []string{"ANSWER: SHIPPED", Shipped(KindIssueTracker)} {
		if !strings.Contains(got, want) {
			t.Errorf("preview of an unwritten kind is missing %q:\n%s", want, got)
		}
	}
	// Both writable documents are still named, an unwritten kind being exactly
	// the case where a reader is about to write one.
	for _, want := range []string{
		"/h/.agents/docs/projects/github.com-tripledot-pop/issue-tracker.md",
		"/h/.agents/docs/issue-tracker.overlay.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview does not name %q, a document the reader may write:\n%s", want, got)
		}
	}
}

// Every rank's scope is the phrase printed beside its origin, so a labelled
// block reads as something other than a file path. A rank with none would
// render an empty parenthesis, and "defaults" is retired as a rank word
// because it named two ranks at opposite ends of the stack (ADR-0226).
func TestEveryRankStatesItsAuthorAndScope(t *testing.T) {
	for _, origin := range append(append([]Origin{}, writtenRanks...), OriginShipped, OriginOverlay) {
		scope := origin.Scope()
		if scope == "" {
			t.Errorf("rank %q states no scope", origin)
		}
		for _, word := range []string{"defaults", "memory"} {
			if strings.Contains(string(origin), word) || strings.Contains(scope, word) {
				t.Errorf("rank %q (%q) still uses the retired word %q", origin, scope, word)
			}
		}
	}
}
