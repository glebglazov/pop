package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var progressHeaderPattern = regexp.MustCompile(`^(\S+)\s+\[([^\]]+)\]\s+(\S+)\s*$`)

// BuildAgentPrompt generates the instruction prompt for an task attempt.
func BuildAgentPrompt(taskPath, runtimePath string) string {
	tasksDir := filepath.Dir(taskPath)
	var b strings.Builder
	fmt.Fprintf(&b, "You are implementing the task at: %s\n\n", taskPath)
	fmt.Fprintf(&b, "Read the task file in full. Follow any optional context references it\n")
	fmt.Fprintf(&b, "contains (for example a \"## Parent\" section) when present; the task may also\n")
	fmt.Fprintf(&b, "be self-contained. Implement the work described under \"What to build\" and\n")
	fmt.Fprintf(&b, "satisfy every box under \"Acceptance criteria\". As you complete each\n")
	fmt.Fprintf(&b, "criterion, check its box (`- [ ]` → `- [x]`) in %s.\n\n", taskPath)
	fmt.Fprintf(&b, "Do NOT modify %s. Do NOT modify other task files in %s.\n",
		filepath.Join(tasksDir, "index.json"), tasksDir)
	fmt.Fprintf(&b, "Do NOT make git commits — the runner handles assessment and committing.\n\n")
	fmt.Fprintf(&b, "Runtime checkout: %s\n\n", runtimePath)
	fmt.Fprintf(&b, "Implementation edits belong only beneath the runtime checkout.\n\n")
	fmt.Fprintf(&b, "This attempt is a single non-interactive session. There is no human and no\n")
	fmt.Fprintf(&b, "later turn: once you end your response the attempt is over, and ending\n")
	fmt.Fprintf(&b, "without a completion sentinel (TASK_COMPLETE or TASK_FAILED) is recorded as a\n")
	fmt.Fprintf(&b, "failure. To wait on a long-running command, keep polling it across successive\n")
	fmt.Fprintf(&b, "bash calls until it finishes (or fails) — never background the work and end\n")
	fmt.Fprintf(&b, "your turn to \"wait\", which orphans it and yields no sentinel. A single bash\n")
	fmt.Fprintf(&b, "call may be killed at its own tool timeout (~10 min), but the whole attempt\n")
	fmt.Fprintf(&b, "has a far longer timeout (~1 hour), so poll across calls rather than waiting\n")
	fmt.Fprintf(&b, "within one.\n\n")
	fmt.Fprintf(&b, "When you have completed the work, print a summary block followed by the\n")
	fmt.Fprintf(&b, "completion sentinel as the final lines of your output, exactly:\n\n")
	fmt.Fprintf(&b, "SUMMARY_START\n")
	fmt.Fprintf(&b, "<one or more lines describing what you did>\n")
	fmt.Fprintf(&b, "SUMMARY_END\n")
	fmt.Fprintf(&b, "TASK_COMPLETE\n\n")
	fmt.Fprintf(&b, "If you cannot complete the task (blocked, unclear, missing info, repeated\n")
	fmt.Fprintf(&b, "failure), instead print as the final line:\n\n")
	fmt.Fprintf(&b, "TASK_FAILED: <one-line reason>\n")
	return b.String()
}

