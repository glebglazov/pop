package tasks

import (
	"fmt"
	"path"
	"strings"
)

// TaskSetStatus is a derived task status for one Task-set row.
type TaskSetStatus string

const (
	StatusMissing          TaskSetStatus = "MISSING"
	StatusDone             TaskSetStatus = "DONE"
	StatusMalformed        TaskSetStatus = "MALFORMED"
	StatusFailed           TaskSetStatus = "FAILED"
	StatusReady            TaskSetStatus = "READY"
	StatusDeferred         TaskSetStatus = "DEFERRED"
	StatusBlocked          TaskSetStatus = "BLOCKED"
	StatusAwaitingApproval TaskSetStatus = "AWAITING-APPROVAL"
	// StatusNeedsVerify is a set whose AFK work is complete but has no fresh PASS
	// Verify verdict at the current work SHA — the verdict is absent or stale, so
	// Agent verification must (re-)run before the set can advance (ADR-0086). It
	// appears only when Agent verification is enabled.
	StatusNeedsVerify TaskSetStatus = "NEEDS-VERIFY"
	// StatusVerifyFailed is a set the Verifier could not clear on its own — a
	// NEEDS-HUMAN verdict at the current work SHA (ADR-0086/0087). It parks with
	// the findings for a human. Appears only when Agent verification is enabled.
	StatusVerifyFailed TaskSetStatus = "VERIFY-FAILED"
)

// Row is one line in the task status table.
type Row struct {
	ID     string
	Status TaskSetStatus
	// Started reports that the set already has at least one done task. With a
	// Ready status it refines the display label to "IN PROGRESS" (render-only,
	// see StatusLabel); it never affects schedulability.
	Started          bool
	Priority         int
	PriorityShow     string
	AutoDrain        bool
	Progress         string
	BlockedReason    string
	FailedTasks      []string
	ResetHints       []string
	CompleteHint     string
	MalformedSummary string
	DetailErrors     []string
	// ConfigError is a config/registration-class fault on the set that is not a
	// manifest malformity — currently an unsatisfiable worktree directive
	// (ADR-0059): `managed` with no resolvable Trunk worktree, or a `name` with no
	// such worktree on this machine. It is surfaced in the status detail and
	// diagnostics so the operator fixes the environment; the set is not drained.
	ConfigError string
	RegIndex    int
	NextPick    bool
	RunTarget   bool
	// VerifyFindings carries the Verifier's human-facing reasons for a
	// VERIFY-FAILED row (ADR-0086); empty for every other status.
	VerifyFindings string
}

// StatusLabel returns a row's display label. A started Ready set (one that
// already has a done task) is shown as "IN PROGRESS" — a presentational
// refinement of READY, not a derived status: scheduling and summary logic key
// on Status, never on this label. Every other row shows its raw status.
func StatusLabel(r Row) string {
	if r.Status == StatusReady && r.Started {
		return "IN PROGRESS"
	}
	return string(r.Status)
}

// anyDone reports whether the manifest has at least one done task.
func anyDone(m *Manifest) bool {
	if m == nil {
		return false
	}
	for _, task := range m.Tasks {
		if task.Status == "done" {
			return true
		}
	}
	return false
}

// DeriveStatus computes Task-set status from manifest validation.
func DeriveStatus(m *Manifest) TaskSetStatus {
	if m == nil {
		return StatusMalformed
	}
	if !m.Valid {
		return StatusMalformed
	}
	if allDone(m) {
		return StatusDone
	}
	if anyFailed(m) {
		return StatusFailed
	}
	if hasEligibleTask(m) {
		return StatusReady
	}
	if isDeferred(m) {
		return StatusDeferred
	}
	// Terminal HITL: all AFK work is done/skipped, only the human approval gate
	// remains (the agent has already verified — the human signs off).
	// Gating HITL: at least one open AFK task is still waiting behind the gate.
	if hasOpenAFKTask(m) {
		return StatusBlocked
	}
	return StatusAwaitingApproval
}

