package workload

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// RefreshResult is the outcome of a workload status refresh.
type RefreshResult struct {
	DefinitionPath   string
	NewRegistrations []string
	Rows             []Row
	Manifests        map[string]*Manifest
	NeedsSave        bool
	RuntimeLock      *RuntimeLockStatus
}

// Refresh discovers workloads, auto-registers Issue sets, and builds table rows.
func Refresh(defPath string) (*RefreshResult, error) {
	return RefreshWith(defaultDeps, defPath, DefaultStatePath())
}

// RefreshWith performs refresh using injected dependencies and state path.
func RefreshWith(d *Deps, defPath, statePath string) (*RefreshResult, error) {
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return nil, err
	}

	disc, err := DiscoverWith(d, canon)
	if err != nil {
		return nil, err
	}
	if disc.IssueDirErr != nil {
		return nil, disc.IssueDirErr
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
	}
	result.Rows = buildRows(state, canon, disc, manifests)
	MarkAutoPick(result.Rows)
	return result, nil
}

func buildRows(state *GlobalState, defPath string, disc *Discovery, manifests map[string]*Manifest) []Row {
	var missing, done, active []Row

	entry := state.Workloads[defPath]

	if entry != nil {
		for i, reg := range entry.IssueSets {
			if _, ok := disc.Manifests[reg.ID]; !ok {
				missing = append(missing, Row{
					ID:           reg.ID,
					Status:       StatusMissing,
					Priority:     reg.Priority,
					PriorityShow: fmt.Sprintf("%d", reg.Priority),
					RegIndex:     i,
				})
				continue
			}
			row := buildIssueSetRow(reg, manifests[reg.ID], i)
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

func buildIssueSetRow(reg RegisteredIssueSet, m *Manifest, regIndex int) Row {
	status := DeriveStatus(m)
	row := Row{
		ID:           reg.ID,
		Status:       status,
		Priority:     reg.Priority,
		PriorityShow: fmt.Sprintf("%d", reg.Priority),
		RegIndex:     regIndex,
	}

	row.Progress = BuildProgress(m, status)
	if status == StatusBlocked {
		row.BlockedReason = BuildBlockedReason(m)
		if hitl := BlockingHITLIssue(m); hitl != nil {
			row.CompleteHint = completeIssueHint(reg.ID, hitl.File)
		}
	}
	if status == StatusFailed {
		row.FailedIssues, row.ResetHints = BuildFailedInfo(reg.ID, m)
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
	if len(result.NewRegistrations) > 0 {
		fmt.Fprintf(w, "Registered new Issue set(s): %s\n\n", strings.Join(result.NewRegistrations, ", "))
	}

	if len(result.Rows) == 0 {
		fmt.Fprintf(w, "No workloads at %s\n", result.DefinitionPath)
		return
	}

	fmt.Fprintln(w, formatTable(result.Rows))
	renderRuntimeLock(w, result.RuntimeLock)
	renderDiagnostics(w, result.Rows)
}

func formatTable(rows []Row) string {
	const (
		idW     = 28
		stW     = 10
		prW     = 5
		detailW = 96
	)

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, "ISSUE SET", stW, "STATUS", prW, "PRI", "DETAILS")
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, strings.Repeat("-", idW), stW, strings.Repeat("-", stW), prW, strings.Repeat("-", prW), strings.Repeat("-", detailW))

	for _, row := range rows {
		detail := rowDetail(row)
		if len(detail) > detailW {
			detail = detail[:detailW-3] + "..."
		}
		fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, row.ID, stW, string(row.Status), prW, row.PriorityShow, detail)
	}
	return b.String()
}

func rowDetail(row Row) string {
	switch row.Status {
	case StatusMissing:
		return "registered Issue set missing"
	case StatusMalformed:
		return row.MalformedSummary
	case StatusFailed:
		parts := []string{row.Progress}
		if len(row.FailedIssues) > 0 {
			parts = append(parts, "failed: "+strings.Join(row.FailedIssues, ", "))
		}
		if len(row.ResetHints) > 0 {
			parts = append(parts, "reset: "+row.ResetHints[0])
		}
		return strings.Join(parts, " — ")
	case StatusBlocked:
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
	fmt.Fprintln(w)
	switch {
	case lock.Malformed:
		fmt.Fprintf(w, "Runtime execution lock (best effort): malformed lock file for %s\n", lock.RuntimePath)
	case lock.Metadata != nil && lock.Locked:
		fmt.Fprintf(w, "Runtime execution lock (best effort): PID %d since %s at %s\n",
			lock.Metadata.PID,
			lock.Metadata.StartedAt.Format(time.RFC3339),
			lock.Metadata.RuntimePath,
		)
	default:
		fmt.Fprintf(w, "Runtime execution lock (best effort): idle at %s\n", lock.RuntimePath)
	}
}

func renderDiagnostics(w io.Writer, rows []Row) {
	var detailRows []Row
	for _, row := range rows {
		if row.Status == StatusMalformed && len(row.DetailErrors) > 0 {
			detailRows = append(detailRows, row)
		}
	}
	if len(detailRows) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Malformed diagnostics:")
	for _, row := range detailRows {
		fmt.Fprintf(w, "  %s:\n", row.ID)
		for _, err := range row.DetailErrors {
			fmt.Fprintf(w, "    - %s\n", err)
		}
	}
}
