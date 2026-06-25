package tasks

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// RefreshResult is the outcome of a task status refresh.
type RefreshResult struct {
	DefinitionPath   string
	NewRegistrations []string
	Rows             []Row
	Manifests        map[string]*Manifest
	NeedsSave        bool
	RuntimeLock      *RuntimeLockStatus
	Checkout         *CheckoutStatus
	ArchivedCount    int
	ShowArchived     bool
}

// CheckoutStatus describes where a whole-set implement started here would run.
// Worktree is true for a linked git worktree (implement adopts it; the set is
// integrateable). A non-worktree checkout is the Trunk worktree and drains
// inline by default.
type CheckoutStatus struct {
	Path     string
	Worktree bool
	Branch   string
}

// Refresh discovers Task sets, auto-registers them, and builds table rows.
func Refresh(defPath string) (*RefreshResult, error) {
	return RefreshWith(defaultDeps, defPath, StatePathFor(defPath))
}

// RefreshWith performs refresh using injected dependencies and state path.
func RefreshWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, false)
}

// RefreshArchivedWith performs refresh and returns only Archived Task sets.
func RefreshArchivedWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, true)
}

func refreshWith(d *Deps, defPath, statePath string, showArchived bool) (*RefreshResult, error) {
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return nil, err
	}

	if _, err := MigrateStorageLayout(d, canon); err != nil {
		return nil, err
	}
	// Migration may have created the tasks directory for the first time, so
	// re-canonicalize: a path that did not exist resolves symlinks once it does,
	// and the state key must match what migration wrote and future calls resolve.
	canon, err = CanonicalDefinitionPathWith(d, canon)
	if err != nil {
		return nil, err
	}

	disc, err := DiscoverWith(d, canon)
	if err != nil {
		return nil, err
	}
	if disc.TaskDirErr != nil {
		return nil, disc.TaskDirErr
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	registered := state.RegisteredIDs(canon)
	needsSave := false
	for id := range disc.Manifests {
		if _, ok := registered[id]; !ok {
			needsSave = true
			break
		}
	}

	var newRegs []string
	if needsSave {
		err := UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
			mergeNewRegistrations(d, canon, disc, state, &newRegs)
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(newRegs)
		state, err = LoadGlobalStateWith(d, statePath)
		if err != nil {
			return nil, err
		}
	}

	manifests := make(map[string]*Manifest)
	for stem, manifestPath := range disc.Manifests {
		manifests[stem] = LoadManifest(d, stem, manifestPath)
	}

	result := &RefreshResult{
		DefinitionPath:   canon,
		NewRegistrations: newRegs,
		Manifests:        manifests,
		NeedsSave:        needsSave,
		ArchivedCount:    archivedCount(state, canon),
		ShowArchived:     showArchived,
	}
	result.Rows = buildRows(state, canon, disc, manifests, showArchived)
	MarkNextPick(result.Rows)
	return result, nil
}

func archivedCount(state *GlobalState, defPath string) int {
	entry := state.Tasks[defPath]
	if entry == nil {
		return 0
	}
	count := 0
	for _, reg := range entry.TaskSets {
		if reg.Archived {
			count++
		}
	}
	return count
}

func buildRows(state *GlobalState, defPath string, disc *Discovery, manifests map[string]*Manifest, showArchived bool) []Row {
	var missing, done, active []Row

	entry := state.Tasks[defPath]

	if entry != nil {
		for i, reg := range entry.TaskSets {
			if reg.Archived != showArchived {
				continue
			}
			if _, ok := disc.Manifests[reg.ID]; !ok {
				missing = append(missing, Row{
					ID:           reg.ID,
					Status:       StatusMissing,
					Priority:     reg.Priority,
					PriorityShow: fmt.Sprintf("%d", reg.Priority),
					AutoDrain:    reg.AutoDrain,
					RegIndex:     i,
				})
				continue
			}
			row := buildTaskSetRow(reg, manifests[reg.ID], i)
			switch row.Status {
			case StatusDone:
				done = append(done, row)
			default:
				active = append(active, row)
			}
		}
	}

	sort.SliceStable(done, func(i, j int) bool {
		return done[i].RegIndex < done[j].RegIndex
	})

	sort.SliceStable(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority > active[j].Priority
		}
		return active[i].RegIndex < active[j].RegIndex
	})

	rows := append([]Row{}, missing...)
	rows = append(rows, done...)
	rows = append(rows, active...)
	return rows
}

