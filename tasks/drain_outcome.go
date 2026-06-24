package tasks

import (
	"os"
	"time"
)

// DrainOutcome is a Drain's terminal exit reason — how its process ended, never
// the set's resulting work disposition (ADR-0056). Whether a finished drain left
// the set done, failed, blocked, unverified, or deferred is read from the
// manifest-derived Task set status, not from the Drain.
//
// The disposition constants below are retained for the queue journal and
// supervisor reaction wiring during the store migration; the Drain itself only
// ever records the exit-reason terminals (finished / quota_paused / interrupted,
// plus crashed in a later slice).
type DrainOutcome string

const (
	// DrainOutcomeFinished — the drain process completed its run. The set's
	// disposition (done/failed/…) is derived from the manifest, not the Drain.
	DrainOutcomeFinished DrainOutcome = "finished"
	// DrainOutcomeDone — every eligible task in the set completed.
	DrainOutcomeDone DrainOutcome = "done"
	// DrainOutcomeFailed — a task failed and draining stopped.
	DrainOutcomeFailed DrainOutcome = "failed"
	// DrainOutcomeBlocked — the set has no eligible AFK task and open AFK work
	// remains gated behind a human (setup or mid-flow HITL), so draining could
	// not proceed.
	DrainOutcomeBlocked DrainOutcome = "blocked"
	// DrainOutcomeUnverified — all AFK work is done/skipped; only a terminal
	// HITL verification gate stands before Done. Agents are finished; human
	// sign-off is all that remains.
	DrainOutcomeUnverified DrainOutcome = "unverified"
	// DrainOutcomeDeferred — the set finished with skipped tasks deferred.
	DrainOutcomeDeferred DrainOutcome = "deferred"
	// DrainOutcomeQuotaPaused — draining stopped because an agent's quota ran
	// out; the task stays Open with partial changes preserved.
	DrainOutcomeQuotaPaused DrainOutcome = "quota_paused"
	// DrainOutcomeInterrupted — the drain was interrupted (SIGINT) mid-attempt.
	DrainOutcomeInterrupted DrainOutcome = "interrupted"
	// DrainOutcomeCrashed — the drain process died without recording a terminal;
	// detected by opportunistic reconciliation (a dead-PID running Drain), not
	// inferred from a missing record (ADR-0055).
	DrainOutcomeCrashed DrainOutcome = "crashed"
)

// Abnormal reports whether the drain ended abnormally — interrupted or crashed —
// rather than reaching a clean terminal stop. A clean stop (finished,
// quota_paused) is how the executor exited; an abnormal one is the process being
// torn down out from under it. This axis feeds backoff in a later slice.
func (o DrainOutcome) Abnormal() bool {
	return o == DrainOutcomeInterrupted || o == DrainOutcomeCrashed
}

// DrainOutcomeRecord is the structured terminal a reader projects from the
// latest non-running Drain for a runtime checkout. It is no longer a file; the
// fields are populated from the store.
type DrainOutcomeRecord struct {
	SetID string `json:"set_id"`
	// Outcome is the terminal exit reason of the drain.
	Outcome DrainOutcome `json:"outcome"`
	// ExhaustedPreset names the agent preset whose quota ran out. It is set only
	// when Outcome is DrainOutcomeQuotaPaused, and powers agent fallback's
	// decision of which preset to cool down.
	ExhaustedPreset string `json:"exhausted_preset,omitempty"`
	// ExhaustedPinned reports that the quota-paused attempt was running because
	// the task itself pinned ExhaustedPreset, not because the queue selected it
	// as the rotating default.
	ExhaustedPinned bool `json:"exhausted_pinned,omitempty"`
	// ExhaustedResetAt is the agent-reported absolute reset instant. A zero
	// value means unknown / unparseable.
	ExhaustedResetAt time.Time `json:"exhausted_reset_at,omitempty,omitzero"`
	// RuntimePath is the runtime checkout the drain ran against.
	RuntimePath string `json:"runtime_path"`
	// PID is the process that ran the drain.
	PID int `json:"pid"`
	// WrittenAt is when the drain reached its terminal (UTC).
	WrittenAt time.Time `json:"written_at"`
}

// ReadDrainOutcome returns the latest terminal Drain for a runtime checkout,
// projected from the store. It returns os.ErrNotExist when no terminal drain is
// recorded there, preserving the contract callers relied on from the file era.
func ReadDrainOutcome(d *Deps, runtimeRoot string) (*DrainOutcomeRecord, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, os.ErrNotExist
	}
	defer func() { _ = s.Close() }()
	drain, err := s.LatestTerminalByRuntimePath(runtimeRoot)
	if err != nil {
		return nil, err
	}
	if drain == nil {
		return nil, os.ErrNotExist
	}
	return &DrainOutcomeRecord{
		SetID:            drain.SetID,
		Outcome:          DrainOutcome(drain.State),
		ExhaustedPreset:  drain.ExhaustedPreset,
		ExhaustedPinned:  drain.ExhaustedPinned,
		ExhaustedResetAt: drain.ExhaustedResetAt,
		RuntimePath:      drain.RuntimePath,
		PID:              drain.PID,
		WrittenAt:        drain.FinishedAt,
	}, nil
}
