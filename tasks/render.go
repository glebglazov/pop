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
	Manifests          map[string]*Manifest
	NeedsSave          bool
	RuntimeLock        *RuntimeLockStatus
	Checkout           *CheckoutStatus
	ArchivedCount      int
	ShowArchived       bool
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

// archivedRows is which registered sets a refresh builds rows for. It is three
// answers rather than a bool because the three views are genuinely different
// questions: the default table (active work), `pop tasks status --archived` (the
// filing cabinet on its own), and the Work dashboard's show-archived toggle, which
// wants both at once so an archived row can be unarchived beside the live ones.
type archivedRows int

const (
	archivedHidden   archivedRows = iota // active sets only
	archivedOnly                         // archived sets only
	archivedIncluded                     // both, archived rows carrying Archived
)

// RefreshWith performs a pure-read refresh using injected dependencies and
// state path. It never writes Task state — discovered-but-unregistered sets
// are inert (ADR-0061).
func RefreshWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, false, false, archivedHidden)
}

// RefreshArchivedWith performs a pure-read refresh and returns only Archived
// Task sets. Like RefreshWith, it never registers.
func RefreshArchivedWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, false, false, archivedOnly)
}

// RefreshIncludingArchivedWith performs a pure-read refresh over archived and
// active sets together, each row saying which it is. It backs the Work dashboard's
// show-archived view (ADR-0186), where the point is to act on an archived row
// without losing sight of the live ones.
func RefreshIncludingArchivedWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, false, false, archivedIncluded)
}

// RegisterWith is the sole writer of Task-set registration (ADR-0061): it
// discovers on-disk sets, registers the new ones (assigning order), and returns
// the resulting status. Registration seeds no worktree/auto-drain state from the
// manifest — those retired keys are no longer read (ADR-0115); binding is
// materialized eagerly by the register command. It must be run from inside the
// repo so its cwd is a valid checkout.
func RegisterWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, true, false, archivedHidden)
}

// RegisterManagedWith registers newly discovered sets like RegisterWith. Managed
// worktrees are provisioned eagerly by the register command (ADR-0147); this
// helper exists for tests and callers that provision bindings separately.
func RegisterManagedWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	return refreshWith(d, defPath, statePath, true, false, archivedHidden)
}

func refreshWith(d *Deps, defPath, statePath string, register, managed bool, archived archivedRows) (*RefreshResult, error) {
	canon, err := migrateForRefresh(d, defPath)
	if err != nil {
		return nil, err
	}

	disc, err := discoverForRefresh(d, canon)
	if err != nil {
		return nil, err
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
				mergeNewRegistrations(d, canon, disc, state, managed, &newRegs, &newIDs)
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

	result := buildRefreshResult(d, canon, disc, state, archived)
	result.NewRegistrations = newRegs
	result.NewRegistrationIDs = newIDs
	result.NeedsSave = needsSave
	return result, nil
}

// migrateForRefresh is the write half of a refresh: the one-shot storage-layout
// migration, plus the canonicalization on either side of it. Migration may have
// created the tasks directory for the first time, so the path is re-canonicalized
// after it — a path that did not exist resolves symlinks once it does, and the
// state key must match what migration wrote and what future calls resolve.
func migrateForRefresh(d *Deps, defPath string) (string, error) {
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return "", err
	}
	if _, err := MigrateStorageLayout(d, canon); err != nil {
		return "", err
	}
	return CanonicalDefinitionPathWith(d, canon)
}

// discoverForRefresh scans a canonical definition path, treating an unreadable
// task directory as the refresh's own error rather than an empty discovery.
func discoverForRefresh(d *Deps, canon string) (*Discovery, error) {
	disc, err := DiscoverWith(d, canon)
	if err != nil {
		return nil, err
	}
	if disc.TaskDirErr != nil {
		return nil, disc.TaskDirErr
	}
	return disc, nil
}

// buildRefreshResult is the pure half of a refresh: load every discovered
// manifest and project the registrations onto rows. It reads the filesystem and
// the state it is handed, and touches neither the store nor anything writable —
// which is what lets several definition paths run through it at once (ADR-0189).
func buildRefreshResult(d *Deps, canon string, disc *Discovery, state *GlobalState, archived archivedRows) *RefreshResult {
	manifests := make(map[string]*Manifest)
	for stem, manifestPath := range disc.Manifests {
		manifests[stem] = LoadManifest(d, stem, manifestPath)
	}

	result := &RefreshResult{
		DefinitionPath: canon,
		Manifests:      manifests,
		ArchivedCount:  archivedCount(state, canon),
		// ShowArchived means "this is the archive view", which the included view is
		// not: it is the active table with the archived rows let in, so the callers
		// that skip work on an archive view (the Verify verdict pass, the "N archived
		// hidden" footer) go on treating it as the ordinary table.
		ShowArchived: archived == archivedOnly,
	}
	result.Rows = buildRows(state, canon, disc, manifests, archived)
	MarkNextPick(result.Rows)
	return result
}

// AttachBound stamps each row's Bound flag from the Worktree bindings already in
// the store — one AllBindings read, no git. Call before rendering `pop tasks
// status` so Unfolded can ride beside the status the way Container.Bound does on
// the Work dashboard (ADR-0197). Best-effort: a missing store leaves Bound false.
func AttachBound(d *Deps, rows []Row) {
	if d == nil || len(rows) == 0 {
		return
	}
	s, ok, err := d.Store(false)
	if err != nil || !ok {
		return
	}
	all, err := s.AllBindings()
	if err != nil {
		return
	}
	bound := make(map[string]bool, len(all))
	for key, b := range all {
		if strings.TrimSpace(b.RuntimePath) == "" {
			continue
		}
		parts := strings.Split(key, "\x00")
		if len(parts) == 2 {
			bound[parts[1]] = true
		}
	}
	for i := range rows {
		rows[i].Bound = bound[rows[i].ID]
	}
}

