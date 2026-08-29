package tasks

import (
	"fmt"
)

// verifyDirective is the small instruction verifyPhase hands back to the drain
// loop so the loop skeleton reads as orchestration: the verify phase decides
// what happens next, the loop acts on it.
type verifyDirective int

const (
	// verifyFallThrough proceeds to the terminal-status switch: the verify phase
	// did not apply (verification off / opted out / not at a DONE/AWAITING-APPROVAL
	// terminal) or the set passed, so its terminal status stands.
	verifyFallThrough verifyDirective = iota
	// verifyContinue keeps draining: a FIXABLE verdict spawned a Remediation task
	// under the cap, a quota-recovery wait resumed, or a human gate disposition
	// cleared the verify-failed terminal.
	verifyContinue
	// verifyReturn hands the run's result and the returned error back to the
	// caller (quota-pause exit, a verifier error, or the verify-failed exit).
	verifyReturn
)

// verifyPhase runs the pre-approval Verifier phase (ADR-0086): with Agent
// verification enabled and the set not opted out, a drain that has exhausted the
// set's AFK work at DONE / AWAITING-APPROVAL must clear a SHA-gated Verify verdict
// before its terminal status stands. The verdict is cache-first — a re-drain at an
// unchanged work SHA reuses the stored verdict and never re-invokes the Verifier,
// so a parked set does not loop. A PASS lets DONE / AWAITING-APPROVAL stand and
// falls through to the switch (verifyFallThrough). A FIXABLE verdict makes the
// Verifier a task producer: while the set is under its remediation depth cap it
// spawns a new AFK Remediation task whose body is the findings and keeps draining
// (verifyContinue) — the Drain picks the task up, its completion moves the work
// SHA, the cached verdict goes stale, and the Verifier re-fires, closing the loop.
// A human-completed set is the exception to everything below the verdict itself:
// the Verifier still runs and its verdict is still recorded and printed, but a
// non-PASS neither parks the set nor spawns remediation, because the status is
// the human's assertion and the verdict rides beside it as a mark. A quota pause
// on such a set is dropped for the same reason: nothing the Verifier could say
// would change the terminal, so the drain does not wait for the quota to return.
// At or over the cap (or on a NEEDS-HUMAN verdict) the set parks as VERIFY-FAILED:
// on a TTY a human gate dispositions it (Accept / Remediate / shell / exit) and a
// handled disposition clears the terminal and resumes (verifyContinue); otherwise
// the drain records the verify_failed terminal and exits (verifyReturn). The
// Runtime execution lock is still held on entry, so the Verifier and the
// remediation write run under it; the verify-fail gate parks it first so the menu
// runs lock-free (ADR-0067/0100).
//
// It mutates the run's result / Drain / prompt-reader state through the receiver so
// the drain loop and the deferred finalize see the latest values. Quota-pause
// during verify parks and waits — except on a human-completed set, where the
// verdict is informational and the pause is reported and dropped — and a failed
// waiter registration exits with the
// QuotaPaused result populated; a human gate disposition clears the verify-failed
// terminal from the result and resumes; the `--yes` path prints the summary and
// exits with the ExitNoRunnable error.
func (r *implementRun) verifyPhase(currentRefresh *RefreshResult, row *Row) (verifyDirective, error) {
	d := r.d
	opts := r.opts
	cfg := r.plan.cfg
	out := r.out
	runtimePath := r.runtimePath
	taskSetID := r.taskSetID
	timeout := r.timeout
	result := r.result

	m := currentRefresh.Manifests[taskSetID]
	if !(verifyEnabled(cfg) && !m.VerifyOptedOut() &&
		(row.Status == StatusDone || row.Status == StatusAwaitingApproval)) {
		return verifyFallThrough, nil
	}

	repo := ""
	if id, idErr := ResolveRepositoryIdentity(d, runtimePath); idErr == nil {
		repo = id.CommonDir
	}
	effective, verdict, verr := drainVerifyPhase(d, cfg, verifyCoreOptions{
		Repo:        repo,
		RuntimePath: runtimePath,
		SetID:       taskSetID,
		Agents:      opts.VerifyAgents,
		Effort:      opts.VerifyEffort,
		Timeout:     timeout,
		Output:      out,
		Convention:  opts.VerificationConvention,
		runVerifier: opts.verifyRunner,
		probeMemo:   r.agentProbeMemo,
	}, m, row.Status)
	if verr != nil {
		if qp, ok := AsVerifyQuotaPause(verr); ok {
			if m != nil && m.HumanCompleted {
				// Waiting buys nothing here: the verdict this Verifier would have
				// produced cannot gate the set (a non-PASS on a human-completed set is
				// information, not a veto), so parking the drain until quota returns
				// would hold a Runtime lock for a mark. Drop it like the Refiner does —
				// the set keeps its terminal status, the mark stays unverified, and the
				// Verifier is still scheduled, so a later drain records it.
				outputFor(out).line(ansiYellow, "━━ Verifier quota paused for %s (%s) — the set is human-completed, so the drain does not wait", taskSetID, qp.Preset)
				return verifyFallThrough, nil
			}
			priority := 0
			if row := findRow(currentRefresh, taskSetID); row != nil {
				priority = row.Priority
			}
			u := NewQuotaPauseVerdict(qp.Preset, qp.Reason, qp.ResetAt)
			th, _ := u.TimeHealing()
			regFailed, waitErr := ParkAndWaitForQuotaRecovery(d, &r.drain, taskSetID, u.Preset, th, runtimePath, priority, out, r.ensureDrain)
			if waitErr != nil {
				return verifyReturn, waitErr
			}
			if regFailed {
				result.ProceedVerdict = &u
				result.QuotaPaused = true
				result.PauseReason = qp.Reason
				result.PausePreset = qp.Preset
				result.PauseResetAt = qp.ResetAt
				result.Refresh = currentRefresh
				printTaskSetSummary(out, result)
				return verifyReturn, nil
			}
			return verifyContinue, nil
		}
		return verifyReturn, verr
	}
	if effective == StatusVerifyFailed && m.HumanCompleted {
		// A human already asserted this set is done, so the finding is information,
		// not a veto: the verdict is recorded and printed (the status surfaces it as
		// a verify-failed mark beside DONE) and the drain falls through to its
		// terminal switch. Remediation is skipped for the same reason — spawning
		// fix work would reopen the set and erase the assertion.
		return verifyFallThrough, nil
	}
	if effective == StatusVerifyFailed {
		// A FIXABLE verdict under the cap spawns a Remediation task and
		// keeps draining; only a NEEDS-HUMAN verdict or an exhausted cap
		// actually parks the set.
		if verdict != nil && Verdict(verdict.Verdict) == VerdictFixable {
			spawned, remID, rerr := spawnRemediationIfUnderCap(d, m, repo, remediationSpawn{
				WorkSHA:         verdict.WorkSHA,
				Findings:        verdict.Findings,
				VerifierSummary: verdict.Summary,
				CommitSubject:   remediationCommitSubject(m, verdict),
			}, maxRemediationDepth(cfg))
			if rerr != nil {
				return verifyReturn, rerr
			}
			if spawned {
				outputFor(out).line(ansiBold+ansiCyan, "━━ Spawned remediation task %s — resuming the drain", remID)
				return verifyContinue, nil
			}
		}
		result.TaskSetVerifyFailed = true
		if verdict != nil {
			result.VerifyFindings = verdict.Findings
		}
		result.VerifyRerunCmd = FormatVerifyCommand(taskSetID, opts.VerifyAgents, opts.VerifyEffort)
		// Overlay the verdict-derived disposition on the display row so the
		// rendered table reads VERIFY-FAILED, matching `pop tasks status`.
		row.Status = StatusVerifyFailed
		row.Progress = BuildProgress(m, StatusVerifyFailed)
		row.VerifyFindings = result.VerifyFindings
		if !opts.Yes {
			fmt.Fprintln(out)
			Render(out, currentRefresh)
		}
		// Verify-fail gate (ADR-0103): on a TTY, let a human disposition the
		// set — Accept / Remediate / shell / exit. Accept and Remediate invoke
		// the same store/spawn behavior as the CLI flags, and both keep the
		// drain going (Accept flips the set to verified via a human-authored
		// PASS; Remediate spawns drainable fix work). Park the Runtime
		// execution lock first so the menu runs lock-free (ADR-0067) and a
		// concurrent quota-recovery drain cannot resume mid-gate (ADR-0100).
		verifyGateWillPrompt := !opts.Yes && canPrompt(opts.ConfirmIn) && m != nil
		if verifyGateWillPrompt {
			r.parkDrain()
			// A verify-fail gate is a human wait that claims nothing (ADR-0135): a
			// non-claiming hold for quiescence occupancy only.
			_ = RegisterCheckoutGateHold(d, taskSetID, runtimePath, false)
		}
		r.sharedPromptReader = ensurePromptReader(r.sharedPromptReader, opts.ConfirmIn, opts.Yes)
		findings := ""
		if verdict != nil {
			findings = verdict.Findings
		}
		handled, gateErr := handleInteractiveVerifyFailedGate(r.newGateEnv(), repo, m, verifyWorkSHA(d, runtimePath), findings)
		if verifyGateWillPrompt {
			r.releaseGateHold()
		}
		if gateErr != nil {
			return verifyReturn, gateErr
		}
		if handled {
			// The human disposed of the set; clear the verify-failed terminal so
			// the deferred finalize does not record verify_failed, and resume.
			result.TaskSetVerifyFailed = false
			result.VerifyFindings = ""
			return verifyContinue, nil
		}
		if opts.Yes {
			printTaskSetSummary(out, result)
		}
		return verifyReturn, exitErr(ExitNoRunnable, "Task set %q verification failed — a human must review it\n%s", taskSetID, result.VerifyRerunCmd)
	}
	return verifyFallThrough, nil
}