func buildTaskSetRow(reg RegisteredTaskSet, m *Manifest, regIndex int) Row {
	status := DeriveStatus(m)
	row := Row{
		ID:           reg.ID,
		Status:       status,
		Priority:     reg.Priority,
		PriorityShow: fmt.Sprintf("%d", reg.Priority),
		AutoDrain:    reg.AutoDrain,
		RegIndex:     regIndex,
	}

	row.Progress = BuildProgress(m, status)
	if status == StatusBlocked || status == StatusUnverified {
		row.BlockedReason = BuildBlockedReason(m)
		if hitl := BlockingHITLTask(m); hitl != nil {
			row.CompleteHint = completeTaskHint(reg.ID, hitl.File)
		}
	}
	if status == StatusFailed {
		row.FailedTasks, row.ResetHints = BuildFailedInfo(reg.ID, m)
	}
	if status == StatusMalformed {
		row.MalformedSummary = MalformedSummary(m)
		if m != nil {
			row.DetailErrors = append([]string{}, m.Errors...)
		}
	}
	return row
}

// Render writes the status table and diagnostics to w.
func Render(w io.Writer, result *RefreshResult) {
	render(outputFor(w), result)
}

func render(out *output, result *RefreshResult) {
	if len(result.NewRegistrations) > 0 {
		out.line(ansiCyan, "Registered new task set(s): %s", strings.Join(result.NewRegistrations, ", "))
		fmt.Fprintln(out)
	}

	if len(result.Rows) == 0 {
		fmt.Fprintf(out, "No task sets at %s\n", result.DefinitionPath)
		renderArchivedFooter(out, result)
		return
	}

	fmt.Fprintln(out, formatTableWithOutput(out, result.Rows))
	renderCheckout(out, result.Checkout)
	renderRuntimeLock(out, result.RuntimeLock)
	renderDiagnostics(out, result.Rows)
	renderArchivedFooter(out, result)
}

// renderCheckout reports where a whole-set `pop tasks implement` started here
// would drain by default. Single task-file runs remain current-checkout
// operations.
func renderCheckout(out *output, cs *CheckoutStatus) {
	if cs == nil {
		return
	}
	fmt.Fprintln(out)
	if cs.Worktree {
		where := "worktree"
		if cs.Branch != "" {
			where = fmt.Sprintf("worktree (%s)", cs.Branch)
		} else {
			where = "worktree (detached)"
		}
		out.line(ansiCyan, "Checkout: %s — implement adopts it (integrateable)", where)
		return
	}
	out.line(ansiDim, "Checkout: Trunk worktree — whole-set implement drains inline")
}

func renderArchivedFooter(out *output, result *RefreshResult) {
	if result == nil || result.ShowArchived || result.ArchivedCount == 0 {
		return
	}
	fmt.Fprintln(out)
	label := "Archived Task set"
	if result.ArchivedCount != 1 {
		label = "Archived Task sets"
	}
	out.line(ansiDim, "%d %s hidden. Run `pop tasks status --archived` to view.", result.ArchivedCount, label)
}

func formatTable(rows []Row) string {
	return formatTableWithOutput(outputFor(io.Discard), rows)
}