// includeRegistration reports whether one registration belongs in the view.
func includeRegistration(rowArchived bool, view archivedRows) bool {
	switch view {
	case archivedOnly:
		return rowArchived
	case archivedIncluded:
		return true
	default:
		return !rowArchived
	}
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

func buildRows(state *GlobalState, defPath string, disc *Discovery, manifests map[string]*Manifest, archived archivedRows) []Row {
	var rows []Row

	entry := state.Tasks[defPath]

	if entry != nil {
		for i, reg := range entry.TaskSets {
			if !includeRegistration(reg.Archived, archived) {
				continue
			}
			if _, ok := disc.Manifests[reg.ID]; !ok {
				rows = append(rows, Row{
					ID:           reg.ID,
					Status:       StatusMissing,
					Priority:     reg.Priority,
					PriorityShow: fmt.Sprintf("%d", reg.Priority),
					AutoDrain:    reg.AutoDrain,
					Archived:     reg.Archived,
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
		Archived:     reg.Archived,
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
		prW     = 5
		detailW = 96
	)
	stW := len("STATUS")
	for _, row := range rows {
		if n := len(statusColumnText(row)); n > stW {
			stW = n
		}
	}

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
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %s", idW, id, stW, statusColumnText(row), prW, row.PriorityShow, detail)
		if row.RunTarget {
			line = out.styled(ansiBold+ansiCyan, line)
		} else {
			line = out.styled(rowStatusStyle(row), line)
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

// statusColumnText is the STATUS cell `pop tasks status` prints: the display
// label plus the unfolded mark when the shared Unfolded predicate holds
// (ADR-0197). Verification badges stay in DETAILS; unfolded rides beside the
// status the way WorkRowStatusSegments does on the Work dashboard.
func statusColumnText(row Row) string {
	label := StatusLabel(row)
	if Unfolded(row.Bound, row.Status) {
		return label + " · " + UnfoldedMark
	}
	return label
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
		parts = append(parts, verifyMarkDetailParts(row)...)
		if suffix := verifiedAtBadgeSuffix(out, row); suffix != "" {
			parts = append(parts, suffix)
		}
		return strings.Join(parts, " — ")
	case StatusNeedsVerify:
		parts := []string{row.Progress, "verify: pop tasks verify " + row.ID}
		if suffix := verifiedAtBadgeSuffix(out, row); suffix != "" {
			parts = append(parts, suffix)
		}
		return strings.Join(parts, " — ")
	case StatusVerifyFailed:
		parts := []string{row.Progress}
		if f := firstFindingsLine(row.VerifyFindings); f != "" {
			parts = append(parts, "findings: "+f)
		}
		parts = append(parts, "re-verify: pop tasks verify "+row.ID)
		if suffix := verifiedAtBadgeSuffix(out, row); suffix != "" {
			parts = append(parts, suffix)
		}
		return strings.Join(parts, " — ")
	case StatusDone:
		parts := append([]string{row.Progress}, verifyMarkDetailParts(row)...)
		if suffix := verifiedAtBadgeSuffix(out, row); suffix != "" {
			parts = append(parts, suffix)
		}
		return strings.Join(parts, " — ")
	default:
		return row.Progress
	}
}

// verifyMarkDetailParts are the detail segments a verification mark contributes
// to a terminal row whose status the verdict did not become — a human-completed
// set. The findings must stay reachable when VERIFY-FAILED is only a mark, so the
// same first-findings-line and re-verify hint a VERIFY-FAILED row carries are
// spelled out here beside DONE.
func verifyMarkDetailParts(row Row) []string {
	if row.Status == StatusNeedsVerify || row.Status == StatusVerifyFailed {
		return nil
	}
	switch row.VerifyMark {
	case VerifyMarkFailed:
		var parts []string
		if f := firstFindingsLine(row.VerifyFindings); f != "" {
			parts = append(parts, "findings: "+f)
		}
		return append(parts, "re-verify: pop tasks verify "+row.ID)
	case VerifyMarkUnverified:
		return []string{"verify: pop tasks verify " + row.ID}
	}
	return nil
}

// verifiedAtBadgeSuffix returns the styled Verified-at SHA badge for a row.
func verifiedAtBadgeSuffix(out *output, row Row) string {
	badge := DeriveVerifiedAtBadge(row)
	text := VerifiedAtBadgeText(badge)
	if text == "" {
		return ""
	}
	return out.styled(verifiedAtBadgeANSI(badge), text)
}

func verifiedAtBadgeANSI(badge VerifiedAtBadge) string {
	switch badge.State {
	case VerifiedAtAtHead:
		return ansiGreen
	case VerifiedAtDrifted:
		return ansiYellow
	case VerifiedAtUnverified, VerifiedAtFailed:
		return ansiRed
	default:
		return ""
	}
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
	bound := false
	if row != nil {
		status = row.Status
		progress = row.Progress
		bound = row.Bound
	}

	statusText := string(status)
	if Unfolded(bound, status) {
		statusText += " · " + UnfoldedMark
	}
	header := fmt.Sprintf("%s  [%s]", taskSetID, statusText)
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
