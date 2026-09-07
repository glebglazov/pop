package tasks

// exploreParked reads the Explore park off the set's Exploration mark (ADR-0262
// decision 6): a set whose author declared its tasks interrelated, on which an
// Explore pass has run, and which has no Exploration report to show for it.
// Every surface that passes over a parked set — the drain's own Explore step,
// automatic selection, the Work daemon's scan — reaches it through the one mark
// resolution, so the park and the mark a human reads can never disagree.
//
// The park is not a fact of its own: it is the unexplored mark with the one
// reason that means nobody should wait for the pass again.
func exploreParked(d *Deps, m *Manifest) bool {
	return exploreParkedBy(ResolveExploreMark(d, m))
}

// exploreParkedBy is that same reading of a mark already resolved, for the
// refresh that resolves every row's mark once and must not resolve it twice.
func exploreParkedBy(res ExploreResolution) bool {
	return res.Mark == ExploreMarkUnexplored && res.Reason == UnexploredPassFailed
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

// applyExploreMarks stamps every row with its Exploration mark and overlays the
// park that mark implies, replacing READY with EXPLORE-FAILED. It runs inside
// the refresh itself rather than per surface, because the mark is a pure read of
// the set directory — no store, no git, no config — so there is nothing for a
// surface to resolve differently, and every reader of a refresh (`pop tasks
// status`, the Work dashboard, the queue scan, automatic selection) then sees
// one answer.
//
// Only a READY row parks: a set whose AFK work is finished, failed or gated has
// nothing left for a map to shape, and the park exists to stop work from
// starting. The mark itself is stamped on every row, because a human reading a
// finished set still asks whether the map it was built from was ever drawn.
func applyExploreMarks(d *Deps, rows []Row, manifests map[string]*Manifest) {
	for i := range rows {
		rows[i].Explore = ResolveExploreMark(d, manifests[rows[i].ID])
		if rows[i].Status == StatusReady && exploreParkedBy(rows[i].Explore) {
			rows[i].Status = StatusExploreFailed
		}
	}
}
