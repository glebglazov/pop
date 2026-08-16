package tasks

// reviewDirective is the small instruction reviewPhase hands back to the drain
// loop, the third of the loop's phase directives so the loop skeleton keeps
// reading as orchestration. It has only the two values a step that gates nothing
// can have: a review cannot park a set, cannot spawn work, and cannot change a
// terminal status, so the one thing that ends the drain here is the human who
// interrupted it.
type reviewDirective int

const (
	// reviewFallThrough proceeds to the terminal-status switch. It is what every
	// outcome of the phase returns — review skipped, review written, or a
	// Reviewer that could not produce a document — because the terminal status a
	// drain reaches must be the same one it would reach with review disabled.
	reviewFallThrough reviewDirective = iota
	// reviewReturn hands the run's result and the returned error back to the
	// caller: the human interrupted the Reviewer, which interrupts the drain.
	reviewReturn
)

// reviewPhase runs the drain's Code review step (ADR-0214): with [work.review]
// enabled and the set's Review episode armed, it writes the set's Review
// artifact once and disarms.
//
// It sits after the verify phase and immediately before the terminal switch, and
// the order is the whole point. Verification may spawn a Remediation task and
// move the tree; a document written before that would describe a changeset that
// no longer exists by the time a human reads it. Running last means the document
// always describes the tree the sign-off gate is approving — and, because the
// verify phase returns rather than falling through when it parks a set, a
// VERIFY-FAILED set is never reviewed at a state nobody is being asked to accept.
//
// The episode rule is the Verification episode's, minus every carve-out: the
// review records the done-AFK work composition it judged, an unchanged
// composition is not reviewed again, and new done-AFK work — including a
// Remediation task the verify phase spawned and the drain then completed —
// re-arms it. Review needs no carve-out because it spawns no work and so cannot
// re-arm itself.
//
// Nothing here gates. A Reviewer that fails, runs out of quota or finds nothing
// to review leaves the episode armed and the drain's terminal status untouched:
// the next quiescence asks again, and the set reaches exactly the status it
// would have reached with review switched off.
func (r *implementRun) reviewPhase(currentRefresh *RefreshResult, row *Row) (reviewDirective, error) {
	cfg := r.plan.cfg
	if !reviewEnabled(cfg) {
		return reviewFallThrough, nil
	}
	// The same terminal zone verification gates: a set at DONE or
	// AWAITING-APPROVAL has finished its agent work and is what a human is about
	// to be asked to approve. A BLOCKED / DEFERRED / FAILED set is not.
	if row.Status != StatusDone && row.Status != StatusAwaitingApproval {
		return reviewFallThrough, nil
	}
	m := currentRefresh.Manifests[r.taskSetID]
	composition := reviewComposition(m)
	repo := ""
	if id, idErr := ResolveRepositoryIdentity(r.d, r.runtimePath); idErr == nil {
		repo = id.CommonDir
	}
	if !reviewEpisodeArmed(r.d, repo, r.taskSetID, composition) {
		return reviewFallThrough, nil
	}

	_, err := reviewResolvedSet(r.d, cfg, reviewCoreOptions{
		DefPath:     r.resolved.DefinitionPath,
		RuntimePath: r.runtimePath,
		Repo:        repo,
		SetID:       r.taskSetID,
		Timeout:     r.timeout,
		Output:      r.out,
		Convention:  r.opts.ReviewConvention,
		runReviewer: r.opts.reviewRunner,
		probeMemo:   r.agentProbeMemo,
	})
	if err == nil {
		return reviewFallThrough, nil
	}
	if isInterrupted(err) {
		return reviewReturn, err
	}
	// Everything else is reported and dropped. A review is the one drain step
	// whose failure is allowed to cost nothing: the set is exactly as approvable
	// as it was, and the episode stays armed so the next drain asks again.
	outputFor(r.out).line(ansiYellow, "━━ Code review did not run for %s: %v", r.taskSetID, err)
	return reviewFallThrough, nil
}
