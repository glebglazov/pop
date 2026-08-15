package tasks

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/glebglazov/pop/internal/prompt"
)

var progressHeaderPattern = regexp.MustCompile(`^(\S+)\s+\[([^\]]+)\]\s+(\S+)\s*$`)

// The Task-set agent prompts live beside the code that owns them, as markdown a
// human can read and edit without touching Go (ADR-0208). Parsing at init means
// a malformed template fails the first test run rather than a live gate.
//
//go:embed prompts/*.tmpl.md
var promptTemplateFS embed.FS

var promptTemplates = prompt.MustParseFS(promptTemplateFS, "prompts/*.tmpl.md")

// taskRow is one line of the manifest listing every gate prompt carries. The
// path is already joined and the blockers already joined into their clause, so
// the "task-listing" partial only ranges — it holds no per-field conditional and
// no formatting decision.
type taskRow struct {
	ID     string
	Type   string
	Status TaskStatus
	Path   string
	// TitleClause and BlockedByClause are the rendered optional fragments,
	// empty when the task has no title or no blockers.
	TitleClause     string
	BlockedByClause string
}

// gateTaskRows builds the listing the HITL, Failed and interrupt gates show.
// It lists every task in the manifest, open and HITL included: the assisting
// agent needs the whole set to advise the human. The done-AFK filter belongs to
// the Verifier's listing alone (ADR-0102), and must never migrate in here.
func gateTaskRows(m *Manifest) []taskRow {
	rows := make([]taskRow, 0, len(m.Tasks))
	for _, task := range m.Tasks {
		row := taskRow{
			ID:     task.ID,
			Type:   task.Type,
			Status: task.Status,
			Path:   filepath.Join(m.Dir, task.File),
		}
		if task.Title != "" {
			row.TitleClause = " " + task.Title
		}
		if len(task.BlockedBy) > 0 {
			row.BlockedByClause = "; blocked_by: " + strings.Join(task.BlockedBy, ", ")
		}
		rows = append(rows, row)
	}
	return rows
}

// taskBodyRow carries the inlined task body each gate fences into its prompt,
// or the read failure that stands in for it. Readable and Unreadable are named
// so the partial picks a whole section rather than negating a field inline.
type taskBodyRow struct {
	Readable   bool
	Unreadable bool
	Path       string
	Body       string
	Error      string
}

func readTaskBody(d *Deps, path string) taskBodyRow {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return taskBodyRow{Unreadable: true, Path: path, Error: fmt.Sprintf("%v", err)}
	}
	return taskBodyRow{Readable: true, Path: path, Body: strings.TrimRight(string(data), "\n")}
}

// runtimeCheckoutLine renders the optional runtime-path line, empty when the
// gate has no binding. An empty line closes up in the renderer's normalizer, so
// the template names the line rather than guarding it.
func runtimeCheckoutLine(runtimePath string) string {
	if runtimePath == "" {
		return ""
	}
	return "Runtime checkout: " + runtimePath
}

