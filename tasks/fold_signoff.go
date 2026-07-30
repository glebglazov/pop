package tasks

import (
	"fmt"
	"strings"
)

// FoldEligibleStatus reports whether a bound task set may be folded (ADR-0156).
func FoldEligibleStatus(status TaskSetStatus) bool {
	return status == StatusDone || status == StatusAwaitingApproval
}

// OpenHITLTasks returns every open HITL task whose blockers are resolved —
// the remaining human sign-off work on an AWAITING-APPROVAL set.
func OpenHITLTasks(m *Manifest) []Task {
	if m == nil || !m.Valid {
		return nil
	}
	var out []Task
	for _, task := range m.Tasks {
		if task.Status == TaskOpen && task.Type == "HITL" && blockersResolved(m, task) {
			out = append(out, task)
		}
	}
	return out
}

// FoldSignOffTaskLabel formats one open HITL task for fold confirmation (id + title).
func FoldSignOffTaskLabel(task Task) string {
	id := strings.TrimSpace(task.ID)
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return id
	}
	slug := strings.ToLower(title)
	slug = strings.ReplaceAll(slug, " ", "")
	slug = strings.ReplaceAll(slug, "-", "")
	if slug != "" && strings.HasSuffix(strings.ToLower(strings.ReplaceAll(id, "-", "")), slug) {
		return id
	}
	if slug == "" {
		return id
	}
	return id + "-" + slug
}

// FormatFoldSignOffConfirmation names the HITL tasks fold will complete on sign-off.
func FormatFoldSignOffConfirmation(hitl []Task) string {
	labels := make([]string, len(hitl))
	for i, task := range hitl {
		labels[i] = FoldSignOffTaskLabel(task)
	}
	return "fold will complete: " + strings.Join(labels, ", ")
}

// CompleteFoldSignOff marks every attendable open HITL task done after trunk has
// landed successfully (ADR-0156).
func CompleteFoldSignOff(d *Deps, m *Manifest, projectPath string) error {
	hitl := OpenHITLTasks(m)
	if len(hitl) == 0 {
		return nil
	}
	ops := make([]TransitionOp, 0, len(hitl))
	for _, task := range hitl {
		ops = append(ops, TransitionOp{
			TaskID:  task.ID,
			To:      TaskDone,
			Actor:   ActorHuman,
			Marker:  "COMPLETE",
			Summary: fmt.Sprintf("fold sign-off completed %s/%s (was open)", m.Stem, task.ID),
		})
	}
	return ApplyTransitions(d, m, projectPath, ops)
}
