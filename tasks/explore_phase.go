package tasks

// exploreDirective is the instruction explorePhase hands back to the drain,
// shaped like the refine phase's so the two read alike at their call sites.
type exploreDirective int

const (
	// exploreFallThrough proceeds into the drain loop — the pass wrote the
	// report, reused the one already there, or was declined.
	exploreFallThrough exploreDirective = iota
	// exploreReturn hands the run's result and the returned error back to the
	// caller: the human interrupted the Explorer, which interrupts the drain.
	exploreReturn
)

// explorePhase runs the drain's Explore step (ADR-0262): a set whose author set
// the Explore directive and which has no Exploration report yet gets one
// written by a fresh read-only Explorer before the drain selects its first
// task, so every builder in the set is handed the same map of the code its
// tasks touch instead of re-deriving one per attempt.
//
// It sits at the head of the drain, not at quiescence where refine and verify
// sit, because it is the only phase whose output is an input to the work: a
// report written after the tasks ran would be a map of a seam nobody needed
// describing.
//
// The pass runs on the one condition the ADR states, absent or present: no
// report means write it, a report means reuse it. Nothing compares the SHA the
// report names to the checkout. An implement attempt commits, so a SHA-exact
// rule would call the report stale after task one and spend an Explorer per
// task to avoid spending exploration per task; the correction duty in the
// builder's own prompt is what keeps a report honest instead.
//
// [work.explore] is the one agent group that defaults on. The set's directive is
// what opts a set in, so a set that never declared it costs nothing — and with
// the group switched off `pop tasks explore <set>` still writes a report on
// request, the way `pop tasks refine` does for a disabled Refine group.
//
// A human-completed set is declined, as refinement declines one: exploration
// exists to shape work that is about to be built, and there is none.
//
// It is the one agent phase that can stop a drain (ADR-0262 decision 5): a set
// whose author said the tasks are interrelated and which has no report parks
// rather than letting its builders start from separate maps. The gate is asked
// of exploreParked, so what stops the drain here is the same fact the status
// table shows and the daemon passes over.
func (r *implementRun) explorePhase() (exploreDirective, error) {
	// --skip-explore is the door that drains a declared set once unexplored. It
	// is a choice about this invocation, so it writes no report and clears no
	// park: the next drain asks the question again.
	if r.opts.SkipExplore {
		return exploreFallThrough, nil
	}
	m := r.refresh.Manifests[r.taskSetID]
	if m == nil || !m.ExploreRequested() {
		return exploreFallThrough, nil
	}
	// Human completion (ADR-0252), read from the manifest bit the transition
	// chokepoint writes — the same source the refine phase reads.
	if m.HumanCompleted {
		return exploreFallThrough, nil
	}
	// The report the builders' prompts resolve, resolved by the same reader: a
	// set whose document is present and non-empty is an explored set, and this
	// pass has nothing left to do for it.
	if explorationBlock(r.d, m.Dir).HasExploration {
		return exploreFallThrough, nil
	}

	if r.plan.cfg.ExploreEnabled() {
		_, err := exploreResolvedSet(r.d, r.plan.cfg, exploreCoreOptions{
			DefPath:     r.resolved.DefinitionPath,
			RuntimePath: r.runtimePath,
			SetID:       r.taskSetID,
			Timeout:     r.timeout,
			Output:      r.out,
			probeMemo:   r.agentProbeMemo,
			runExplorer: r.opts.exploreRunner,
			// The drain is already holding this checkout for this set, so the explore
			// step reads under that claim rather than asking for it again (ADR-0238).
			checkoutHeld: true,
		})
		if err == nil {
			return exploreFallThrough, nil
		}
		if isInterrupted(err) {
			return exploreReturn, err
		}
		// An Explorer that could not answer is named in the drain's output; whether
		// that ends the drain is the park's question, asked below.
		outputFor(r.out).line(ansiYellow, "━━ Exploration did not run for %s: %v", r.taskSetID, err)
	}
	// The pass just gave up, or an earlier one did and the group is switched off
	// so this drain ran none. Either way the park is re-derived rather than
	// inferred from the error: a run the machinery cut short (a quota pause, an
	// agent nothing could start) is a condition a later drain finds gone, and the
	// drain carries on unexplored as it did before this gate existed.
	if !exploreParked(r.d, m) {
		return exploreFallThrough, nil
	}
	return exploreReturn, r.parkUnexplored(m)
}

// parkUnexplored stops the drain at the Explore gate: it appends the one
// set-level Progress record that says why the work stopped — so the reason is
// readable without opening a Captured run — names the three doors out, and
// returns the no-runnable error the drain exits on.
//
// The record is the park's only write. The status is derived, not stored
// (exploreParked), so there is nothing to persist and nothing to clear when a
// later pass writes the report.
func (r *implementRun) parkUnexplored(m *Manifest) error {
	out := outputFor(r.out)
	out.line(ansiRed, "✗ Task set %s is parked: the explore phase produced no Exploration report", r.taskSetID)
	out.line(ansiDim, "   Explore it by hand:  pop tasks explore %s", r.taskSetID)
	out.line(ansiDim, "   Drain it unexplored: pop tasks implement %s --skip-explore", r.taskSetID)
	out.line(ansiDim, "   Retract it:          set \"explore\": false in %s", m.Path)
	if err := AppendSetProgress(r.d, m.Dir, string(StatusExploreFailed), exploreParkSummary); err != nil {
		return exitErr(ExitOperational, "record the explore park for task set %s: %v", r.taskSetID, err)
	}
	return exitErr(ExitNoRunnable, "Task set %q is parked: the explore phase produced no Exploration report, and the set declares its tasks interrelated", r.taskSetID)
}

// exploreParkSummary is what the park writes into the set's Progress record. It
// names the phase, the reason and the ways out in full, because a human reading
// progress.txt weeks later has neither the drain's output nor the Captured run
// in front of them.
const exploreParkSummary = "Explore phase: the pass spent its retry cap without producing an Exploration report, and the set declares its tasks interrelated, so nothing was built against separate maps. Explore it by hand, drain it once with --skip-explore, or retract the explore declaration."
