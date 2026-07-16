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
	// NewRegistrationIDs holds the raw identifiers of the sets this register
	// activated (NewRegistrations carries their display form). The register
	// command eager-binds the current checkout to each (ADR-0115); it is empty on
	// pure reads and re-registers, so re-register never rebinds.
	NewRegistrationIDs []string
	Rows               []Row
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

// Refresh discovers Task sets and builds table rows. It is a pure read: it
// never registers discovered sets (ADR-0061). Use RegisterWith to register.
func Refresh(defPath string) (*RefreshResult, error) {
	return RefreshWith(defaultDeps, defPath, StatePathFor(defPath))
}

// RefreshWith performs a pure-read refresh using injected dependencies and
// state path. It never writes Task state — discovered-but-unregistered sets
// are inert (ADR-0061).
func RefreshWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, false, false)
}

// RefreshArchivedWith performs a pure-read refresh and returns only Archived
// Task sets. Like RefreshWith, it never registers.
func RefreshArchivedWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, false, true)
}

// RegisterWith is the sole writer of Task-set registration (ADR-0061): it
// discovers on-disk sets, registers the new ones (assigning order, seeding
// auto_drain per ADR-0047 and the worktree directive per ADR-0059), and
// returns the resulting status. It must be run from inside the repo so its
// cwd is a valid checkout.
func RegisterWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, true, false)
}

func refreshWith(d *Deps, defPath, statePath string, register, showArchived bool) (*RefreshResult, error) {
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

	// Registration is an explicit write performed only by RegisterWith
	// (ADR-0061). Pure reads pass register=false and never mutate Task state:
	// a discovered-but-unregistered set stays inert.
	needsSave := false
	var newRegs []string
	var newIDs []string
	if register {
		registered := state.RegisteredIDs(canon)
		for id := range disc.Manifests {
			if _, ok := registered[id]; !ok {
				needsSave = true
				break
			}
		}
		if needsSave {
			err := UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
				mergeNewRegistrations(d, canon, disc, state, &newRegs, &newIDs)
				return nil
			})
			if err != nil {
				return nil, err
			}
			sort.Strings(newRegs)
			sort.Strings(newIDs)
			state, err = LoadGlobalStateWith(d, statePath)
			if err != nil {
				return nil, err
			}
		}
	}

	manifests := make(map[string]*Manifest)
	for stem, manifestPath := range disc.Manifests {
		manifests[stem] = LoadManifest(d, stem, manifestPath)
	}

	result := &RefreshResult{
		DefinitionPath:     canon,
		NewRegistrations:   newRegs,
		NewRegistrationIDs: newIDs,
		Manifests:          manifests,
		NeedsSave:          needsSave,
		ArchivedCount:      archivedCount(state, canon),
		ShowArchived:       showArchived,
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
	var rows []Row

	entry := state.Tasks[defPath]

	if entry != nil {
		for i, reg := range entry.TaskSets {
			if reg.Archived != showArchived {
				continue
			}
			if _, ok := disc.Manifests[reg.ID]; !ok {
				rows = append(rows, Row{
					ID:           reg.ID,
					Status:       StatusMissing,
					Priority:     reg.Priority,
					PriorityShow: fmt.Sprintf("%d", reg.Priority),
					AutoDrain:    reg.AutoDrain,
					RegIndex:     i,
				})
				continue
			}
			rows = append(rows, buildTaskSetRow(reg, manifests[reg.ID], i))
		}
	}

	return orderStatusRows(rows)
}

