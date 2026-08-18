package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// TestUnwrittenKindResolvesToItsRecipe pins the last rank: a kind no document
// and no memory answers still resolves, to the method for deriving one, and the
// rendering says so twice — in the block label a reader scans, and in the
// banner the body opens with (ADR-0223 decision 5).
func TestUnwrittenKindResolvesToItsRecipe(t *testing.T) {
	stack := Stack{Kind: KindCommits, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/h/.agents/docs/commits.md"},
		{Origin: OriginRepository, Path: "/r/docs/agents/commits.md"},
		{Origin: OriginMemory, Path: "/d/pop/repos/pop-abc/conventions/commits.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md"},
	}}

	if got := stack.Answer(); got.Origin != OriginRecipe || got.Body == "" {
		t.Fatalf("Answer() = %s / %q, want the recipe rank", got.Origin, got.Body)
	}
	// Nothing is written anywhere, so nothing is losing to anything.
	if stack.Contested() {
		t.Error("a kind resolving to its recipe is reported as contested")
	}

	var out bytes.Buffer
	if err := RenderStack(&out, stack); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"METHOD: CONVENTION RECIPE",
		"METHOD, not a convention",
		Recipe(KindCommits),
		"the method for deriving one",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("recipe rank rendering missing %q:\n%s", want, got)
		}
	}
	// The recipe answers; it is never labelled as somebody's answer.
	if strings.Contains(got, "ANSWER:") {
		t.Errorf("the method is rendered as an answer:\n%s", got)
	}
	// `recipe <kind>` and a fallthrough hand over the same body.
	var direct bytes.Buffer
	if err := RenderRecipe(&direct, KindCommits); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, strings.TrimPrefix(direct.String(), "RECIPE commits\n\n")) {
		t.Errorf("the resolved recipe is not the body `recipe` prints:\n%s\n----\n%s", got, direct.String())
	}
}

// A written rank of any kind outranks the recipe, which is the whole reason the
// recipe sits beneath all three.
func TestAnyWrittenRankOutranksTheRecipe(t *testing.T) {
	for _, origin := range writtenRanks {
		s := Stack{Kind: KindCommits, Layers: []Layer{
			{Origin: origin, Path: "/p", Present: true, Body: "WRITTEN"},
		}}
		if got := s.Answer(); got.Origin != origin {
			t.Errorf("%s answers with %s", origin, got.Origin)
		}
		if prose := StackProse(s); strings.Contains(prose, "METHOD") {
			t.Errorf("%s answered and the recipe still printed:\n%s", origin, prose)
		}
	}
}

