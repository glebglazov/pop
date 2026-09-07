package tasks

import (
	"strings"

	"github.com/glebglazov/pop/work"
)

// ExploreMark is whether a set that asked to be explored has the map its tasks
// share, carried beside its status and its Verification mark rather than inside
// either (ADR-0262). Completion, verification, refinement and exploration are
// four independent facts, and one status slot holds one of them, so the fourth
// gets a mark for the same reason the second and third did.
//
// It is an alias of work.ExploreMark so a Work container can carry the field
// without `work` importing this package, exactly as TaskSetStatus and
// VerifyMark are. ResolveExploreMark is the one place that derives it.
type ExploreMark = work.ExploreMark

const (
	// ExploreMarkNone is the absent mark: the set never asked to be explored, so
	// there is nothing it was owed. A set with no declaration carries this mark
	// even when a human's hand run left it a report.
	ExploreMarkNone ExploreMark = ""
	// ExploreMarkExplored is a declared set with a report — the builders in it
	// are handed one map instead of each re-deriving the seam.
	ExploreMarkExplored ExploreMark = "explored"
	// ExploreMarkUnexplored is a declared set with no report. It is not a finding
	// about the work: it says only that the map the set asked for is not there,
	// and the reason beside it says how far the pass got.
	ExploreMarkUnexplored ExploreMark = "unexplored"
)

// UnexploredReason is why an unexplored set has no report — the detail beside
// the mark, never a second mark. Both are worth spelling out to a human,
// because they are two different next actions: waiting for a drain to run the
// pass, and taking one of the three doors out of a park (ADR-0262 decision 6).
type UnexploredReason string

const (
	// UnexploredReasonNone is the absent reason: the set is explored, or it
	// carries no mark at all.
	UnexploredReasonNone UnexploredReason = ""
	// UnexploredNotRun is a declared set no Explore pass has given up on — the
	// pass has not run yet, or every run it has was stopped by the machinery
	// rather than by the pass answering.
	UnexploredNotRun UnexploredReason = "not-run"
	// UnexploredPassFailed is a declared set whose pass ran to an ending of its
	// own and left no report. This is the reason the set parks on.
	UnexploredPassFailed UnexploredReason = "pass-failed"
)

// ExploreResolution is what the read-side resolution answers for one set: the
// mark every surface displays, the reason riding beside it, and the report as a
// pointer. The three travel together because every surface that says anything
// about exploration says some part of the same answer — the row's mark, the
// detail view's reason, the gate's pointer — so they are resolved once and
// handed round as one value.
type ExploreResolution struct {
	Mark ExploreMark
	// Reason is why the set has no report, blank unless Mark is
	// ExploreMarkUnexplored.
	Reason UnexploredReason
	// Pointer is where the report is and which tree it describes, never a word of
	// what it says (ADR-0252). HasReport is false for a set with no report at
	// all, which is not the same as the absent mark: a hand run on an undeclared
	// set leaves a report the set was never owed.
	Pointer   ExplorationPointer
	HasReport bool
}

// ResolveExploreMark is the single read-side Exploration mark resolution: whether a
// set that declared its tasks interrelated has the map they share, why not when
// it has not, and where the map is when it does. Every surface that says
// anything about exploration routes through here — the dashboard row and its
// detail view, `pop work status`, `pop tasks status`, the sign-off gate, and
// the park the drain and automatic selection read — so none of them can
// disagree about a set.
//
// It stores nothing and derives everything from the set directory: the document
// itself, through the one reader the builders' prompts use, and the pass's
// Captured runs of phase `explore`. There is no verdict to cache as the Verify
// verdict has one, because the pass reaches no verdict — its product is the
// report, so the report's absence *is* the negative answer.
//
// It is not gated on [work.explore]: the group decides whether a drain runs a
// pass, while the mark is a fact about the set — it asked to be explored and
// either has a map or does not.
func ResolveExploreMark(d *Deps, m *Manifest) ExploreResolution {
	res := ExploreResolution{}
	res.Pointer, res.HasReport = latestExplorationPointer(d, m)
	if m == nil || !m.Valid || !m.ExploreRequested() {
		return res
	}
	if res.HasReport {
		res.Mark = ExploreMarkExplored
		return res
	}
	res.Mark, res.Reason = ExploreMarkUnexplored, UnexploredNotRun
	if run, ok := latestCapturedRunOfPhase(d, m, spendPhaseExplore); ok && explorePassGaveUp(run.Outcome) {
		res.Reason = UnexploredPassFailed
	}
	return res
}

// exploreMarkPhrase is the mark and its reason as a human reads them, in the one
// wording every detail surface uses. A set carrying no mark reads as nothing at
// all: it was never owed a map, and saying so on every row would make the
// feature look like something every set opts out of.
func exploreMarkPhrase(res ExploreResolution) string {
	switch {
	case res.Mark == ExploreMarkExplored:
		return "Explored"
	case res.Mark != ExploreMarkUnexplored:
		return ""
	case res.Reason == UnexploredPassFailed:
		return "Not explored: the pass gave up and left no report"
	}
	return "Not explored: no pass has run yet"
}

// ExploreMarkBadgeText is the mark as a row's STATUS cell carries it: the mark's
// own word, and nothing on a row whose status already *is* the mark. A parked
// set reads EXPLORE-FAILED, and `unexplored` beside it would be the same fact in
// different words — the rule the Verified-at badge follows for NEEDS-VERIFY
// (ADR-0156).
func ExploreMarkBadgeText(c work.Container) string {
	if c.RawStatus == StatusExploreFailed {
		return ""
	}
	return string(c.ExploreMark)
}

// ExplorationSectionTitle names the Exploration block on every detail surface,
// the way ArtifactSectionTitle names the artifact one.
const ExplorationSectionTitle = "Exploration"

// explorationParts are the Exploration answer's tokens: the mark with its
// reason, then the report as a pointer — its path and the tree it records,
// never its body, which is a page long and would bury whatever it was printed
// into (ADR-0217, ADR-0252). Every surface prints these two and joins them as
// its own shape allows, so none of them decides what to say, only how wide.
//
// A set with nothing to say — no mark and no report — yields none.
func explorationParts(res ExploreResolution) []string {
	var parts []string
	if phrase := exploreMarkPhrase(res); phrase != "" {
		parts = append(parts, phrase)
	}
	if res.HasReport {
		parts = append(parts, res.Pointer.Summary())
	}
	return parts
}

// explorationLine is those tokens on one line, for the surfaces that have one:
// a gate preamble and a terminal's set detail. Empty when there is nothing to
// say.
func explorationLine(res ExploreResolution) string {
	return strings.Join(explorationParts(res), " · ")
}

// ExplorationSection is the same tokens as a detail view's prose block, one to
// a line, and false for a set with nothing to say.
func ExplorationSection(res ExploreResolution) (work.Section, bool) {
	parts := explorationParts(res)
	if len(parts) == 0 {
		return work.Section{}, false
	}
	return work.Section{Title: ExplorationSectionTitle, Body: strings.Join(parts, "\n")}, true
}
