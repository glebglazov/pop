package workload

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Selection is the Issue set and issue chosen for execution.
type Selection struct {
	PRDID      string
	IssueID    string
	IssuePath  string
	IssueFile  string
	Manifest   *Manifest
	Issue      Issue
	IssueIndex int
}

// SelectPRD chooses the PRD to drain using the same readiness and failed-PRD
// gates as SelectIssue, without selecting an issue.
func SelectPRD(refresh *RefreshResult, prdOverride string) (string, error) {
	if prdOverride != "" {
		return selectExplicitPRD(refresh, prdOverride)
	}
	return selectAutomaticPRD(refresh)
}

func selectAutomaticPRD(refresh *RefreshResult) (string, error) {
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

func selectExplicitPRD(refresh *RefreshResult, prdID string) (string, error) {
	row := findRow(refresh, prdID)
	if row == nil {
		return "", exitErr(ExitNoRunnable, "%s", unknownPRDTargetMessage(refresh, prdID))
	}

	m := refresh.Manifests[prdID]
	if m == nil {
		return "", exitErr(ExitNoRunnable, "PRD %q has no issue manifest", prdID)
	}

	switch row.Status {
	case StatusFailed:
		return "", exitErr(ExitNoRunnable, "PRD %q has failed issues; reset required before execution", prdID)
	case StatusMalformed:
		return "", exitErr(ExitNoRunnable, "PRD %q is malformed", prdID)
	case StatusMissing:
		return "", exitErr(ExitNoRunnable, "PRD %q is missing", prdID)
	case StatusReady:
		return prdID, nil
	default:
		return "", exitErr(ExitNoRunnable, "PRD %q is %s: %s", prdID, strings.ToLower(string(row.Status)), row.BlockedReason)
	}
}

// SelectIssueInPRD chooses the first eligible AFK issue in manifest-array order
// for one PRD.
func SelectIssueInPRD(refresh *RefreshResult, prdID string) (*Selection, error) {
	m := refresh.Manifests[prdID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "PRD %q has no issue manifest", prdID)
	}
	return firstEligibleIssue(prdID, m)
}

func findRow(refresh *RefreshResult, prdID string) *Row {
	for i := range refresh.Rows {
		if refresh.Rows[i].ID == prdID {
			return &refresh.Rows[i]
		}
	}
	return nil
}

// SelectIssue chooses the next issue to run from a refreshed workload.
func SelectIssue(refresh *RefreshResult, prdOverride, issueOverride string) (*Selection, error) {
	if issueOverride != "" && prdOverride == "" {
		return nil, exitErr(ExitSetup, "explicit --issue requires --prd")
	}

	manifests := refresh.Manifests
	if manifests == nil {
		manifests = make(map[string]*Manifest)
	}

	if prdOverride != "" {
		return selectExplicit(refresh, manifests, prdOverride, issueOverride)
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

func selectExplicit(refresh *RefreshResult, manifests map[string]*Manifest, prdID, issueID string) (*Selection, error) {
	row := findRow(refresh, prdID)
	if row == nil {
		return nil, exitErr(ExitNoRunnable, "%s", unknownPRDTargetMessage(refresh, prdID))
	}

	m := manifests[prdID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "PRD %q has no issue manifest", prdID)
	}

	switch row.Status {
	case StatusFailed:
		return nil, exitErr(ExitNoRunnable, "PRD %q has failed issues; reset required before execution", prdID)
	case StatusMalformed:
		return nil, exitErr(ExitNoRunnable, "PRD %q is malformed", prdID)
	case StatusMissing:
		return nil, exitErr(ExitNoRunnable, "PRD %q is missing", prdID)
	}

	if issueID == "" {
		if row.Status != StatusReady {
			return nil, exitErr(ExitNoRunnable, "PRD %q is %s: %s", prdID, strings.ToLower(string(row.Status)), row.BlockedReason)
		}
		return firstEligibleIssue(prdID, m)
	}

	return selectExplicitIssue(prdID, m, issueID)
}

func selectExplicitIssue(prdID string, m *Manifest, issueID string) (*Selection, error) {
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
	}
	if issue.Type == "HITL" {
		return nil, exitErr(ExitNoRunnable, "issue %q is HITL", issueID)
	}
	if issue.Type != "AFK" {
		return nil, exitErr(ExitNoRunnable, "issue %q is not AFK", issueID)
	}
	for _, blocker := range issue.BlockedBy {
		if !issueDone(m, blocker) {
			return nil, exitErr(ExitNoRunnable, "issue %q blocked by %s", issueID, blocker)
		}
	}

	return &Selection{
		PRDID:      prdID,
		IssueID:    issueID,
		IssuePath:  filepath.Join(m.Dir, issue.File),
		IssueFile:  issue.File,
		Manifest:   m,
		Issue:      issue,
		IssueIndex: idx,
	}, nil
}

func firstEligibleIssue(prdID string, m *Manifest) (*Selection, error) {
	for i, issue := range m.Issues {
		if !isEligible(m, issue) {
			continue
		}
		return &Selection{
			PRDID:      prdID,
			IssueID:    issue.ID,
			IssuePath:  filepath.Join(m.Dir, issue.File),
			IssueFile:  issue.File,
			Manifest:   m,
			Issue:      issue,
			IssueIndex: i,
		}, nil
	}
	return nil, exitErr(ExitNoRunnable, "PRD %q has no eligible AFK issue", prdID)
}

func unknownPRDTargetMessage(refresh *RefreshResult, prdID string) string {
	var ids []string
	for _, row := range refresh.Rows {
		if row.Status != StatusMissing {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return fmt.Sprintf("unknown PRD %q", prdID)
	}
	return fmt.Sprintf("unknown PRD %q; valid: %s", prdID, strings.Join(ids, ", "))
}

func unknownIssueMessage(m *Manifest, issueID string) string {
	var ids []string
	for _, issue := range m.Issues {
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	return fmt.Sprintf("unknown issue %q; valid: %s", issueID, strings.Join(ids, ", "))
}

// MarkAutoPick marks the highest-priority runnable PRD row with AUTO.
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

// MarkRunTarget marks the selected PRD row with RUN, combining with AUTO when applicable.
func MarkRunTarget(rows []Row, prdID string) {
	for i := range rows {
		if rows[i].ID != prdID {
			continue
		}
		if rows[i].AutoPick {
			rows[i].PriorityShow = fmt.Sprintf("%d AUTO RUN", rows[i].Priority)
		} else {
			rows[i].PriorityShow = fmt.Sprintf("%d RUN", rows[i].Priority)
		}
		return
	}
}
