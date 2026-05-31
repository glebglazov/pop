package workload

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Selection is the PRD and issue chosen for execution.
type Selection struct {
	PRDID      string
	IssueID    string
	PRDPath    string
	IssuePath  string
	IssueFile  string
	Manifest   *Manifest
	Issue      Issue
	IssueIndex int
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
	var row *Row
	for i := range refresh.Rows {
		if refresh.Rows[i].ID == prdID {
			row = &refresh.Rows[i]
			break
		}
	}
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
	case StatusUnplanned:
		return nil, exitErr(ExitNoRunnable, "PRD %q is unplanned", prdID)
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

	prdPath := discoverPRDPath(m)
	return &Selection{
		PRDID:      prdID,
		IssueID:    issueID,
		PRDPath:    prdPath,
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
		prdPath := discoverPRDPath(m)
		return &Selection{
			PRDID:      prdID,
			IssueID:    issue.ID,
			PRDPath:    prdPath,
			IssuePath:  filepath.Join(m.Dir, issue.File),
			IssueFile:  issue.File,
			Manifest:   m,
			Issue:      issue,
			IssueIndex: i,
		}, nil
	}
	return nil, exitErr(ExitNoRunnable, "PRD %q has no eligible AFK issue", prdID)
}

func discoverPRDPath(m *Manifest) string {
	defPath := filepath.Dir(filepath.Dir(m.Dir))
	return filepath.Join(defPath, prdsSubdir, m.Stem+".md")
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
