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
	// EffortClause is the badge the Assist listing carries inside the brackets
	// and the gates' listings do not. Carrying it as row data keeps the shared
	// partial prose: it ranges and interpolates, it never asks which caller
	// built the row.
	EffortClause string
}

// gateTaskRows builds the listing the HITL, Failed and interrupt gates show.
// It lists every task in the manifest, open and HITL included: the assisting
// agent needs the whole set to advise the human. The done-AFK filter belongs to
// the Verifier's listing alone (ADR-0102), and must never migrate in here.
func gateTaskRows(m *Manifest) []taskRow {
	return manifestTaskRows(m, false)
}

// assistTaskRows builds the same listing for the Assist session, which shows
// each task's effort so the human can re-effort from the session.
func assistTaskRows(m *Manifest) []taskRow {
	return manifestTaskRows(m, true)
}

func manifestTaskRows(m *Manifest, withEffort bool) []taskRow {
	rows := make([]taskRow, 0, len(m.Tasks))
	for _, task := range m.Tasks {
		row := taskRow{
			ID:     task.ID,
			Type:   task.Type,
			Status: task.Status,
			Path:   filepath.Join(m.Dir, task.File),
		}
		if withEffort {
			row.EffortClause = " effort=" + task.Effort
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
//
// implementationConvention is the resolved `implementation` convention prose,
// already free of the Read-whole notice, or empty when the repository has not
// asked for it ([work.implement].include_implementation_convention, ADR-0246).
// Empty renders the prompt exactly as it read before the toggle existed;
// non-empty adds one labelled block, so a planned task and a Remediation task —
// which drain through this same prompt — are held to the standard upfront.
//
// The completion sentinels the template names (SUMMARY_START, SUMMARY_END,
// TASK_COMPLETE, TASK_FAILED) stay literal text there. They are also compiled
// into the assessor's regexes and written again in the retry lessons; folding
// the three sites onto a shared constant is its own change (ADR-0208).
func BuildAgentPrompt(taskPath, runtimePath, implementationConvention string) string {
	tasksDir := filepath.Dir(taskPath)
	view := agentPromptView{
		TaskPath:     taskPath,
		TasksDir:     tasksDir,
		ManifestPath: filepath.Join(tasksDir, ManifestFileName),
		RuntimePath:  runtimePath,
	}
	if convention := strings.TrimSpace(implementationConvention); convention != "" {
		view.ImplementationConventionRecorded = true
		view.ImplementationConvention = convention
	}
	return prompt.MustRender(promptTemplates, "agent.tmpl.md", view)
}

// agentPromptView is what the AFK worker's template renders against: a long
// instruction document with four paths in it and one conditional section, the
// repository's implementation convention.
type agentPromptView struct {
	TaskPath     string
	TasksDir     string
	ManifestPath string
	RuntimePath  string
	// ImplementationConventionRecorded guards the convention block the way every other
	// prompt view guards an optional section: a named boolean beside the text,
	// so the template never asks why a field is empty.
	ImplementationConventionRecorded bool
	ImplementationConvention         string
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
		Refine:              refineBlock(d, m, runtimePath),
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
	Refine              refineBlockView
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
		Refine:              refineBlock(d, m, runtimePath),
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
	Refine                refineBlockView
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

	// The diff bodies stay out of the prompt for the same reason the Verifier's
	// do (see workDiffView): the assisting agent is in the checkout and can read
	// what the human asks about, while an inlined diff of a large set overflows
	// both argv and the context window. The view carries the range and stat the
	// heading announces, never a body.
	work := verifyWorkDiff(d, runtimePath, taskSetID, m)
	view := verifyFailedPromptView{
		TaskSetID:           taskSetID,
		TaskSetPath:         m.Dir,
		WorkSHALine:         optionalLine("Work SHA: ", workSHA),
		RuntimeCheckoutLine: runtimeCheckoutLine(runtimePath),
		WorkRange:           work.Range,
		WorkStat:            work.Stat,
		WorkUndetermined:    work.Undetermined,
		Tasks:               gateTaskRows(m),
		Refine:              refineBlock(d, m, runtimePath),
	}
	if workSHA != "" {
		view.WorkSHAClause = " (at " + workSHA + ")"
	}
	if trimmed := strings.TrimSpace(findings); trimmed != "" {
		view.FindingsRecorded = true
		view.Findings = trimmed
	} else {
		view.FindingsMissing = true
	}
	if !work.Undetermined {
		view.WorkEmpty = work.Empty()
		view.WorkPresent = !work.Empty()
	}
	return prompt.MustRender(promptTemplates, "verify-failed-assistance.tmpl.md", view)
}

// verifyFailedPromptView is what the Verify-fail gate's template renders
// against. The findings and work-diff states are named booleans so the template
// picks a whole section rather than deriving which of the three diff cases holds.
type verifyFailedPromptView struct {
	TaskSetID           string
	TaskSetPath         string
	WorkSHALine         string
	RuntimeCheckoutLine string
	FindingsRecorded    bool
	FindingsMissing     bool
	Findings            string
	// WorkSHAClause is the parenthetical the work-diff heading carries when the
	// gate knows the SHA under judgment.
	WorkSHAClause    string
	WorkUndetermined bool
	WorkEmpty        bool
	WorkPresent      bool
	WorkRange        string
	WorkStat         string
	Tasks            []taskRow
	Refine           refineBlockView
}

// optionalLine renders prefix+value, or nothing when the value is absent. An
// empty line closes up in the renderer's normalizer, so the template names the
// line rather than guarding it.
func optionalLine(prefix, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
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
		Refine:              refineBlock(d, m, runtimePath),
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
	Refine              refineBlockView
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

	view := assistPromptView{
		TaskSetID:   taskSetID,
		TaskSetPath: m.Dir,
		Status:      string(status),
		BindingLine: optionalLine("Worktree binding / Runtime path (Binding-first): ", runtimePath),
		Tasks:       assistTaskRows(m),
	}
	if trimmed := strings.TrimSpace(findings); trimmed != "" {
		view.FindingsRecorded = true
		view.Findings = trimmed
	}
	// The load-bearing surface of ADR-0240: named as a path, like every task
	// body, so "read the report and let's work out what to do about it" needs no
	// plumbing beyond the agent opening the file.
	view.Refine = refineBlock(d, m, runtimePath)
	view.Progress, view.HasProgress, view.ProgressEmpty, view.ProgressUnavailable = recentProgressRows(d, m)
	return prompt.MustRender(promptTemplates, "assist.tmpl.md", view)
}

// assistPromptView is what the Assist session's template renders against. The
// three progress states are named booleans so the template picks a whole
// section: the recent records, the empty file, or no file at all.
type assistPromptView struct {
	TaskSetID           string
	TaskSetPath         string
	Status              string
	BindingLine         string
	Tasks               []taskRow
	FindingsRecorded    bool
	Findings            string
	Refine              refineBlockView
	Progress            []progressRow
	HasProgress         bool
	ProgressEmpty       bool
	ProgressUnavailable bool
}

// progressRow is one recent progress record, its summary already split into the
// non-blank lines the prompt indents.
type progressRow struct {
	Timestamp    string
	File         string
	Outcome      string
	SummaryLines []string
}

// recentProgressRowLimit is how much of the tail of progress.txt the Assist
// session shows: enough for the human to see how the set got here, short of
// replaying a long drain.
const recentProgressRowLimit = 8

// recentProgressRows reads progress.txt once and returns the tail of its
// records with the state the template selects on.
func recentProgressRows(d *Deps, m *Manifest) (rows []progressRow, has, empty, unavailable bool) {
	data, err := d.FS.ReadFile(filepath.Join(m.Dir, "progress.txt"))
	if err != nil {
		return nil, false, false, true
	}
	records := parseProgressRecords(string(data))
	if len(records) == 0 {
		return nil, false, true, false
	}
	start := 0
	if len(records) > recentProgressRowLimit {
		start = len(records) - recentProgressRowLimit
	}
	for _, rec := range records[start:] {
		rows = append(rows, progressRow{
			Timestamp:    rec.Timestamp,
			File:         rec.File,
			Outcome:      rec.Outcome,
			SummaryLines: nonBlankLines(rec.Summary),
		})
	}
	return rows, true, false, false
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
