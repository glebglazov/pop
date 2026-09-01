package tasks

// refineDirective is the small instruction refinePhase hands back to the drain
// loop, the first of the loop's phase directives so the loop skeleton keeps
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

// refinePhase runs the drain's Refine step (ADR-0252): with [work.refine]
// enabled and the set's Refine episode armed, a fresh Refiner fixes in place
// what the resolved `refine` convention licenses, the runner commits the pass,
// and the set's Refine report records what was fixed and what was left.
//
// It sits at AFK quiescence immediately *before* the verify phase, and the order
// is the whole point. A pass's edits are code like any other, so they need
// judging; running first means the Verifier that was going to read this
// changeset reads the refined one instead, and the pass costs no verification of
// its own. Placed after verify it would either leave its edits unjudged or force
// a second heavy pass on every drain that refined anything.
//
// The episode rule is the Verification episode's with one carve-out: the refine
// pass records the *non-remediation* done-AFK composition it judged, so a
// verify → FIXABLE → remediate → re-verify lap re-verifies without re-refining
// and the heavy pass stays out of the iteration that must be cheapest. Real new
// work re-arms it. Refine needs no carve-out for itself, because it spawns no
// work and so cannot re-arm itself.
//
// A human-completed set is skipped entirely. The drain does not edit code a
// human declared done: direct edits are a stronger intrusion than the verdict
// dispositions the same bit already suspends. `pop tasks refine` still refines
// one on request — that is the human re-opening the question.
//
// Nothing here gates. A Refiner that fails, runs out of quota or finds nothing
// to fix leaves the episode armed and the drain's terminal status untouched:
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
	// Human completion (ADR-0252), read from the manifest bit the transition
	// chokepoint writes — the same source the verify phase reads before it
	// declines to park or remediate such a set.
	if m != nil && m.HumanCompleted {
		return refineFallThrough, nil
	}
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
		Convention:  r.opts.ImplementationConvention,
		Overlay:     r.opts.DocumentOverlay,
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