// DeriveStatusWithVerdict layers the SHA-gated Verify verdict onto the
// manifest-derived status (ADR-0086). When verification is disabled it returns
// the manifest status unchanged — all-AFK-done still reaches DONE and no
// NEEDS-VERIFY/VERIFY-FAILED state ever appears. When enabled, the verdict
// becomes a real gate on the terminal zone (a set whose AFK work is complete —
// manifest status DONE or AWAITING-APPROVAL):
//
//   - verdict absent or stale (nil, because the caller looks it up at the set's
//     current work SHA) → NEEDS-VERIFY
//   - PASS → the manifest status stands (DONE when nothing is open, or
//     AWAITING-APPROVAL when a terminal HITL approval task is still open)
//   - any non-PASS verdict (NEEDS-HUMAN, and — until remediation ships in a
//     later slice — FIXABLE) → VERIFY-FAILED
//
// Every other manifest status is returned untouched, so READY/FAILED/DEFERRED/
// MALFORMED/MISSING are never gated, and a BLOCKED set (an open AFK task still
// gated behind a human) stays BLOCKED — its work is not complete, so there is
// nothing to verify yet. The verdict is a SHA-keyed cache, not a completion
// flag: when the work SHA moves the caller's lookup misses, verdict is nil, and
// the set returns to NEEDS-VERIFY, so "artifacts drive status" still holds.
func DeriveStatusWithVerdict(m *Manifest, verifyEnabled bool, verdict *Verdict) TaskSetStatus {
	base := DeriveStatus(m)
	if !verifyEnabled {
		return base
	}
	switch base {
	case StatusDone, StatusAwaitingApproval:
		// AFK work is complete — the verdict decides.
	default:
		return base
	}
	if verdict == nil {
		return StatusNeedsVerify
	}
	if *verdict == VerdictPass {
		return base
	}
	return StatusVerifyFailed
}

// isDeferred reports whether every task is Done or Skipped with at least one
// Skipped. A still-Open task (AFK or HITL) leaves the set Ready or Blocked,
// never Deferred (see ADR 0006).
func isDeferred(m *Manifest) bool {
	if len(m.Tasks) == 0 {
		return false
	}
	anySkipped := false
	for _, task := range m.Tasks {
		switch task.Status {
		case "skipped":
			anySkipped = true
		case "done":
		default:
			return false
		}
	}
	return anySkipped
}

