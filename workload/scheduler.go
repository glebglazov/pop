package workload

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Selection is the Issue set and issue chosen for execution.
type Selection struct {
	IssueSetID string
	IssueID    string
	IssuePath  string
	IssueFile  string
	Manifest   *Manifest
	Issue      Issue
	IssueIndex int
}

// SelectIssueSet chooses the Issue set to drain using the same readiness and failed-set
// gates as SelectIssue, without selecting an issue.
func SelectIssueSet(refresh *RefreshResult, issueSetOverride string) (string, error) {
	if issueSetOverride != "" {
		return selectExplicitIssueSet(refresh, issueSetOverride)
	}
	return selectAutomaticIssueSet(refresh)
}

func selectAutomaticIssueSet(refresh *RefreshResult) (string, error) {
	manifests := refresh.Manifests
	if manifests == nil {
		manifests = make(map[string]*Manifest)
	}
	for _, row := range refresh.Rows {
		if row.Status != StatusReady {
			continue
		}
		m := manifests[row.ID]
		if m == nil || !m.Valid {
			continue
		}
		if _, err := firstEligibleIssue(row.ID, m); err != nil {
			continue
		}
		return row.ID, nil
	}
	return "", exitErr(ExitNoRunnable, "no runnable work")
}

func selectExplicitIssueSet(refresh *RefreshResult, issueSetID string) (string, error) {
	row := findRow(refresh, issueSetID)
	if row == nil {
		return "", exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, issueSetID))
	}

	m := refresh.Manifests[issueSetID]
	if m == nil {
		return "", exitErr(ExitNoRunnable, "Issue set %q has no issue manifest", issueSetID)
	}

	switch row.Status {
	case StatusFailed:
		return "", exitErr(ExitNoRunnable, "Issue set %q has failed issues; reset required before execution", issueSetID)
	case StatusMalformed:
		return "", exitErr(ExitNoRunnable, "Issue set %q is malformed", issueSetID)
	case StatusMissing:
		return "", exitErr(ExitNoRunnable, "Issue set %q is missing", issueSetID)
	case StatusReady, StatusDeferred:
		return issueSetID, nil
	default:
		return "", exitErr(ExitNoRunnable, "Issue set %q is %s: %s", issueSetID, strings.ToLower(string(row.Status)), row.BlockedReason)
	}
}

// SelectIssueInSet chooses the first eligible AFK issue in manifest-array order
// for one Issue set.
func SelectIssueInSet(refresh *RefreshResult, issueSetID string) (*Selection, error) {
	m := refresh.Manifests[issueSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "Issue set %q has no issue manifest", issueSetID)
	}
	return firstEligibleIssue(issueSetID, m)
}

func findRow(refresh *RefreshResult, issueSetID string) *Row {
	for i := range refresh.Rows {
		if refresh.Rows[i].ID == issueSetID {
			return &refresh.Rows[i]
		}
	}
	return nil
}

// SelectIssue chooses the next issue to run from a refreshed workload.
func SelectIssue(refresh *RefreshResult, issueSetOverride, issueOverride string) (*Selection, error) {
	if issueOverride != "" && issueSetOverride == "" {
		return nil, exitErr(ExitSetup, "explicit issue requires an Issue set")
	}

	manifests := refresh.Manifests
	if manifests == nil {
		manifests = make(map[string]*Manifest)
	}

	if issueSetOverride != "" {
		return selectExplicit(refresh, manifests, issueSetOverride, issueOverride)
	}
	return selectAutomatic(refresh, manifests)
}

func selectAutomatic(refresh *RefreshResult, manifests map[string]*Manifest) (*Selection, error) {
	for _, row := range refresh.Rows {
		if row.Status != StatusReady {
			continue
		}
		m := manifests[row.ID]
		if m == nil || !m.Valid {
			continue
		}
		sel, err := firstEligibleIssue(row.ID, m)
		if err != nil {
			continue
		}
		return sel, nil
	}
	return nil, exitErr(ExitNoRunnable, "no runnable work")
}

