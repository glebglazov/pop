package tasks

import (
	"fmt"
)

// Interrupt auto-drain revocation (ADR-0120): interrupting a live drain is the
// human taking manual ownership of the set, so it clears the set's Auto-drain
// consent bit and `pop work daemon` stops re-firing the set. The clear happens at
// two moments, for two different reasons:
//
//   - At the interrupt gate, the moment the interrupt is noticed and before the
//     human chooses, so a crash while the menu is up cannot let the daemon grab
//     the set mid-decision. Only this half snapshots and revives — Continue
//     restores the pre-interrupt value.
//   - At the drain's exit, for every run that ends interrupted. The gate covers
//     one phase only, the task-execution attempt; an interrupt during a
//     quota-recovery wait, during the Verifier or during the Reviewer ends the
//     drain from its own path and never shows a menu.
//
// Both go through revokeAutoDrainOnInterrupt, and the clear is idempotent: an
// interrupt that did pass the gate finds the bit already off at the exit, so it
// announces nothing and writes no second trace.

// revokeAutoDrainOnInterrupt clears the set's Auto-drain consent bit and returns
// the pre-interrupt value. The clear is unconditional and reuses
// SetTaskSetAutoDrain — the same primitive the terminal (DONE /
// AWAITING-APPROVAL) auto-drain clear uses — following that clear's
// announce-and-trace pattern (a line to the user plus a durable
// AUTO-DRAIN-CLEARED per-set progress note). SetTaskSetAutoDrain's changed flag is
// the snapshot: it is true exactly when the bit was on, and only then is the clear
// announced/traced (setting an already-clear bit is a clean no-op). Continue at
// the interrupt gate revives the snapshot; every other end of the drain leaves it
// cleared. A nil manifest — a set whose files no longer parse — still clears the
// bit and only skips the trace, because consent is the part that must not survive.
func (r *implementRun) revokeAutoDrainOnInterrupt(m *Manifest) (bool, error) {
	wasOn, err := SetTaskSetAutoDrain(r.d, r.resolved.DefinitionPath, r.taskSetID, false)
	if err != nil {
		return false, exitErr(ExitOperational, "clear auto-drain for task set %s: %v", r.taskSetID, err)
	}
	if wasOn {
		fmt.Fprintf(r.out, "Auto-drain cleared for task set %s: drain interrupted mid-run.\n", r.taskSetID)
		if m == nil {
			return wasOn, nil
		}
		if err := AppendSetProgress(r.d, m.Dir, "AUTO-DRAIN-CLEARED", "Auto-drain cleared: drain interrupted mid-run."); err != nil {
			return false, exitErr(ExitOperational, "record auto-drain clear for task set %s: %v", r.taskSetID, err)
		}
	}
	return wasOn, nil
}

// reviveAutoDrainOnContinue restores the pre-interrupt Auto-drain value when the
// human chooses Continue at the interrupt gate (ADR-0120): the consent
// revokeAutoDrainOnInterrupt cleared is re-granted and the user is told, with a
// symmetric durable AUTO-DRAIN-RESTORED per-set trace. It is called only when the
// snapshot was on, so it always re-enables the bit.
func (r *implementRun) reviveAutoDrainOnContinue(m *Manifest) error {
	if _, err := SetTaskSetAutoDrain(r.d, r.resolved.DefinitionPath, r.taskSetID, true); err != nil {
		return exitErr(ExitOperational, "restore auto-drain for task set %s: %v", r.taskSetID, err)
	}
	fmt.Fprintf(r.out, "Auto-drain restored for task set %s: continuing drain after interrupt.\n", r.taskSetID)
	if err := AppendSetProgress(r.d, m.Dir, "AUTO-DRAIN-RESTORED", "Auto-drain restored: drain continued after interrupt."); err != nil {
		return exitErr(ExitOperational, "record auto-drain restore for task set %s: %v", r.taskSetID, err)
	}
	return nil
}

// revokeAutoDrainOnInterruptedExit is the exit-path half: whichever phase the
// SIGINT landed in, a drain that ends interrupted leaves the set without consent.
// It runs after the loop and before the deferred finalize stamps the interrupted
// terminal, so the bit is off by the time the run's Drain row says the human took
// over.
//
// A failed clear is announced rather than returned. The run's error is what maps
// the Drain to its interrupted terminal (drainTerminal), so returning an
// operational error in its place would record the drain as finished and hide the
// interrupt; the thing the human has to act on is that the set may still be
// daemon-eligible, and saying so covers that.
func (r *implementRun) revokeAutoDrainOnInterruptedExit(err error) {
	if !isInterrupted(err) {
		return
	}
	if _, rerr := r.revokeAutoDrainOnInterrupt(r.setManifest()); rerr != nil {
		outputFor(r.out).line(ansiYellow, "━━ Auto-drain could not be cleared for task set %s after the interrupt: %v", r.taskSetID, rerr)
	}
}

// setManifest is the drained set's manifest from the run's opening snapshot. The
// exit-path revocation wants it only for the set directory its progress trace
// goes to, and that directory does not move during a run.
func (r *implementRun) setManifest() *Manifest {
	if r.refresh == nil {
		return nil
	}
	return r.refresh.Manifests[r.taskSetID]
}
