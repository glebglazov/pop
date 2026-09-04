package tasks

import (
	"strings"

	"github.com/glebglazov/pop/config"
)

// RefineMark is whether a terminal Task set's changeset was held to the
// implementation standard, carried beside its status and its Verification mark
// rather than inside either (ADR-0260). Completion, verification and refinement
// are three independent facts: whether the work is finished, whether a Verifier
// judged it, and whether a Refiner applied the standard to it. One status slot
// holds one of them, so the third gets a mark for the same reason the second
// did.
//
// It answers one question — was this refined, yes or no — because to a human at
// a sign-off gate the reasons a pass did not refine all mean the same thing.
// Those reasons ride beside it as an UnrefinedReason. ResolveRefineMark is the
// one place that derives both.
type RefineMark string

const (
	// RefineMarkNone is the absent mark: Refine is disabled, the set opted out of
	// it, or the set is not terminal and so has no finished changeset to have
	// been held to anything.
	RefineMarkNone RefineMark = ""
	// RefineMarkRefined is a terminal set whose last Refine pass reached a
	// refined outcome — the standard was applied to this changeset.
	RefineMarkRefined RefineMark = "refined"
	// RefineMarkNotRefined is a terminal set nothing has refined. It is not a
	// finding about the code and it withholds nothing: it says only that the
	// check did not happen, and the reason beside it says why.
	RefineMarkNotRefined RefineMark = "not-refined"
)

// UnrefinedReason is why a not-refined set was not refined — the detail beside
// the mark, never a second mark. Only one of the four is worth spelling out to a
// human in words: a gate that was already red is a statement about the work
// rather than about the pass (ADR-0260 decision 3).
type UnrefinedReason string

const (
	// UnrefinedReasonNone is the absent reason: the set is refined, or it carries
	// no mark at all.
	UnrefinedReasonNone UnrefinedReason = ""
	// UnrefinedInterrupted is a pass that did not reach an outcome pop could
	// record — a human's interrupt, a timeout, a crash, a quota pause, or a run
	// that ended clean having produced no report.
	UnrefinedInterrupted UnrefinedReason = "interrupted"
	// UnrefinedGateBlocked is a pass that never began, because the scoped gate
	// was already red when refinement started.
	UnrefinedGateBlocked UnrefinedReason = "gate-blocked"
	// UnrefinedAbandoned is a pass that gave up: the gate went red under its own
	// edits and it could not leave it green, so pop discarded them.
	UnrefinedAbandoned UnrefinedReason = "abandoned"
	// UnrefinedNeverRan is a set no Refine pass has ever been recorded for.
	UnrefinedNeverRan UnrefinedReason = "never-ran"
)

// RefineResolution is what the read-side resolution answers for one set: the
// mark every surface displays, and — when the mark is not-refined — the reason
// riding beside it. The two are separate fields because they are separate
// questions: the mark is what a human at a gate decides on, the reason is the
// detail behind it (ADR-0260 decision 2).
type RefineResolution struct {
	Mark RefineMark
	// Reason is why the set was not refined, blank unless Mark is
	// RefineMarkNotRefined.
	Reason UnrefinedReason
}

// ResolveRefineMark is the single read-side Refine mark resolution: whether
// the standard was applied to a set's changeset, and why not when it was not
// (ADR-0260 decision 5). Every surface that says anything about refinement
// routes through here, so the sign-off gate, the detail view and the Assist
// prompt cannot disagree about a set.
//
// It stores nothing and derives everything from what a Refine pass already left
// behind: the set's Captured runs of phase `refine`. The Refine episode is not
// consulted, and that is the point — an episode is written only for a refined
// outcome, so reading it alone cannot tell a set whose pass failed from one
// whose new work re-armed it.
//
// The latest refine run is the whole answer. Its stream outcome comes first: a
// run that timed out or crashed left prose that pop never turned into a report,
// whatever that prose claimed the pass had done. Only a run that ran to its own
// ending is read for the outcome it recorded.
//
// It is read-only and gates nothing (ADR-0260 decision 6): no caller may park,
// refuse or hold back a set on what it answers.
func ResolveRefineMark(d *Deps, cfg *config.Config, m *Manifest) RefineResolution {
	if m == nil || !refineEnabled(cfg) || m.RefineOptedOut() {
		return RefineResolution{}
	}
	if !TerminalStatus(DeriveStatus(m)) {
		// Nothing is finished, so there is no changeset to have been refined.
		return RefineResolution{}
	}
	run, ok := latestRefineRun(d, m)
	if !ok {
		return RefineResolution{Mark: RefineMarkNotRefined, Reason: UnrefinedNeverRan}
	}
	if run.Outcome != streamOutcomeCompleted {
		return RefineResolution{Mark: RefineMarkNotRefined, Reason: UnrefinedInterrupted}
	}
	// A refine run files the pass outcome it reported in the run's own outcome
	// slot, the way a verify run files its verdict there. Empty is a run that
	// answered nothing at all, and a pass that produced no report refined nothing.
	switch run.Verdict {
	case refineOutcomeRefined:
		return RefineResolution{Mark: RefineMarkRefined}
	case refineOutcomeGateBlocked:
		return RefineResolution{Mark: RefineMarkNotRefined, Reason: UnrefinedGateBlocked}
	case refineOutcomeAbandoned:
		return RefineResolution{Mark: RefineMarkNotRefined, Reason: UnrefinedAbandoned}
	}
	return RefineResolution{Mark: RefineMarkNotRefined, Reason: UnrefinedInterrupted}
}

// latestRefineRun is the set's most recent Captured run of phase `refine`. It
// reads the index halves only: what the mark needs is what each run recorded,
// never what it streamed, and decompressing every event payload to answer a
// gate preamble would cost a set's whole drain history.
//
// A set whose run directory cannot be read has no refine run as far as this can
// tell, which is what a set that was never refined also looks like — the same
// direction the episode rule fails in.
func latestRefineRun(d *Deps, m *Manifest) (capturedRunMeta, bool) {
	if d == nil {
		d = defaultDeps
	}
	if d == nil || d.FS == nil || m == nil || strings.TrimSpace(m.Dir) == "" {
		return capturedRunMeta{}, false
	}
	metas, err := listCapturedRunMetas(d, m.Dir)
	if err != nil {
		return capturedRunMeta{}, false
	}
	var latest capturedRunMeta
	found := false
	// The list is chronological, so the last refine run in it is the newest.
	for _, meta := range metas {
		if meta.Phase == "refine" {
			latest, found = meta, true
		}
	}
	return latest, found
}

// refineMarkPhrase is the mark as a human reads it, in the one wording every
// surface uses. Gate-blocked is the single reason spelled out: it says the
// scoped gate was already red, which is a fact about the work rather than about
// the pass, and the human signing off is the one who can act on it. The other
// three all mean "nothing held this to the standard", and naming them here would
// ask a reader to tell them apart for a decision they are not making.
func refineMarkPhrase(res RefineResolution) string {
	switch {
	case res.Mark == RefineMarkRefined:
		return "Refined"
	case res.Mark != RefineMarkNotRefined:
		return ""
	case res.Reason == UnrefinedGateBlocked:
		return "Not refined: the scoped gate was already red when the pass began"
	}
	return "Not refined"
}
