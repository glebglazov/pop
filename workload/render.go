package workload

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// RefreshResult is the outcome of a workload status refresh.
type RefreshResult struct {
	DefinitionPath string
	NewRegistrations []string
	Rows           []Row
	NeedsSave      bool
}

// Refresh discovers workloads, auto-registers PRDs, and builds table rows.
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
	if disc.PRDDirErr != nil {
		return nil, disc.PRDDirErr
	}
	if disc.IssueDirErr != nil {
		return nil, disc.IssueDirErr
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	entry := state.Entry(canon)
	registered := state.RegisteredIDs(canon)

	var newRegs []string
	for stem, prdPath := range disc.PRDs {
		if _, ok := registered[stem]; ok {
			continue
		}
		title := ExtractPRDTitle(d, prdPath)
		entry.PRDs = append(entry.PRDs, RegisteredPRD{
			ID:       stem,
			Priority: 0,
			Title:    title,
		})
		registered[stem] = len(entry.PRDs) - 1
		newRegs = append(newRegs, stem)
	}
	sort.Strings(newRegs)

	needsSave := len(newRegs) > 0
	if needsSave {
		if err := state.SaveWith(d); err != nil {
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
		NeedsSave:        needsSave,
	}
	result.Rows = buildRows(state, canon, disc, manifests)
	MarkAutoPick(result.Rows)
	return result, nil
}

func buildRows(state *GlobalState, defPath string, disc *Discovery, manifests map[string]*Manifest) []Row {
	var missing, done, active []Row

	entry := state.Workloads[defPath]
	discovered := make(map[string]bool)

	if entry != nil {
		for i, reg := range entry.PRDs {
			if _, ok := disc.PRDs[reg.ID]; !ok {
				missing = append(missing, Row{
					ID:           reg.ID,
					Status:       StatusMissing,
					Priority:     reg.Priority,
					PriorityShow: fmt.Sprintf("%d", reg.Priority),
					RegIndex:     i,
				})
				continue
			}
			discovered[reg.ID] = true
			row := buildPRDRow(reg, manifests[reg.ID], i, false)
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

	var orphans []Row
	for stem := range disc.Manifests {
		if disc.PRDs[stem] != "" {
			continue
		}
		m := manifests[stem]
		orphans = append(orphans, Row{
			ID:               stem,
			Status:           StatusMalformed,
			PriorityShow:     "-",
			MalformedSummary: MalformedSummary(m, true),
			DetailErrors:     orphanDetailErrors(stem),
			IsOrphan:         true,
		})
	}
	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].ID < orphans[j].ID
	})

	rows := append([]Row{}, missing...)
	rows = append(rows, done...)
	rows = append(rows, active...)
	rows = append(rows, orphans...)
	return rows
}

func buildPRDRow(reg RegisteredPRD, m *Manifest, regIndex int, orphan bool) Row {
	status := DeriveStatus(m)
	row := Row{
		ID:           reg.ID,
		Status:       status,
		Priority:     reg.Priority,
		PriorityShow: fmt.Sprintf("%d", reg.Priority),
		RegIndex:     regIndex,
		IsOrphan:     orphan,
	}

	if status == StatusUnplanned {
		row.Progress = BuildProgress(nil, status)
		return row
	}

	row.Progress = BuildProgress(m, status)
	if status == StatusBlocked {
		row.BlockedReason = BuildBlockedReason(m)
	}
	if status == StatusFailed {
		row.FailedIssues, row.ResetHints = BuildFailedInfo(reg.ID, m)
	}
	if status == StatusMalformed {
		row.MalformedSummary = MalformedSummary(m, orphan)
		if m != nil {
			row.DetailErrors = append([]string{}, m.Errors...)
		}
	}
	return row
}

func orphanDetailErrors(stem string) []string {
	return []string{
		fmt.Sprintf("no paired PRD at %s/%s.md", prdsSubdir, stem),
	}
}

// Render writes the status table and diagnostics to w.
func Render(w io.Writer, result *RefreshResult) {
	if len(result.NewRegistrations) > 0 {
		fmt.Fprintf(w, "Registered new PRD(s): %s\n\n", strings.Join(result.NewRegistrations, ", "))
	}

	if len(result.Rows) == 0 {
		fmt.Fprintf(w, "No workloads at %s\n", result.DefinitionPath)
		return
	}

	fmt.Fprintln(w, formatTable(result.Rows))
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
	fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %s\n", idW, "PRD", stW, "STATUS", prW, "PRI", "DETAILS")
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
		return "registered PRD document missing"
	case StatusUnplanned:
		return row.Progress
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
		if row.BlockedReason != "" {
			return row.Progress + " — " + row.BlockedReason
		}
		return row.Progress
	default:
		return row.Progress
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