// TestResolutionPicksOneAnswer walks the rank order that decides everything
// else: the human's document over the team's, the team's over pop's memory, and
// the overlay riding on whichever won rather than competing with it.
func TestResolutionPicksOneAnswer(t *testing.T) {
	defaults := Layer{Origin: OriginUserDefaults, Path: "/h/.agents/docs/commits.md", Present: true, Body: "MINE"}
	repo := Layer{Origin: OriginRepository, Path: "/r/docs/agents/commits.md", Present: true, Body: "TEAM"}
	memory := Layer{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "POP"}
	overlay := Layer{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md", Present: true, Body: "EXTRA"}

	tests := []struct {
		name string
		// answer is the body expected in force; empty means the kind falls
		// through to its recipe.
		layers     []Layer
		answer     string
		hasOverlay bool
		contested  bool
	}{
		{name: "nothing anywhere falls through to the recipe"},
		{name: "memory alone", layers: []Layer{memory}, answer: "POP"},
		{name: "the team's document stands memory down",
			layers: []Layer{repo, memory}, answer: "TEAM", contested: true},
		{name: "the human's document stands the team's down",
			layers: []Layer{defaults, repo}, answer: "MINE", contested: true},
		{name: "the human's document stands both down",
			layers: []Layer{defaults, repo, memory}, answer: "MINE", contested: true},
		{name: "the overlay rides on the answer",
			layers: []Layer{repo, overlay}, answer: "TEAM", hasOverlay: true},
		{name: "the overlay alone rides on the recipe",
			layers: []Layer{overlay}, hasOverlay: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Stack{Kind: KindCommits, Layers: tt.layers}
			answer := s.Answer()
			wantBody, wantOrigin := tt.answer, Origin("")
			if wantBody == "" {
				wantBody, wantOrigin = recipeLayer(KindCommits).Body, OriginRecipe
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
// each skill phrase it.
func TestProvenanceDisclosesTheAnswerAndTheOverlay(t *testing.T) {
	tests := []struct {
		name   string
		layers []Layer
		want   []string
		absent []string
	}{
		{
			name: "memory answers with full frontmatter",
			layers: []Layer{
				{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "b",
					DerivedFrom: "a sample of 40 commits", DerivedAt: "2026-08-01"},
			},
			want: []string{"pop memory", "/d/conventions/commits.md", "a sample of 40 commits", "2026-08-01"},
		},
		{
			name: "the repository's document stands memory down",
			layers: []Layer{
				{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "b", DerivedFrom: "a sample of 40 commits"},
				{Origin: OriginRepository, Path: "/r/docs/agents/commits.md", Present: true, Body: "b"},
			},
			want: []string{"repository", "/r/docs/agents/commits.md"},
			// A memory nobody is being handed has no derivation worth quoting.
			absent: []string{"a sample of 40 commits", "/d/conventions/commits.md"},
		},
		{
			name: "answering memory without frontmatter still discloses itself",
			layers: []Layer{
				{Origin: OriginMemory, Path: "/d/conventions/commits.md", Present: true, Body: "b"},
			},
			want: []string{"pop memory", "records no derivation"},
		},
		{
			name: "the overlay is named as appended, not as the answer",
			layers: []Layer{
				{Origin: OriginRepository, Path: "/r/docs/agents/commits.md", Present: true, Body: "b"},
				{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md", Present: true, Body: "b"},
			},
			want:   []string{"resolved to repository", "appended", "/h/.agents/docs/commits.overlay.md"},
			absent: []string{"Pop memory"},
		},
		{
			name: "an unwritten kind discloses that it holds a method",
			want: []string{"resolved to convention recipe", "recipes/commits.md", "not rules to follow"},
		},
		{
			name: "an overlay rides on the recipe when nothing is written",
			layers: []Layer{
				{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md", Present: true, Body: "b"},
			},
			want: []string{"convention recipe", "appended", "/h/.agents/docs/commits.overlay.md"},
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

// TestSplitFrontmatterOnlyTakesAWellFormedFence: a document that merely opens
// with a horizontal rule is prose, and swallowing it would silently delete a
// convention.
func TestSplitFrontmatterOnlyTakesAWellFormedFence(t *testing.T) {
	fields, body := splitFrontmatter("---\nderived_from: git log\n---\nthe convention\n")
	if fields["derived_from"] != "git log" {
		t.Errorf("derived_from = %q", fields["derived_from"])
	}
	if strings.TrimSpace(body) != "the convention" {
		t.Errorf("body = %q", body)
	}

	unterminated := "---\nnot really frontmatter\nthe convention\n"
	if fields, body := splitFrontmatter(unterminated); len(fields) != 0 || body != unterminated {
		t.Errorf("unterminated fence was treated as frontmatter: %v / %q", fields, body)
	}
}

// TestStackPreviewShowsTheAnswerAndTheOverlay pins what a surface showing a
// convention beside values of another sort gets: the same answer, overlay and
// provenance `get` prints, plus where the layer an editor writes lives. A rank
// the answer stood down is not shown — what is in force is the question the
// pane exists to answer.
func TestStackPreviewShowsTheAnswerAndTheOverlay(t *testing.T) {
	stack := Stack{Kind: KindCommits, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/h/.agents/docs/commits.md", Present: true, Body: "Imperative subjects."},
		{Origin: OriginRepository, Path: "/r/docs/agents/commits.md", Present: true, Body: "Conventional commits."},
		{Origin: OriginMemory, Path: "/d/conventions/commits.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md"},
	}}

	got := StackPreview(stack)

	for _, want := range []string{
		"ANSWER: USER DEFAULTS",
		"yours, every repository",
		"Imperative subjects.",
		"Provenance:",
		// The layer an editor here would write, named whether or not it holds
		// anything: a reader deciding to edit needs to know which of the four
		// they would be changing.
		"not written yet",
		"/h/.agents/docs/commits.overlay.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"Conventional commits.", "/d/conventions/commits.md"} {
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

// The pane shows the recipe when that is what answers: the ADR's rule is that
// `get` and the pane cannot describe one convention differently, and the rank
// in force is as much the pane's business as any other.
func TestStackPreviewOfAnUnwrittenKindShowsTheRecipe(t *testing.T) {
	stack := Stack{Kind: KindIssueTracker, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/h/.agents/docs/issue-tracker.md"},
		{Origin: OriginRepository, Path: "/r/docs/agents/issue-tracker.md"},
		{Origin: OriginMemory, Path: "/d/conventions/issue-tracker.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/issue-tracker.overlay.md"},
	}}

	got := StackPreview(stack)

	for _, want := range []string{"METHOD: CONVENTION RECIPE", Recipe(KindIssueTracker)} {
		if !strings.Contains(got, want) {
			t.Errorf("preview of an unwritten kind is missing %q:\n%s", want, got)
		}
	}
	// The overlay is still the layer an edit here writes.
	if !strings.Contains(got, "/h/.agents/docs/issue-tracker.overlay.md") {
		t.Errorf("preview does not name the layer an edit writes:\n%s", got)
	}
}