// BuildHITLAssistancePrompt generates the attended-agent prompt shown when a
// Task set reaches a human-in-the-loop gate.
func BuildHITLAssistancePrompt(d *Deps, taskSetID string, m *Manifest, blocking Task, runtimePath string) string {
	if d == nil {
		d = defaultDeps
	}
	if d.FS == nil {
		d.FS = DefaultDeps().FS
	}

	taskPath := filepath.Join(m.Dir, blocking.File)
	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human at a HITL gate for a Pop task set.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", taskSetID)
	fmt.Fprintf(&b, "Task set path: %s\n", m.Dir)
	fmt.Fprintf(&b, "Blocking HITL task: %s", blocking.ID)
	if blocking.Title != "" {
		fmt.Fprintf(&b, " - %s", blocking.Title)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Human-facing task path: %s\n", taskPath)
	if runtimePath != "" {
		fmt.Fprintf(&b, "Runtime checkout: %s\n", runtimePath)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Allowed manual outcomes:\n")
	fmt.Fprintf(&b, "- complete: the human marks the HITL task done after verifying the required work.\n")
	fmt.Fprintf(&b, "- defer: the human skips the HITL task so downstream work can continue while the set remains Deferred.\n")
	fmt.Fprintf(&b, "- edit and rerun: the human edits tasks or implementation state, then reruns the task set.\n")
	fmt.Fprintf(&b, "- exit without changing task state: leave the HITL task open and make no manual override.\n\n")

	fmt.Fprintf(&b, "Full HITL task body:\n")
	if data, err := d.FS.ReadFile(taskPath); err == nil {
		fmt.Fprintf(&b, "```markdown\n%s\n```\n\n", strings.TrimRight(string(data), "\n"))
	} else {
		fmt.Fprintf(&b, "Could not read %s: %v.\n", taskPath, err)
		fmt.Fprintf(&b, "Proceed by inspecting the task path manually or asking the human for the missing task body.\n\n")
	}

	fmt.Fprintf(&b, "Task set context:\n")
	for _, task := range m.Tasks {
		fmt.Fprintf(&b, "- %s [%s %s]", task.ID, task.Type, task.Status)
		if task.Title != "" {
			fmt.Fprintf(&b, " %s", task.Title)
		}
		fmt.Fprintf(&b, " (%s)", filepath.Join(m.Dir, task.File))
		if len(task.BlockedBy) > 0 {
			fmt.Fprintf(&b, "; blocked_by: %s", strings.Join(task.BlockedBy, ", "))
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Completed AFK work from task artifacts:\n")
	completed := completedAFKProgress(d, m)
	if len(completed) == 0 {
		fmt.Fprintf(&b, "- No completed AFK work summary is available in progress.txt.\n\n")
	} else {
		for _, item := range completed {
			fmt.Fprintf(&b, "- %s (%s, %s at %s)\n", item.TaskID, item.File, item.Outcome, item.Timestamp)
			for _, line := range strings.Split(item.Summary, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "Use the repository and task context to help the human decide which allowed outcome is correct. Do not mark tasks complete or skipped unless the human explicitly chooses that outcome.\n")
	return b.String()
}

// BuildFailedAssistancePrompt generates the attended-agent prompt shown when a
// Task set stops at the Failed gate. It reopens the failed task for another
// attempt: the agent sees the task body — framed as the work to do again — and
// the structured failure reason from the last attempt, scoped to the two
// outcomes the Failed gate allows (re-run or complete by hand). It deliberately
// omits defer (not an option at the Failed gate) and never points the agent at
// the raw captured stream; the structured reason is the durable signal (ADR
// 0020).
func BuildFailedAssistancePrompt(d *Deps, taskSetID string, m *Manifest, failed Task, runtimePath string) string {
	if d == nil {
		d = defaultDeps
	}
	if d.FS == nil {
		d.FS = DefaultDeps().FS
	}

	taskPath := filepath.Join(m.Dir, failed.File)
	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human with a failed task in a Pop task set.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", taskSetID)
	fmt.Fprintf(&b, "Task set path: %s\n", m.Dir)
	fmt.Fprintf(&b, "Failed task: %s", failed.ID)
	if failed.Title != "" {
		fmt.Fprintf(&b, " - %s", failed.Title)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Task path: %s\n", taskPath)
	if runtimePath != "" {
		fmt.Fprintf(&b, "Runtime checkout: %s\n", runtimePath)
	}
	fmt.Fprintf(&b, "\n")

	if reason, err := LatestFailureReason(d, m.Dir, failed.File); err == nil && reason != "" {
		fmt.Fprintf(&b, "Why the last attempt failed:\n%s\n\n", strings.TrimRight(reason, "\n"))
	} else {
		fmt.Fprintf(&b, "Why the last attempt failed: no structured failure reason was recorded for the last attempt.\n\n")
	}

	fmt.Fprintf(&b, "Allowed outcomes:\n")
	fmt.Fprintf(&b, "- re-run: fix the underlying problem in the runtime checkout so a fresh attempt can pass; the human then reruns the task set to retry the task AFK.\n")
	fmt.Fprintf(&b, "- complete by hand: the human finishes the task's work directly and marks the task done.\n")
	fmt.Fprintf(&b, "These are the only outcomes at the Failed gate. Do not change task state yourself; the human chooses the outcome.\n\n")

	fmt.Fprintf(&b, "Treat the following as the task to work again. Read it in full and satisfy every acceptance criterion:\n")
	if data, err := d.FS.ReadFile(taskPath); err == nil {
		fmt.Fprintf(&b, "```markdown\n%s\n```\n\n", strings.TrimRight(string(data), "\n"))
	} else {
		fmt.Fprintf(&b, "Could not read %s: %v.\n", taskPath, err)
		fmt.Fprintf(&b, "Proceed by inspecting the task path manually or asking the human for the missing task body.\n\n")
	}

	fmt.Fprintf(&b, "Task set context:\n")
	for _, task := range m.Tasks {
		fmt.Fprintf(&b, "- %s [%s %s]", task.ID, task.Type, task.Status)
		if task.Title != "" {
			fmt.Fprintf(&b, " %s", task.Title)
		}
		fmt.Fprintf(&b, " (%s)", filepath.Join(m.Dir, task.File))
		if len(task.BlockedBy) > 0 {
			fmt.Fprintf(&b, "; blocked_by: %s", strings.Join(task.BlockedBy, ", "))
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Help the human get this task to a passing state. Do not mark the task done or reset it yourself unless the human explicitly chooses that outcome.\n")
	return b.String()
}

// BuildInterruptAssistancePrompt generates the attended-agent prompt shown when a
// live AFK attempt is interrupted (SIGINT) and the drain lands on the interrupt
// gate (ADR-0119). The agent is loaded with the interrupted task and surrounding
// Task set context to advise or edit by hand; it deliberately mirrors the HITL
// assistance contract — it must not mutate task state or resume the drain, since
// the human resolves the interrupt from the gate menu (Continue / Exit).
func BuildInterruptAssistancePrompt(d *Deps, taskSetID string, m *Manifest, interrupted Task, runtimePath string) string {
	if d == nil {
		d = defaultDeps
	}
	if d.FS == nil {
		d.FS = DefaultDeps().FS
	}

	taskPath := filepath.Join(m.Dir, interrupted.File)
	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human with an interrupted task in a Pop task set.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", taskSetID)
	fmt.Fprintf(&b, "Task set path: %s\n", m.Dir)
	fmt.Fprintf(&b, "Interrupted task: %s", interrupted.ID)
	if interrupted.Title != "" {
		fmt.Fprintf(&b, " - %s", interrupted.Title)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Task path: %s\n", taskPath)
	if runtimePath != "" {
		fmt.Fprintf(&b, "Runtime checkout: %s\n", runtimePath)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "This task's live attempt was stopped mid-run by an interrupt (SIGINT). The\n")
	fmt.Fprintf(&b, "human is deciding at the interrupt gate whether to continue draining (re-run\n")
	fmt.Fprintf(&b, "this task) or exit. You are here to advise and edit by hand only:\n")
	fmt.Fprintf(&b, "- Do not change task state yourself and do not resume the drain; the human\n")
	fmt.Fprintf(&b, "  chooses Continue or Exit from the gate menu after you exit.\n")
	fmt.Fprintf(&b, "- exit without changing task state: leave the interrupted task open and make no manual override.\n\n")

	fmt.Fprintf(&b, "Full interrupted task body:\n")
	if data, err := d.FS.ReadFile(taskPath); err == nil {
		fmt.Fprintf(&b, "```markdown\n%s\n```\n\n", strings.TrimRight(string(data), "\n"))
	} else {
		fmt.Fprintf(&b, "Could not read %s: %v.\n", taskPath, err)
		fmt.Fprintf(&b, "Proceed by inspecting the task path manually or asking the human for the missing task body.\n\n")
	}

	fmt.Fprintf(&b, "Task set context:\n")
	for _, task := range m.Tasks {
		fmt.Fprintf(&b, "- %s [%s %s]", task.ID, task.Type, task.Status)
		if task.Title != "" {
			fmt.Fprintf(&b, " %s", task.Title)
		}
		fmt.Fprintf(&b, " (%s)", filepath.Join(m.Dir, task.File))
		if len(task.BlockedBy) > 0 {
			fmt.Fprintf(&b, "; blocked_by: %s", strings.Join(task.BlockedBy, ", "))
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Use the repository and task context to help the human decide whether to continue draining this task or exit. Do not mark tasks complete, skipped, or reset unless the human explicitly chooses that outcome.\n")
	return b.String()
}

// formatSiblingCompletedBriefs renders the inter-task feed appended to the
// worker prompt on a retry: briefs of sibling tasks already completed in the
// same Task set, for cross-task orientation — what already landed, so the
// worker knows where to look (ADR 0040). It draws from the same completed-AFK
// join the HITL assistance prompt uses (done manifest status + DONE/COMPLETE
// outcome, deduped to the latest record per task), so sibling failure/reset
// churn never reaches the worker. Returns "" when no sibling has a brief.
func formatSiblingCompletedBriefs(d *Deps, m *Manifest) string {
	completed := completedAFKProgress(d, m)
	if len(completed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("Sibling tasks already completed in this task set (for orientation — what\n")
	b.WriteString("already landed, so you know where to look). These are done siblings, not\n")
	b.WriteString("this task:\n\n")
	for _, item := range completed {
		fmt.Fprintf(&b, "%s — %s\n", item.TaskID, item.Outcome)
		for _, line := range strings.Split(item.Summary, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

type completedAFKProgressItem struct {
	TaskID    string
	File      string
	Outcome   string
	Timestamp string
	Summary   string
}

func completedAFKProgress(d *Deps, m *Manifest) []completedAFKProgressItem {
	progressPath := filepath.Join(m.Dir, "progress.txt")
	data, err := d.FS.ReadFile(progressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}

	tasksByFile := make(map[string]Task, len(m.Tasks))
	for _, task := range m.Tasks {
		tasksByFile[task.File] = task
	}

	// Dedupe to the latest record per task: a done→reset→done task yields two
	// DONE records, and only the current one is a live brief — the earlier one
	// describes the abandoned line of attack (ADR 0040). The State gate already
	// drops a done→reset→failed task (its manifest status is not "done").
	var order []string
	latest := make(map[string]completedAFKProgressItem)
	for _, record := range parseProgressRecords(string(data)) {
		task, ok := tasksByFile[record.File]
		if !ok || task.Type != "AFK" || task.Status != TaskDone {
			continue
		}
		if record.Outcome != "DONE" && record.Outcome != "COMPLETE" {
			continue
		}
		item := completedAFKProgressItem{
			TaskID:    task.ID,
			File:      record.File,
			Outcome:   record.Outcome,
			Timestamp: record.Timestamp,
			Summary:   record.Summary,
		}
		prev, seen := latest[record.File]
		if !seen {
			order = append(order, record.File)
		} else if !recordAfter(record.Timestamp, prev.Timestamp) {
			continue
		}
		latest[record.File] = item
	}

	completed := make([]completedAFKProgressItem, 0, len(order))
	for _, file := range order {
		completed = append(completed, latest[file])
	}
	return completed
}

// recordAfter reports whether progress record timestamp a is at or after b.
// Both are RFC3339; on a parse failure it falls back to true so the later
// (append-only) record wins, matching progress.txt's chronological order.
func recordAfter(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return true
	}
	return !ta.Before(tb)
}

type progressRecord struct {
	Timestamp string
	File      string
	Outcome   string
	Summary   string
}

func parseProgressRecords(data string) []progressRecord {
	var records []progressRecord
	for _, block := range strings.Split(data, "\n---\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		if len(lines) == 0 {
			continue
		}
		matches := progressHeaderPattern.FindStringSubmatch(strings.TrimSpace(lines[0]))
		if matches == nil {
			continue
		}
		records = append(records, progressRecord{
			Timestamp: matches[1],
			File:      matches[2],
			Outcome:   matches[3],
			Summary:   strings.TrimSpace(strings.Join(lines[1:], "\n")),
		})
	}
	return records
}