// SkippedTaskIDs returns the IDs of skipped tasks in manifest-array order.
func SkippedTaskIDs(m *Manifest) []string {
	if m == nil {
		return nil
	}
	var ids []string
	for _, task := range m.Tasks {
		if task.Status == "skipped" {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func allDone(m *Manifest) bool {
	if len(m.Tasks) == 0 {
		return false
	}
	for _, task := range m.Tasks {
		if task.Status != "done" {
			return false
		}
	}
	return true
}

func anyFailed(m *Manifest) bool {
	for _, task := range m.Tasks {
		if task.Status == "failed" {
			return true
		}
	}
	return false
}

func taskDone(m *Manifest, id string) bool {
	for _, task := range m.Tasks {
		if task.ID == id {
			return task.Status == "done"
		}
	}
	return false
}

// blockerSatisfied reports whether a blocked_by prerequisite is satisfied.
// A Skipped task is deliberately set aside, not completed, yet it satisfies
// blocked_by so its dependents can proceed (see ADR 0006).
func blockerSatisfied(m *Manifest, id string) bool {
	for _, task := range m.Tasks {
		if task.ID == id {
			return task.Status == "done" || task.Status == "skipped"
		}
	}
	return false
}

func hasEligibleTask(m *Manifest) bool {
	for _, task := range m.Tasks {
		if isEligible(m, task) {
			return true
		}
	}
	return false
}

func isEligible(m *Manifest, task Task) bool {
	if task.Status != "open" || task.Type != "AFK" {
		return false
	}
	for _, blocker := range task.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return false
		}
	}
	return true
}

// hasOpenAFKTask reports whether any AFK task is still open (eligible or not).
func hasOpenAFKTask(m *Manifest) bool {
	for _, task := range m.Tasks {
		if task.Status == "open" && task.Type == "AFK" {
			return true
		}
	}
	return false
}

// BuildProgress returns compact progress text for a row.
func BuildProgress(m *Manifest, status TaskSetStatus) string {
	if status == StatusMissing {
		return ""
	}

	if m == nil {
		return ""
	}

	done, open, failed, hitl, skipped := 0, 0, 0, 0, 0
	for _, task := range m.Tasks {
		switch task.Status {
		case "done":
			done++
		case "failed":
			failed++
		case "skipped":
			skipped++
		case "open":
			if task.Type == "HITL" {
				hitl++
			} else {
				open++
			}
		}
	}
	total := len(m.Tasks)
	parts := []string{fmt.Sprintf("%d/%d done", done, total)}
	if open > 0 {
		parts = append(parts, fmt.Sprintf("%d open", open))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if hitl > 0 {
		parts = append(parts, fmt.Sprintf("%d HITL", hitl))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	return strings.Join(parts, ", ")
}

// BuildBlockedReason explains why an Task set is blocked.
func BuildBlockedReason(m *Manifest) string {
	if m == nil || !m.Valid {
		return ""
	}

	for _, task := range m.Tasks {
		if task.Status != "open" {
			continue
		}
		if task.Type == "HITL" && blockersResolved(m, task) {
			return fmt.Sprintf("HITL: %s", task.ID)
		}
	}

	for _, task := range m.Tasks {
		if task.Status != "open" || task.Type != "AFK" {
			continue
		}
		for _, blocker := range task.BlockedBy {
			if !blockerSatisfied(m, blocker) {
				return fmt.Sprintf("blocked by %s", blocker)
			}
		}
	}
	return "no eligible AFK task"
}

// BlockingHITLTask returns the open HITL task whose blockers are all resolved
// — the task that holds a Human-blocked Task set at a human-in-the-loop gate.
// Returns nil when no such task exists.
func BlockingHITLTask(m *Manifest) *Task {
	if m == nil || !m.Valid {
		return nil
	}
	for i := range m.Tasks {
		task := m.Tasks[i]
		if task.Status == "open" && task.Type == "HITL" && blockersResolved(m, task) {
			return &m.Tasks[i]
		}
	}
	return nil
}

// FailedTask returns the first failed task in a manifest, or nil when none has
// failed. A set goes Failed on its first failure and selection halts before
// another task can fail, so this is the single task the Failed gate targets.
func FailedTask(m *Manifest) *Task {
	if m == nil {
		return nil
	}
	for i := range m.Tasks {
		if m.Tasks[i].Status == "failed" {
			return &m.Tasks[i]
		}
	}
	return nil
}

func blockersResolved(m *Manifest, task Task) bool {
	for _, blocker := range task.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return false
		}
	}
	return true
}

// taskPathHint returns the canonical copy-paste Task target reference for an
// task file: the <task-set>/<file>.md form (see ADR 0039).
func taskPathHint(stem, file string) string {
	return path.Join(stem, file)
}

// resetTaskHint returns the copy-paste open command for a task file.
func resetTaskHint(stem, file string) string {
	return fmt.Sprintf("pop tasks open %s", taskPathHint(stem, file))
}

// completeTaskHint returns the copy-paste complete command for a task file.
func completeTaskHint(stem, file string) string {
	return fmt.Sprintf("pop tasks complete %s", taskPathHint(stem, file))
}

// skipTaskHint returns the copy-paste skip command for a task file.
func skipTaskHint(stem, file string) string {
	return fmt.Sprintf("pop tasks skip %s", taskPathHint(stem, file))
}

// BuildFailedInfo returns failed task IDs and reset command hints.
func BuildFailedInfo(stem string, m *Manifest) (ids []string, hints []string) {
	if m == nil {
		return nil, nil
	}
	for _, task := range m.Tasks {
		if task.Status == "failed" {
			ids = append(ids, task.ID)
			hints = append(hints, resetTaskHint(stem, task.File))
		}
	}
	return ids, hints
}

// MalformedSummary returns a compact malformed summary for table display.
func MalformedSummary(m *Manifest) string {
	if m == nil {
		return "malformed"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0]
	}
	if len(m.Errors) > 1 {
		return fmt.Sprintf("%d validation errors", len(m.Errors))
	}
	return "malformed"
}
