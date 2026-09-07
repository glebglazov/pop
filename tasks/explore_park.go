package tasks

import "strings"

// exploreParked is the single read-side resolution of the Explore park
// (ADR-0262 decision 6): a set whose author declared its tasks interrelated, on
// which an Explore pass has run, and which has no Exploration report to show
// for it. Every surface that passes over a parked set — the status table, the
// drain's own Explore step, automatic selection, the Work daemon's scan —
// reaches this one answer, so none of them can disagree about whether a set is
// parked.
//
// It stores nothing and derives everything from the set directory: the pass's
// Captured runs of phase `explore` and the presence of the document itself.
// There is no verdict to cache as the Verify verdict has one, because the pass
// reaches no verdict — its product is the report, so the report's absence *is*
// the negative answer.
//
// The report is read through explorationBlock, the same reader the builders'
// prompts use, so "explored" means one thing across the feature.
//
// It is not gated on [work.explore]: the group decides whether a drain runs a
// pass, while the park is a fact about the set — it declared the tasks
// interrelated and has no map. The three doors ADR-0262 names are the way out,
// and switching a global group off is not one of them.
func exploreParked(d *Deps, m *Manifest) bool {
	if m == nil || !m.Valid || !m.ExploreRequested() {
		return false
	}
	if explorationBlock(d, m.Dir).HasExploration {
		return false
	}
	run, ok := latestCapturedRunOfPhase(d, m, spendPhaseExplore)
	return ok && explorePassGaveUp(run.Outcome)
}

// explorePassGaveUp reads an Explore run's ending for the one distinction the
// park turns on: whether the pass gave up, or never happened.
//
// The Explorer reaching an ending of its own and leaving no report is the pass
// giving up — it ran to completion saying nothing, failed, ran out of time, or
// stopped at its Turn cap — and by then the walk has spent the whole per-phase
// retry cap on it, so the park is a considered outcome rather than one flake.
//
// Every other ending is the machinery stopping the run rather than the pass
// answering: a human's interrupt, a quota pause, an agent nothing could start,
// a model the provider refused. Those are conditions a later drain finds gone,
// and parking on them would trade the set's autonomy for someone else's outage.
func explorePassGaveUp(outcome string) bool {
	switch outcome {
	case streamOutcomeCompleted, streamOutcomeFailed, streamOutcomeTimedOut, streamOutcomeTurnCapExhausted:
		return true
	}
	return false
}

// applyExploreParks overlays the Explore park onto a refresh's rows, replacing
// READY with EXPLORE-FAILED on every parked set. It runs inside the refresh
// itself rather than per surface, because the park is a pure read of the set
// directory — no store, no git, no config — so there is nothing for a surface to
// resolve differently, and every reader of a refresh (`pop tasks status`, the
// Work dashboard, the queue scan, automatic selection) then sees one answer.
//
// Only a READY row is considered: a set whose AFK work is finished, failed or
// gated has nothing left for a map to shape, and the park exists to stop work
// from starting.
func applyExploreParks(d *Deps, rows []Row, manifests map[string]*Manifest) {
	for i := range rows {
		if rows[i].Status != StatusReady {
			continue
		}
		if exploreParked(d, manifests[rows[i].ID]) {
			rows[i].Status = StatusExploreFailed
		}
	}
}

// latestCapturedRunOfPhase is a set's most recent Captured run of one phase. It
// reads the index halves only: what a phase's read-side mark needs is what each
// run recorded, never what it streamed, and decompressing every event payload to
// answer a gate preamble would cost a set's whole drain history.
//
// A set whose run directory cannot be read has no run of that phase as far as
// this can tell, which is what a set the phase never ran on also looks like —
// the direction both the Refine mark and the Explore park want to fail in.
func latestCapturedRunOfPhase(d *Deps, m *Manifest, phase string) (capturedRunMeta, bool) {
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
	// The list is chronological, so the last run of the phase in it is the newest.
	for _, meta := range metas {
		if meta.Phase == phase {
			latest, found = meta, true
		}
	}
	return latest, found
}
