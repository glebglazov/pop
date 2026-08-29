package tasks

// refineDirective is the small instruction refinePhase hands back to the drain
// loop, the third of the loop's phase directives so the loop skeleton keeps
// reading as orchestration. It has only the two values a step that gates nothing
// can have: Refine cannot park a set, cannot spawn work, and cannot change a
// terminal status, so the one thing that ends the drain here is the human who
// interrupted it.
type refineDirective int

const (
	// refineFallThrough proceeds to the terminal-status switch. It is what every
	// outcome of the phase returns — refine skipped, refine report written, or a
	// Refiner that could not produce a report — because the terminal status a
	// drain reaches must be the same one it would reach with Refine disabled.
	refineFallThrough refineDirective = iota
	// refineReturn hands the run's result and the returned error back to the
	// caller: the human interrupted the Refiner, which interrupts the drain.
	refineReturn
)

// refinePhase runs the drain's Refine step (ADR-0240): with [work.refine]
// enabled and the set's Refine episode armed, it writes the set's Refine
// report once and disarms.
//
// It sits after the verify phase and immediately before the terminal switch, and
// the order is the whole point. Verification may spawn a Remediation task and
// move the tree; a report written before that would describe a changeset that
// no longer exists by the time a human reads it. Running last means the report
// always describes the tree the sign-off gate is approving — and, because the
// verify phase returns rather than falling through when it parks a set, a
// VERIFY-FAILED set is never refined at a state nobody is being asked to accept.
//
// The episode rule is the Verification episode's, minus every carve-out: the
// refine pass records the done-AFK work composition it judged, an unchanged
// composition is not refined again, and new done-AFK work — including a
// Remediation task the verify phase spawned and the drain then completed —
// re-arms it. Refine needs no carve-out because it spawns no work and so cannot
// re-arm itself.
//
// Nothing here gates. A Refiner that fails, runs out of quota or finds nothing
// to refine leaves the episode armed and the drain's terminal status untouched:
// the next quiescence asks again, and the set reaches exactly the status it
// would have reached with Refine switched off.
func (r *implementRun) refinePhase(currentRefresh *RefreshResult, row *Row) (refineDirective, error) {
	cfg := r.plan.cfg
	if !refineEnabled(cfg) {
		return refineFallThrough, nil
	}
	// The same terminal zone verification gates: a set at DONE or
	// AWAITING-APPROVAL has finished its agent work and is what a human is about
	// to be asked to approve. A BLOCKED / DEFERRED / FAILED set is not.
	if row.Status != StatusDone && row.Status != StatusAwaitingApproval {
		return refineFallThrough, nil
	}
	m := currentRefresh.Manifests[r.taskSetID]
	// The per-set decline (ADR-0214), the Verifier's opt-out key for key: a set of
	// generated or vendored code says no to Refine here rather than by switching
	// the group off for every set in the repository.
	if m.RefineOptedOut() {
		return refineFallThrough, nil
	}
	composition := refineComposition(m)
	repo := ""
	if id, idErr := ResolveRepositoryIdentity(r.d, r.runtimePath); idErr == nil {
		repo = id.CommonDir
	}
	if !refineEpisodeArmed(r.d, repo, r.taskSetID, composition) {
		return refineFallThrough, nil
	}

	_, err := refineResolvedSet(r.d, cfg, refineCoreOptions{
		DefPath:     r.resolved.DefinitionPath,
		RuntimePath: r.runtimePath,
		Repo:        repo,
		SetID:       r.taskSetID,
		Timeout:     r.timeout,
		Output:      r.out,
		Convention:  r.opts.RefineConvention,
		runRefiner:  r.opts.refineRunner,
		probeMemo:   r.agentProbeMemo,
		// The drain is already holding this checkout for this set, so the refine
		// step runs inside that claim rather than asking for it again (ADR-0238).
		checkoutHeld: true,
	})
	if err == nil {
		return refineFallThrough, nil
	}
	if isInterrupted(err) {
		return refineReturn, err
	}
	// Everything else is reported and dropped. Refine is the one drain step
	// whose failure is allowed to cost nothing: the set is exactly as approvable
	// as it was, and the episode stays armed so the next drain asks again.
	outputFor(r.out).line(ansiYellow, "━━ Refine did not run for %s: %v", r.taskSetID, err)
	return refineFallThrough, nil
}