// orderStatusRows returns rows in status-table display order: missing sets
// first, then Done sets (by registration order), then every active set (by
// priority, then registration order). It is applied after building rows and
// re-applied after Verify verdicts change statuses, so a formerly-Done set that
// now needs verification moves out of the Done group into the active group.
func orderStatusRows(rows []Row) []Row {
	var missing, done, active []Row
	for _, row := range rows {
		switch row.Status {
		case StatusMissing:
			missing = append(missing, row)
		case StatusDone:
			done = append(done, row)
		default:
			active = append(active, row)
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

	ordered := append([]Row{}, missing...)
	ordered = append(ordered, done...)
	ordered = append(ordered, active...)
	return ordered
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
		Started:      anyDone(m),
	}

	row.Progress = BuildProgress(m, status)
	if status == StatusBlocked || status == StatusAwaitingApproval {
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
		stW     = 17 // widest label is "AWAITING-APPROVAL" (17)
		prW     = 5
		detailW = 96
	)

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, "TASK SET", stW, "STATUS", prW, "PRI", "DETAILS")
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, strings.Repeat("-", idW), stW, strings.Repeat("-", stW), prW, strings.Repeat("-", prW), strings.Repeat("-", detailW))

	for _, row := range rows {
		detail := rowDetail(out, row)
		if len(detail) > detailW {
			detail = detail[:detailW-3] + "..."
		}
		id := row.ID
		if row.RunTarget {
			id = "▶ " + id
		}
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %s", idW, id, stW, StatusLabel(row), prW, row.PriorityShow, detail)
		if row.RunTarget {
			line = out.styled(ansiBold+ansiCyan, line)
		} else {
			line = out.styled(rowStatusStyle(row), line)
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

func rowDetail(out *output, row Row) string {
	if row.ConfigError != "" {
		base := rowStatusDetail(out, row)
		if base == "" {
			return "config error: " + row.ConfigError
		}
		return base + " — config error: " + row.ConfigError
	}
	return rowStatusDetail(out, row)
}

func rowStatusDetail(out *output, row Row) string {
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
	case StatusBlocked, StatusAwaitingApproval:
		parts := []string{row.Progress}
		if row.BlockedReason != "" {
			parts = append(parts, row.BlockedReason)
		}
		if row.CompleteHint != "" {
			parts = append(parts, "complete: "+row.CompleteHint)
		}
		if suffix := verifiedAtSuffix(row); suffix != "" {
			parts = append(parts, out.styled(ansiYellow, suffix))
		}
		return strings.Join(parts, " — ")
	case StatusNeedsVerify:
		return strings.Join([]string{row.Progress, "verify: pop tasks verify " + row.ID}, " — ")
	case StatusVerifyFailed:
		parts := []string{row.Progress}
		if f := firstFindingsLine(row.VerifyFindings); f != "" {
			parts = append(parts, "findings: "+f)
		}
		parts = append(parts, "re-verify: pop tasks verify "+row.ID)
		return strings.Join(parts, " — ")
	case StatusDone:
		if suffix := verifiedAtSuffix(row); suffix != "" {
			return row.Progress + " — " + out.styled(ansiYellow, suffix)
		}
		return row.Progress
	default:
		return row.Progress
	}
}

// verifiedAtSuffix returns the yellow display suffix for an immunized terminal
// row whose HEAD differs from the PASS verdict's work SHA. Empty when the row
// carries no such SHA.
func verifiedAtSuffix(row Row) string {
	if row.VerifiedAtSHA == "" {
		return ""
	}
	return "verified @ " + row.VerifiedAtSHA
}

// firstFindingsLine returns the first non-empty line of a Verifier's findings,
// so a VERIFY-FAILED row's one-line detail carries a hint of why without
// spilling the whole (possibly multi-paragraph) findings text into the table.
func firstFindingsLine(findings string) string {
	for _, line := range strings.Split(findings, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
	if task.Status == TaskFailed && task.FailedAfter != nil {
		return fmt.Sprintf("failed(%d)", *task.FailedAfter)
	}
	return string(task.Status)
}

func taskStyle(m *Manifest, task Task) string {
	switch task.Status {
	case TaskDone:
		return ansiGreen
	case TaskFailed:
		return ansiRed
	case TaskSkipped:
		return ansiYellow
	}
	if task.Type == "HITL" || !isEligible(m, task) {
		return ansiYellow
	}
	return ansiCyan
}

func taskSymbol(m *Manifest, task Task) string {
	switch task.Status {
	case TaskDone:
		return "✓"
	case TaskFailed, TaskSkipped:
		return "⊘"
	case TaskOpen:
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
