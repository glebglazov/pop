package work_test

import (
	"slices"
	"testing"

	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The ladder's third pass (ADR-0241 decisions 1 and 2). These tests are about the
// pass's arity and its place in the ladder rather than about any kind's answer, so
// the kinds are stubs: what is being pinned here is that the repository pass
// concatenates every kind's answer where the two passes above it stop at the first,
// and that it is reached only when both of them are silent.

// repositoryStubKind is a stubKind that also answers the repository pass, with the
// containers a test handed it. Two of these on one page is the whole fixture for
// the merge.
type repositoryStubKind struct {
	stubKind
	repoCommonDir string
	answers       []work.AttributedContainer
}

func (k repositoryStubKind) AttributePaneRepository(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.RepoCommonDir != k.repoCommonDir || len(k.answers) == 0 {
		return work.Attribution{}, false
	}
	return work.Attribution{Containers: k.answers}, true
}

// answering builds one kind that holds the named containers in the repository.
func answering(id work.KindID, repo string, ids ...string) repositoryStubKind {
	k := repositoryStubKind{
		stubKind:      stubKind{id: id},
		repoCommonDir: repo,
	}
	for _, containerID := range ids {
		k.stubKind.containers = append(k.stubKind.containers, work.Container{
			ID:        containerID,
			CursorKey: string(id) + ":" + containerID,
		})
		k.answers = append(k.answers, work.AttributedContainer{
			Ref:       ref.WorkRef{Kind: ref.Kind(id), ContainerID: containerID},
			CursorKey: string(id) + ":" + containerID,
			Label:     string(id) + " " + containerID,
		})
	}
	return k
}

// attributedKeys is the answer in the order the ladder produced it, which is the
// only thing this pass decides.
func attributedKeys(att *work.Attribution) []string {
	if att == nil {
		return nil
	}
	var keys []string
	for _, c := range att.Containers {
		keys = append(keys, c.CursorKey)
	}
	return keys
}

// Decision 2: the repository pass is the one place the ladder is not first-hit.
// Both kinds hold work in the pane's repository and both contribute, concatenated
// in kind precedence order with each kind's own ranking kept inside its block. The
// arity is pinned here rather than left to the slice that wires the second real
// kind, because first-hit would look correct for as long as only one kind answers.
func TestTheRepositoryPassConcatenatesEveryKindThatHasWorkThere(t *testing.T) {
	const repo = "/repo/.git"
	sets := answering(ref.KindTaskSet, repo, "2026-06-01-older", "2026-08-01-newer")
	maps := answering(ref.KindMap, repo, "2026-07-01-chart")
	// Handed to the ladder in the wrong order on purpose: precedence is the
	// builder's, not the caller's.
	kinds := []work.Kind{maps, sets}

	att := work.AttributePane(kinds, work.PaneFacts{PaneID: "%4", RepoCommonDir: repo})

	want := []string{
		"task-set:2026-06-01-older",
		"task-set:2026-08-01-newer",
		"map:2026-07-01-chart",
	}
	if got := attributedKeys(att); !slices.Equal(got, want) {
		t.Fatalf("repository attribution = %v, want both kinds' answers concatenated in precedence order %v", got, want)
	}
}

// A kind with nothing in the repository contributes nothing and does not stop the
// pass: the merge skips it rather than answering on its behalf.
func TestTheRepositoryPassSkipsAKindWithNothingThere(t *testing.T) {
	const repo = "/repo/.git"
	sets := answering(ref.KindTaskSet, repo)
	maps := answering(ref.KindMap, repo, "2026-07-01-chart")

	att := work.AttributePane([]work.Kind{sets, maps}, work.PaneFacts{PaneID: "%4", RepoCommonDir: repo})

	if got, want := attributedKeys(att), []string{"map:2026-07-01-chart"}; !slices.Equal(got, want) {
		t.Fatalf("attribution = %v, want %v", got, want)
	}
}

// Decision 1: the pass is the weakest rung, so any answer from either pass above
// it wins outright — including a checkout answer from one kind over a repository
// answer from another. Nothing merges across the boundary.
func TestAnyAnswerAboveTheRepositoryPassWinsOutright(t *testing.T) {
	const repo = "/repo/.git"
	sets := answering(ref.KindTaskSet, repo, "2026-06-01-older", "2026-08-01-newer")
	maps := answering(ref.KindMap, repo, "2026-07-01-chart")
	facts := work.PaneFacts{PaneID: "%4", RepoCommonDir: repo}

	t.Run("a tag beats every repository answer", func(t *testing.T) {
		tagged := taggedStubKind{repositoryStubKind: maps, answer: maps.answers[0]}
		att := work.AttributePane([]work.Kind{sets, tagged}, facts)
		if got, want := attributedKeys(att), []string{"map:2026-07-01-chart"}; !slices.Equal(got, want) {
			t.Fatalf("attribution = %v, want only the tagged container %v", got, want)
		}
	})

	t.Run("a checkout answer from one kind beats a repository answer from another", func(t *testing.T) {
		near := neighbourhoodStubKind{repositoryStubKind: maps, answer: maps.answers[0]}
		att := work.AttributePane([]work.Kind{sets, near}, facts)
		if got, want := attributedKeys(att), []string{"map:2026-07-01-chart"}; !slices.Equal(got, want) {
			t.Fatalf("attribution = %v, want only the checkout answer %v", got, want)
		}
	})
}

// A pane in no repository, or in one no kind holds work for, is attributed to
// nothing: the pass answers or it is silent, and silence is not an error.
func TestAPaneWithNoRepositoryToAnswerForIsAttributedToNothing(t *testing.T) {
	sets := answering(ref.KindTaskSet, "/repo/.git", "2026-06-01-older")

	for _, tc := range []struct {
		name  string
		facts work.PaneFacts
	}{
		{"outside any repository", work.PaneFacts{PaneID: "%4", Directory: "/tmp/elsewhere"}},
		{"a repository holding no work", work.PaneFacts{PaneID: "%4", RepoCommonDir: "/other/.git"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if att := work.AttributePane([]work.Kind{sets}, tc.facts); att != nil {
				t.Fatalf("attribution = %v, want none", attributedKeys(att))
			}
		})
	}
}

// taggedStubKind answers the ladder's first pass as well as its third, so a test
// can watch one kind's tag beat another kind's repository.
type taggedStubKind struct {
	repositoryStubKind
	answer work.AttributedContainer
}

func (k taggedStubKind) AttributePane(work.PaneFacts) (work.Attribution, bool) {
	return work.AttributeOne(k.answer), true
}

// neighbourhoodStubKind answers the ladder's second pass as well as its third.
type neighbourhoodStubKind struct {
	repositoryStubKind
	answer work.AttributedContainer
}

func (k neighbourhoodStubKind) AttributePaneNeighbourhood(work.PaneFacts) (work.Attribution, bool) {
	return work.AttributeOne(k.answer), true
}