func formatTableWithOutput(out *output, rows []Row) string {
	const (
		idW     = 28
		stW     = 10
		prW     = 5
		detailW = 96
	)

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, "TASK SET", stW, "STATUS", prW, "PRI", "DETAILS")
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, strings.Repeat("-", idW), stW, strings.Repeat("-", stW), prW, strings.Repeat("-", prW), strings.Repeat("-", detailW))

	for _, row := range rows {
		detail := rowDetail(row)
		if len(detail) > detailW {
			detail = detail[:detailW-3] + "..."
		}
		id := row.ID
		if row.RunTarget {
			id = "▶ " + id
		}
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %s", idW, id, stW, string(row.Status), prW, row.PriorityShow, detail)
		if row.RunTarget {
			line = out.styled(ansiBold+ansiCyan, line)
		} else {
			line = out.styled(statusStyle(row.Status), line)
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

func rowDetail(row Row) string {
	if row.ConfigError != "" {
		base := rowStatusDetail(row)
		if base == "" {
			return "config error: " + row.ConfigError
		}
		return base + " — config error: " + row.ConfigError
	}
	return rowStatusDetail(row)
}

func rowStatusDetail(row Row) string {
	switch row.Status {
	case StatusMissing:
		return "registered task set missing"
	case StatusMalformed:
		return row.MalformedSummary
	case StatusFailed:
		parts := []string{row.Progress}
		if len(row.FailedTasks) > 0 {
			parts = append(parts, "failed: "+strings.Join(row.FailedTasks, ", "))
		}
		if len(row.ResetHints) > 0 {
			parts = append(parts, "reset: "+row.ResetHints[0])
		}
		return strings.Join(parts, " — ")
	case StatusBlocked, StatusUnverified:
		parts := []string{row.Progress}
		if row.BlockedReason != "" {
			parts = append(parts, row.BlockedReason)
		}
		if row.CompleteHint != "" {
			parts = append(parts, "complete: "+row.CompleteHint)
		}
		return strings.Join(parts, " — ")
	default:
		return row.Progress
	}
}

func renderRuntimeLock(w io.Writer, lock *RuntimeLockStatus) {
	if lock == nil || lock.RuntimePath == "" {
		return
	}
	out := outputFor(w)
	if styled, ok := w.(*output); ok {
		out = styled
	}
	fmt.Fprintln(out)
	switch {
	case lock.Malformed:
		out.line(ansiYellow, "Runtime execution lock (best effort): malformed lock file for %s", lock.RuntimePath)
	case lock.Metadata != nil && lock.Locked:
		out.line(ansiYellow, "Runtime execution lock (best effort): PID %d since %s at %s",
			lock.Metadata.PID,
			lock.Metadata.StartedAt.Format(time.RFC3339),
			lock.Metadata.RuntimePath,
		)
	default:
		out.line(ansiDim, "Runtime execution lock (best effort): idle at %s", lock.RuntimePath)
	}
}

func renderDiagnostics(w io.Writer, rows []Row) {
	var detailRows []Row
	var configErrorRows []Row
	for _, row := range rows {
		if row.Status == StatusMalformed && len(row.DetailErrors) > 0 {
			detailRows = append(detailRows, row)
		}
		if row.ConfigError != "" {
			configErrorRows = append(configErrorRows, row)
		}
	}
	if len(detailRows) == 0 && len(configErrorRows) == 0 {
		return
	}

	out := outputFor(w)
	if styled, ok := w.(*output); ok {
		out = styled
	}
	if len(detailRows) > 0 {
		fmt.Fprintln(out)
		out.line(ansiRed, "Malformed diagnostics:")
		for _, row := range detailRows {
			fmt.Fprintf(out, "  %s:\n", row.ID)
			for _, err := range row.DetailErrors {
				fmt.Fprintf(out, "    - %s\n", err)
			}
		}
	}
	if len(configErrorRows) > 0 {
		fmt.Fprintln(out)
		out.line(ansiRed, "Config errors:")
		for _, row := range configErrorRows {
			fmt.Fprintf(out, "  %s:\n", row.ID)
			fmt.Fprintf(out, "    - %s\n", row.ConfigError)
		}
	}
}

// RenderTaskList writes the tasks in one Task set before confirmation.
func RenderTaskList(w io.Writer, taskSetID string, m *Manifest) {
	renderTaskList(outputFor(w), taskSetID, m)
}

func renderTaskList(out *output, taskSetID string, m *Manifest) {
	if m == nil || !m.Valid {
		return
	}
	fmt.Fprintf(out, "\nTasks in %s:\n", taskSetID)
	fmt.Fprintln(out, "  Legend: → runnable  ○ blocked  ◐ needs human  ✓ done  ⊘ failed/skipped")
	for _, task := range m.Tasks {
		sym := taskSymbol(m, task)
		fmt.Fprintf(out, "  %s\n", out.styled(taskStyle(m, task), fmt.Sprintf("%s %s  %s  %s  %s", sym, task.ID, task.Type, task.Status, task.Title)))
	}
	fmt.Fprintln(out)
}

// RenderTaskSetDetail writes a per-task breakdown of one task set — the arg
// form of `pop tasks status`. Where the no-arg overview shows one row per set,
// this drills into a single set and shows every task's status, type, identifier,
// title, and blockers in manifest (dependency) order, so a set's aggregate
// state can be read down to the task that holds it.
func RenderTaskSetDetail(w io.Writer, taskSetID string, row *Row, m *Manifest) {
	renderTaskSetDetail(outputFor(w), taskSetID, row, m)
}

func renderTaskSetDetail(out *output, taskSetID string, row *Row, m *Manifest) {
	status := DeriveStatus(m)
	progress := ""
	if row != nil {
		status = row.Status
		progress = row.Progress
	}

	header := fmt.Sprintf("%s  [%s]", taskSetID, status)
	if progress != "" {
		header += "  " + progress
	}
	out.line(statusStyle(status), "%s", header)

	if status == StatusMissing {
		out.line(ansiYellow, "registered task set missing")
		return
	}
	if m == nil || !m.Valid {
		out.line(ansiRed, "malformed manifest")
		if m != nil {
			for _, e := range m.Errors {
				fmt.Fprintf(out, "  - %s\n", e)
			}
		}
		return
	}

	const (
		stW    = 10
		tyW    = 4
		titleW = 40
	)
	idW := len("ID")
	for _, task := range m.Tasks {
		if len(task.ID) > idW {
			idW = len(task.ID)
		}
	}
	fmt.Fprintf(out, "%-*s  %-*s  %-*s  %-*s  %s\n", stW, "STATUS", tyW, "TYPE", idW, "ID", titleW, "TITLE", "BLOCKED-BY")
	fmt.Fprintf(out, "%-*s  %-*s  %-*s  %-*s  %s\n",
		stW, strings.Repeat("-", stW), tyW, strings.Repeat("-", tyW), idW, strings.Repeat("-", idW), titleW, strings.Repeat("-", titleW), strings.Repeat("-", 12))

	for _, task := range m.Tasks {
		title := task.Title
		if len(title) > titleW {
			title = title[:titleW-3] + "..."
		}
		blockedBy := "-"
		if len(task.BlockedBy) > 0 {
			blockedBy = strings.Join(task.BlockedBy, ", ")
		}
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s", stW, taskStatusCell(task), tyW, task.Type, idW, task.ID, titleW, title, blockedBy)
		fmt.Fprintln(out, out.styled(taskStyle(m, task), line))
	}
}

// taskStatusCell renders one task's status for the detail table, folding the
// retry count into a failed task's cell (failed(N)) so the table needs no
// separate column for it.
func taskStatusCell(task Task) string {
	if task.Status == "failed" && task.FailedAfter != nil {
		return fmt.Sprintf("failed(%d)", *task.FailedAfter)
	}
	return task.Status
}

func taskStyle(m *Manifest, task Task) string {
	switch task.Status {
	case "done":
		return ansiGreen
	case "failed":
		return ansiRed
	case "skipped":
		return ansiYellow
	}
	if task.Type == "HITL" || !isEligible(m, task) {
		return ansiYellow
	}
	return ansiCyan
}

func taskSymbol(m *Manifest, task Task) string {
	switch task.Status {
	case "done":
		return "✓"
	case "failed", "skipped":
		return "⊘"
	case "open":
		if task.Type == "HITL" && blockersResolved(m, task) {
			return "◐"
		}
		if isEligible(m, task) {
			return "→"
		}
		return "○"
	default:
		return "○"
	}
}