// taskHeading joins a task's ID with its optional title the way every gate
// header states it.
func taskHeading(task Task) string {
	if task.Title == "" {
		return task.ID
	}
	return task.ID + " - " + task.Title
}

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
		filepath.Join(tasksDir, ManifestFileName), tasksDir)
	fmt.Fprintf(&b, "Do NOT make git commits — the runner handles assessment and committing.\n\n")
	fmt.Fprintf(&b, "Runtime checkout: %s\n\n", runtimePath)
	fmt.Fprintf(&b, "Implementation edits belong only beneath the runtime checkout. The task file\n")
	fmt.Fprintf(&b, "above is the one file you also edit — its acceptance boxes are yours to tick.\n\n")
	fmt.Fprintf(&b, "This attempt is a single non-interactive session. There is no human and no\n")
	fmt.Fprintf(&b, "later turn: once you end your response the attempt is over, and ending\n")
	fmt.Fprintf(&b, "without a completion sentinel (TASK_COMPLETE or TASK_FAILED) is recorded as a\n")
	fmt.Fprintf(&b, "failure. To wait on a long-running command, keep polling it across successive\n")
	fmt.Fprintf(&b, "bash calls until it finishes (or fails) — never background the work and end\n")
	fmt.Fprintf(&b, "your turn to \"wait\", which orphans it and yields no sentinel. A single bash\n")
	fmt.Fprintf(&b, "call may be killed at its own tool timeout (~10 min), but the whole attempt\n")
	fmt.Fprintf(&b, "has a far longer timeout (~1 hour), so poll across calls rather than waiting\n")
	fmt.Fprintf(&b, "within one.\n\n")
	fmt.Fprintf(&b, "Your context is billed on every turn and only grows within the attempt, so\n")
	fmt.Fprintf(&b, "the attempt's cost rises with the square of how many tool calls you make.\n")
	fmt.Fprintf(&b, "Probe wide once rather than laddering narrowing greps; read the ranges of a\n")
	fmt.Fprintf(&b, "file you need instead of whole large files; never re-run a command or re-read\n")
	fmt.Fprintf(&b, "a file whose output is already in this session; chain setup and command in one\n")
	fmt.Fprintf(&b, "shell call instead of repeating cd or env lines. Images are never evicted —\n")
	fmt.Fprintf(&b, "read one only when visual judgement is the question.\n\n")
	fmt.Fprintf(&b, "When you have completed the work, close out in this order:\n\n")
	fmt.Fprintf(&b, "1. Re-read the task file and tick every box under \"Acceptance criteria\" that\n")
	fmt.Fprintf(&b, "   you have satisfied (`- [ ]` → `- [x]`). An attempt that leaves a box\n")
	fmt.Fprintf(&b, "   unticked is recorded as failed even when the work itself landed.\n")
	fmt.Fprintf(&b, "2. Print a summary block followed by the completion sentinel as the final\n")
	fmt.Fprintf(&b, "   lines of your output, exactly:\n\n")
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

	completed := completedAFKProgressRows(d, m)
	view := hitlPromptView{
		TaskSetID:           taskSetID,
		TaskSetPath:         m.Dir,
		BlockingTask:        taskHeading(blocking),
		TaskPath:            filepath.Join(m.Dir, blocking.File),
		RuntimeCheckoutLine: runtimeCheckoutLine(runtimePath),
		Body:                readTaskBody(d, filepath.Join(m.Dir, blocking.File)),
		Tasks:               gateTaskRows(m),
		CompletedWork:       completed,
		HasCompletedWork:    len(completed) > 0,
		NoCompletedWork:     len(completed) == 0,
	}
	return prompt.MustRender(promptTemplates, "hitl-assistance.tmpl.md", view)
}

// hitlPromptView is what the HITL gate's template renders against.
type hitlPromptView struct {
	TaskSetID           string
	TaskSetPath         string
	BlockingTask        string
	TaskPath            string
	RuntimeCheckoutLine string
	Body                taskBodyRow
	Tasks               []taskRow
	CompletedWork       []completedWorkRow
	HasCompletedWork    bool
	NoCompletedWork     bool
}

// completedWorkRow is one completed-AFK brief, its summary already split into
// the non-blank lines the prompt indents.
type completedWorkRow struct {
	TaskID       string
	File         string
	Outcome      string
	Timestamp    string
	SummaryLines []string
}

func completedAFKProgressRows(d *Deps, m *Manifest) []completedWorkRow {
	items := completedAFKProgress(d, m)
	rows := make([]completedWorkRow, 0, len(items))
	for _, item := range items {
		rows = append(rows, completedWorkRow{
			TaskID:       item.TaskID,
			File:         item.File,
			Outcome:      item.Outcome,
			Timestamp:    item.Timestamp,
			SummaryLines: nonBlankLines(item.Summary),
		})
	}
	return rows
}

func nonBlankLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
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
	view := failedPromptView{
		TaskSetID:           taskSetID,
		TaskSetPath:         m.Dir,
		FailedTask:          taskHeading(failed),
		TaskPath:            taskPath,
		RuntimeCheckoutLine: runtimeCheckoutLine(runtimePath),
		Body:                readTaskBody(d, taskPath),
		Tasks:               gateTaskRows(m),
	}
	if reason, err := LatestFailureReason(d, m.Dir, failed.File); err == nil && reason != "" {
		view.FailureReasonRecorded = true
		view.FailureReason = strings.TrimRight(reason, "\n")
	} else {
		view.FailureReasonMissing = true
	}
	return prompt.MustRender(promptTemplates, "failed-assistance.tmpl.md", view)
}

