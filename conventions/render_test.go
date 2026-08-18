package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// TestRenderStackEmptyNamesEveryPlaceItLooked pins the miss rendering, which is
// the only output a caller gets before pop exits 1: the four paths, in
// resolution order, so the reader knows where an answer would go.
func TestRenderStackEmptyNamesEveryPlaceItLooked(t *testing.T) {
	stack := Stack{Kind: KindCommits, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/h/.agents/docs/commits.md"},
		{Origin: OriginRepository, Path: "/r/docs/agents/commits.md"},
		{Origin: OriginMemory, Path: "/d/pop/repos/pop-abc/conventions/commits.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md"},
	}}

	var out bytes.Buffer
	if err := RenderStack(&out, stack); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "EMPTY") {
		t.Errorf("empty stack is not marked empty:\n%s", got)
	}
	for _, l := range stack.Layers {
		if !strings.Contains(got, l.Path) {
			t.Errorf("consulted path %q missing:\n%s", l.Path, got)
		}
	}
	// A table, not prose: the house plain-text style puts an uppercase header
	// over it.
	if !strings.Contains(got, "ORIGIN") || !strings.Contains(got, "PATH") {
		t.Errorf("consulted-paths table has no uppercase header:\n%s", got)
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
		name       string
		layers     []Layer
		answer     string
		hasAnswer  bool
		hasOverlay bool
		contested  bool
	}{
		{name: "nothing anywhere"},
		{name: "memory alone", layers: []Layer{memory}, answer: "POP", hasAnswer: true},
		{name: "the team's document stands memory down",
			layers: []Layer{repo, memory}, answer: "TEAM", hasAnswer: true, contested: true},
		{name: "the human's document stands the team's down",
			layers: []Layer{defaults, repo}, answer: "MINE", hasAnswer: true, contested: true},
		{name: "the human's document stands both down",
			layers: []Layer{defaults, repo, memory}, answer: "MINE", hasAnswer: true, contested: true},
		{name: "the overlay rides on the answer",
			layers: []Layer{repo, overlay}, answer: "TEAM", hasAnswer: true, hasOverlay: true},
		{name: "the overlay alone is no contender",
			layers: []Layer{overlay}, hasOverlay: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Stack{Kind: KindCommits, Layers: tt.layers}
			answer, ok := s.Answer()
			if ok != tt.hasAnswer || (ok && answer.Body != tt.answer) {
				t.Errorf("Answer() = %q, %v; want %q, %v", answer.Body, ok, tt.answer, tt.hasAnswer)
			}
			if _, ok := s.Overlay(); ok != tt.hasOverlay {
				t.Errorf("Overlay() present = %v, want %v", ok, tt.hasOverlay)
			}
			if got := s.Empty(); got != (!tt.hasAnswer && !tt.hasOverlay) {
				t.Errorf("Empty() = %v", got)
			}
			if got := s.Contested(); got != tt.contested {
				t.Errorf("Contested() = %v, want %v", got, tt.contested)
			}

			// The rendered surfaces carry the answer and the overlay, and never a
			// rank the answer stood down.
			prose, spoke := StackProse(s)
			if spoke == s.Empty() {
				t.Fatalf("StackProse spoke = %v for an Empty() = %v stack", spoke, s.Empty())
			}
			for _, l := range tt.layers {
				shown := strings.Contains(prose, l.Body)
				inForce := (tt.hasAnswer && l.Body == tt.answer) || l.Origin == OriginOverlay
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
			name: "an overlay with nothing to ride on says so",
			layers: []Layer{
				{Origin: OriginOverlay, Path: "/h/.agents/docs/commits.overlay.md", Present: true, Body: "b"},
			},
			want: []string{"your overlay alone", "no layer holds an answer"},
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

// An empty stack previews as the places pop looked, the same fact `get` gives a
// miss — without the recipe, which is a page of method and not a pane of state.
func TestStackPreviewOfAnEmptyStackNamesWherePopLooked(t *testing.T) {
	stack := Stack{Kind: KindIssueTracker, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/h/.agents/docs/issue-tracker.md"},
		{Origin: OriginRepository, Path: "/r/docs/agents/issue-tracker.md"},
		{Origin: OriginMemory, Path: "/d/conventions/issue-tracker.md"},
		{Origin: OriginOverlay, Path: "/h/.agents/docs/issue-tracker.overlay.md"},
	}}

	got := StackPreview(stack)

	if !strings.Contains(got, "nothing answers it") {
		t.Fatalf("an empty stack does not say so:\n%s", got)
	}
	for _, l := range stack.Layers {
		if !strings.Contains(got, l.Path) {
			t.Errorf("consulted path %q missing:\n%s", l.Path, got)
		}
	}
}