func selectExplicit(refresh *RefreshResult, manifests map[string]*Manifest, issueSetID, issueID string) (*Selection, error) {
	row := findRow(refresh, issueSetID)
	if row == nil {
		return nil, exitErr(ExitNoRunnable, "%s", unknownIssueSetTargetMessage(refresh, issueSetID))
	}

	m := manifests[issueSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "Issue set %q has no issue manifest", issueSetID)
	}

	switch row.Status {
	case StatusFailed:
		return nil, exitErr(ExitNoRunnable, "Issue set %q has failed issues; reset required before execution", issueSetID)
	case StatusMalformed:
		return nil, exitErr(ExitNoRunnable, "Issue set %q is malformed", issueSetID)
	case StatusMissing:
		return nil, exitErr(ExitNoRunnable, "Issue set %q is missing", issueSetID)
	}

	if issueID == "" {
		if row.Status != StatusReady {
			return nil, exitErr(ExitNoRunnable, "Issue set %q is %s: %s", issueSetID, strings.ToLower(string(row.Status)), row.BlockedReason)
		}
		return firstEligibleIssue(issueSetID, m)
	}

	return selectExplicitIssue(issueSetID, m, issueID)
}

func selectExplicitIssue(issueSetID string, m *Manifest, issueID string) (*Selection, error) {
	idx := -1
	for i, issue := range m.Issues {
		if issue.ID == issueID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, exitErr(ExitNoRunnable, "%s", unknownIssueMessage(m, issueID))
	}

	issue := m.Issues[idx]
	switch issue.Status {
	case "done":
		return nil, exitErr(ExitNoRunnable, "issue %q is already done", issueID)
	case "failed":
		return nil, exitErr(ExitNoRunnable, "issue %q failed; reset required", issueID)
	case "skipped":
		return nil, exitErr(ExitNoRunnable, "issue %q is skipped", issueID)
	}
	if issue.Type == "HITL" {
		return nil, exitErr(ExitNoRunnable, "issue %q is HITL", issueID)
	}
	if issue.Type != "AFK" {
		return nil, exitErr(ExitNoRunnable, "issue %q is not AFK", issueID)
	}
	for _, blocker := range issue.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return nil, exitErr(ExitNoRunnable, "issue %q blocked by %s", issueID, blocker)
		}
	}

	return &Selection{
		IssueSetID: issueSetID,
		IssueID:    issueID,
		IssuePath:  filepath.Join(m.Dir, issue.File),
		IssueFile:  issue.File,
		Manifest:   m,
		Issue:      issue,
		IssueIndex: idx,
	}, nil
}

func firstEligibleIssue(issueSetID string, m *Manifest) (*Selection, error) {
	for i, issue := range m.Issues {
		if !isEligible(m, issue) {
			continue
		}
		return &Selection{
			IssueSetID: issueSetID,
			IssueID:    issue.ID,
			IssuePath:  filepath.Join(m.Dir, issue.File),
			IssueFile:  issue.File,
			Manifest:   m,
			Issue:      issue,
			IssueIndex: i,
		}, nil
	}
	return nil, exitErr(ExitNoRunnable, "Issue set %q has no eligible AFK issue", issueSetID)
}

func unknownIssueSetTargetMessage(refresh *RefreshResult, issueSetID string) string {
	var ids []string
	for _, row := range refresh.Rows {
		if row.Status != StatusMissing {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return fmt.Sprintf("unknown Issue set %q", issueSetID)
	}
	return fmt.Sprintf("unknown Issue set %q; valid: %s", issueSetID, strings.Join(ids, ", "))
}

func unknownIssueMessage(m *Manifest, issueID string) string {
	var ids []string
	for _, issue := range m.Issues {
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	return fmt.Sprintf("unknown issue %q; valid: %s", issueID, strings.Join(ids, ", "))
}

// MarkAutoPick marks the highest-priority runnable Issue-set row with AUTO.
// Non-runnable higher-priority rows are skipped.
func MarkAutoPick(rows []Row) {
	for i := range rows {
		if rows[i].Status != StatusReady {
			continue
		}
		rows[i].AutoPick = true
		rows[i].PriorityShow = fmt.Sprintf("%d AUTO", rows[i].Priority)
		return
	}
}

// MarkRunTarget marks the selected Issue-set row with RUN, combining with AUTO when applicable.
func MarkRunTarget(rows []Row, issueSetID string) {
	for i := range rows {
		if rows[i].ID != issueSetID {
			continue
		}
		rows[i].RunTarget = true
		if rows[i].AutoPick {
			rows[i].PriorityShow = fmt.Sprintf("%d AUTO RUN", rows[i].Priority)
		} else {
			rows[i].PriorityShow = fmt.Sprintf("%d RUN", rows[i].Priority)
		}
		return
	}
}
