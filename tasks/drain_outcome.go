package tasks

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DrainOutcome is the terminal disposition of a task-set drain. It is written to
// a machine-readable record on exit so the queue supervisor can react — agent
// fallback (which preset to cool down) and crash backoff (clean vs abnormal
// exit) — without parsing human-facing output.
type DrainOutcome string

const (
	// DrainOutcomeDone — every eligible task in the set completed.
	DrainOutcomeDone DrainOutcome = "done"
	// DrainOutcomeFailed — a task failed and draining stopped.
	DrainOutcomeFailed DrainOutcome = "failed"
	// DrainOutcomeBlocked — the set has no eligible AFK task (HITL gate or
	// unmet dependency) so draining could not proceed.
	DrainOutcomeBlocked DrainOutcome = "blocked"
	// DrainOutcomeDeferred — the set finished with skipped tasks deferred.
	DrainOutcomeDeferred DrainOutcome = "deferred"
	// DrainOutcomeQuotaPaused — draining stopped because an agent's quota ran
	// out; the task stays Open with partial changes preserved.
	DrainOutcomeQuotaPaused DrainOutcome = "quota_paused"
	// DrainOutcomeInterrupted — the drain was interrupted (SIGINT) mid-attempt.
	DrainOutcomeInterrupted DrainOutcome = "interrupted"
)

// Abnormal reports whether the drain ended abnormally — interrupted, or (by the
// absence of any record at all) crashed — rather than reaching a clean terminal
// stop (done / failed / blocked / deferred / quota-paused). A clean stop is a
// disposition the executor chose; an abnormal one is the process being torn down
// out from under it.
func (o DrainOutcome) Abnormal() bool {
	return o == DrainOutcomeInterrupted
}

// DrainOutcomeRecord is the structured record describing how a drain ended.
type DrainOutcomeRecord struct {
	SetID string `json:"set_id"`
	// Outcome is the terminal disposition of the drain.
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
	// value means unknown / unparseable and is omitted so fixed-interval
	// cooldown remains byte-identical for fallback records.
	ExhaustedResetAt time.Time `json:"exhausted_reset_at,omitempty,omitzero"`
	// RuntimePath is the runtime checkout the drain ran against; the record is
	// keyed by it on disk.
	RuntimePath string `json:"runtime_path"`
	// PID is the process that wrote the record.
	PID int `json:"pid"`
	// WrittenAt is when the record was written (UTC).
	WrittenAt time.Time `json:"written_at"`
}

// DrainOutcomeDirWith returns the directory for drain-outcome records.
func DrainOutcomeDirWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "drain-outcomes")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "drain-outcomes")
	}
	return filepath.Join(home, ".local", "share", "pop", "drain-outcomes")
}

// DrainOutcomePathFor returns the record path for a canonical runtime root.
// Records are keyed by the runtime checkout so a reader can look up the last
// drain outcome for a project without enumerating the directory.
func DrainOutcomePathFor(d *Deps, runtimeRoot string) string {
	sum := sha256.Sum256([]byte(runtimeRoot))
	name := fmt.Sprintf("%x.json", sum)
	return filepath.Join(DrainOutcomeDirWith(d), name)
}

// WriteDrainOutcome writes a drain-outcome record atomically, keyed by the
// record's RuntimePath. It survives the process exiting because it lands on disk
// before the writer returns.
func WriteDrainOutcome(d *Deps, rec DrainOutcomeRecord) error {
	if rec.WrittenAt.IsZero() {
		rec.WrittenAt = time.Now().UTC()
	}
	payload, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, DrainOutcomePathFor(d, rec.RuntimePath), payload, 0o644)
}

// ReadDrainOutcome reads the last drain-outcome record for a runtime root.
func ReadDrainOutcome(d *Deps, runtimeRoot string) (*DrainOutcomeRecord, error) {
	data, err := d.FS.ReadFile(DrainOutcomePathFor(d, runtimeRoot))
	if err != nil {
		return nil, err
	}
	var rec DrainOutcomeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// drainOutcomeFor maps a finished RunTaskSet (result, err) to a drain-outcome
// record. The second return is false when no record should be written — a
// declined run never drained, so there is nothing to report.
func drainOutcomeFor(setID, runtimePath string, result *RunTaskSetResult, err error) (DrainOutcomeRecord, bool) {
	outcome, preset, pinned, ok := classifyDrainOutcome(result, err)
	if !ok {
		return DrainOutcomeRecord{}, false
	}
	var resetAt time.Time
	if result != nil && result.QuotaPaused {
		resetAt = result.PauseResetAt
	}
	return DrainOutcomeRecord{
		SetID:            setID,
		Outcome:          outcome,
		ExhaustedPreset:  preset,
		ExhaustedPinned:  pinned,
		ExhaustedResetAt: resetAt,
		RuntimePath:      runtimePath,
		PID:              os.Getpid(),
	}, true
}

func classifyDrainOutcome(result *RunTaskSetResult, err error) (DrainOutcome, string, bool, bool) {
	if result != nil {
		switch {
		case result.Declined:
			return "", "", false, false
		case result.QuotaPaused:
			return DrainOutcomeQuotaPaused, result.PausePreset, result.PausePinnedAgent, true
		case result.TaskSetDone:
			return DrainOutcomeDone, "", false, true
		case result.TaskSetDeferred:
			return DrainOutcomeDeferred, "", false, true
		case result.BlockedReason != "":
			return DrainOutcomeBlocked, "", false, true
		}
	}
	if err != nil {
		var ee *ExitError
		if errors.As(err, &ee) {
			switch ee.Code {
			case ExitInterrupted:
				return DrainOutcomeInterrupted, "", false, true
			case ExitNoRunnable:
				// No eligible AFK task: a blocked / HITL-gated set.
				return DrainOutcomeBlocked, "", false, true
			}
		}
		return DrainOutcomeFailed, "", false, true
	}
	// Reached a clean end with no specific disposition flagged: treat as done.
	return DrainOutcomeDone, "", false, true
}