// failedPromptView is what the Failed gate's template renders against. The two
// failure-reason booleans are named so the template picks a whole section:
// either the recorded reason or the sentence that stands in for it.
type failedPromptView struct {
	TaskSetID             string
	TaskSetPath           string
	FailedTask            string
	TaskPath              string
	RuntimeCheckoutLine   string
	FailureReasonRecorded bool
	FailureReasonMissing  bool
	FailureReason         string
	Body                  taskBodyRow
	Tasks                 []taskRow
}

// BuildVerifyFailedAssistancePrompt generates the attended-agent prompt shown when
// a Task set stops at the Verify-fail gate. The agent reads the recorded Verifier
// findings and the accumulated work diff under judgment so the human can decide
// whether to Accept, Remediate, or exit — it does not disposition the set.
func BuildVerifyFailedAssistancePrompt(d *Deps, taskSetID string, m *Manifest, workSHA, findings, runtimePath string) string {
	if d == nil {
		d = defaultDeps
	}
	if d.FS == nil {
		d.FS = DefaultDeps().FS
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human at a Verify-failed gate for a Pop task set.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", taskSetID)
	fmt.Fprintf(&b, "Task set path: %s\n", m.Dir)
	if workSHA != "" {
		fmt.Fprintf(&b, "Work SHA: %s\n", workSHA)
	}
	if runtimePath != "" {
		fmt.Fprintf(&b, "Runtime checkout: %s\n", runtimePath)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Allowed outcomes at this gate:\n")
	fmt.Fprintf(&b, "- accept: the human records a human-authored PASS verdict with an optional note.\n")
	fmt.Fprintf(&b, "- remediate: the human spawns a Remediation task carrying the findings and an optional note.\n")
	fmt.Fprintf(&b, "- exit without changing task state: leave the set Verify-failed and make no disposition.\n")
	fmt.Fprintf(&b, "Re-running the Verifier is not offered here — it is a separate force action, not a response to findings.\n")
	fmt.Fprintf(&b, "You are advisory only: help the human understand the findings and diff, but do not Accept, Remediate, or change task state yourself.\n\n")

	if trimmed := strings.TrimSpace(findings); trimmed != "" {
		fmt.Fprintf(&b, "Recorded Verifier findings:\n%s\n\n", trimmed)
	} else {
		fmt.Fprintf(&b, "Recorded Verifier findings: none were recorded for this verdict.\n\n")
	}

	// The diff bodies stay out of the prompt for the same reason the Verifier's
	// do (see workDiffView): the assisting agent is in the checkout and can read
	// what the human asks about, while an inlined diff of a large set overflows
	// both argv and the context window.
	work := verifyWorkDiff(d, runtimePath, taskSetID, m)
	fmt.Fprintf(&b, "Accumulated work diff")
	if workSHA != "" {
		fmt.Fprintf(&b, " (at %s)", workSHA)
	}
	fmt.Fprintf(&b, "\n")
	if work.Undetermined {
		fmt.Fprintf(&b, "(the set's commit range could not be determined — helping the human establish what this set actually landed is the task at this gate)\n\n")
	} else if work.Empty() {
		fmt.Fprintf(&b, "(no committed changes for this set)\n\n")
	} else {
		fmt.Fprintf(&b, "Commit range: %s\n", work.Range)
		fmt.Fprintf(&b, "The `git diff --stat` below is complete; fetch any file's diff yourself with `git diff %s -- <path>`.\n", work.Range)
		fmt.Fprintf(&b, "```\n%s\n```\n\n", work.Stat)
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

	fmt.Fprintf(&b, "Help the human decide which allowed outcome fits the findings and diff. Do not record a verdict or spawn remediation unless the human explicitly chooses that outcome.\n")
	return b.String()
}

// BuildInterruptAssistancePrompt generates the attended-agent prompt shown when a
// live AFK attempt is interrupted (SIGINT) and the drain lands on the interrupt
// gate (ADR-0163). The agent is loaded with the interrupted task and surrounding
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
	view := interruptPromptView{
		TaskSetID:           taskSetID,
		TaskSetPath:         m.Dir,
		InterruptedTask:     taskHeading(interrupted),
		TaskPath:            taskPath,
		RuntimeCheckoutLine: runtimeCheckoutLine(runtimePath),
		Body:                readTaskBody(d, taskPath),
		Tasks:               gateTaskRows(m),
	}
	return prompt.MustRender(promptTemplates, "interrupt-assistance.tmpl.md", view)
}

// interruptPromptView is what the interrupt gate's template renders against.
type interruptPromptView struct {
	TaskSetID           string
	TaskSetPath         string
	InterruptedTask     string
	TaskPath            string
	RuntimeCheckoutLine string
	Body                taskBodyRow
	Tasks               []taskRow
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

// BuildAssistPrompt generates the attended-agent prompt for an Assist session's
// agent assistance. It describes the whole set — identity, storage path, derived
// status, manifest listing (status/type/effort/blockers), binding and runtime
// path, recent progress, latest findings, the task contract, and allowed
// operations — without inlining task bodies (the agent reads those from Task
// storage).
func BuildAssistPrompt(d *Deps, taskSetID string, m *Manifest, status TaskSetStatus, runtimePath, findings string) string {
	if d == nil {
		d = defaultDeps
	}
	if d.FS == nil {
		d.FS = DefaultDeps().FS
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human in an Assist session for a Pop task set.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", taskSetID)
	fmt.Fprintf(&b, "Task set path: %s\n", m.Dir)
	fmt.Fprintf(&b, "Derived status: %s\n", status)
	if runtimePath != "" {
		fmt.Fprintf(&b, "Worktree binding / Runtime path (Binding-first): %s\n", runtimePath)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Manifest listing (task bodies are NOT inlined — read them from Task storage):\n")
	for _, task := range m.Tasks {
		fmt.Fprintf(&b, "- %s [%s %s effort=%s]", task.ID, task.Type, task.Status, task.Effort)
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

	if trimmed := strings.TrimSpace(findings); trimmed != "" {
		fmt.Fprintf(&b, "Latest Verify verdict findings:\n%s\n\n", trimmed)
	}

	fmt.Fprintf(&b, "Recent progress:\n")
	progressPath := filepath.Join(m.Dir, "progress.txt")
	if data, err := d.FS.ReadFile(progressPath); err == nil {
		records := parseProgressRecords(string(data))
		if len(records) == 0 {
			fmt.Fprintf(&b, "- (progress.txt is empty)\n\n")
		} else {
			start := 0
			if len(records) > 8 {
				start = len(records) - 8
			}
			for _, rec := range records[start:] {
				fmt.Fprintf(&b, "- %s [%s] %s\n", rec.Timestamp, rec.File, rec.Outcome)
				for _, line := range strings.Split(rec.Summary, "\n") {
					if strings.TrimSpace(line) == "" {
						continue
					}
					fmt.Fprintf(&b, "  %s\n", line)
				}
			}
			fmt.Fprintf(&b, "\n")
		}
	} else {
		fmt.Fprintf(&b, "- No progress.txt is available yet.\n\n")
	}

	fmt.Fprintf(&b, "Task contract to respect:\n")
	fmt.Fprintf(&b, "- Each task file has \"What to build\" and \"## Acceptance criteria\" checkboxes.\n")
	fmt.Fprintf(&b, "- Do not modify index.json's task list shape carelessly; run `pop tasks authoring-guide` for what must stay coherent.\n")
	fmt.Fprintf(&b, "- Do not make git commits — the human owns commits and drain assessment.\n")
	fmt.Fprintf(&b, "- Do not start a Drain and do not run the Verifier.\n\n")

	fmt.Fprintf(&b, "Operations you may perform (by editing Task storage / the checkout):\n")
	fmt.Fprintf(&b, "- Inspect task bodies and the runtime checkout to advise the human.\n")
	fmt.Fprintf(&b, "- Add, remove, reorder, or re-effort tasks by editing index.json and task files under the Task set path.\n")
	fmt.Fprintf(&b, "- Edit implementation under the runtime checkout when the human asks.\n")
	fmt.Fprintf(&b, "- Do not mark tasks complete/skipped/open yourself unless the human explicitly asks; gate dispositions stay human choices.\n")
	fmt.Fprintf(&b, "- Do not invoke `pop tasks implement` or `pop tasks verify` (those start a Drain or the Verifier).\n")
	return b.String()
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
