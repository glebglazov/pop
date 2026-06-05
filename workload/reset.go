package workload

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ResetIssueOptions configures resetting one failed or skipped issue.
type ResetIssueOptions struct {
	ResolveInput
	IssuePath string
}

// ResetIssueResult is the outcome of resetting a failed issue.
type ResetIssueResult struct {
	IssueSetID string
	IssueID    string
	Refresh    *RefreshResult
}

// ResetIssue returns one failed or skipped issue to open status.
func ResetIssue(opts ResetIssueOptions) (*ResetIssueResult, error) {
	return ResetIssueWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// ResetIssueWith resets a failed issue using injected dependencies.
func ResetIssueWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts ResetIssueOptions) (*ResetIssueResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	issueSetID, issueID, err := ResolveIssueFileTarget(refresh, opts.IssuePath)
	if err != nil {
		return nil, err
	}
	if issueSetID == "" || issueID == "" {
		return nil, exitErr(ExitSetup, "open requires a task path")
	}

	m := refresh.Manifests[issueSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "task set %q has no task manifest", issueSetID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "task set %q is malformed", issueSetID)
	}

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
	if issue.Status != "failed" && issue.Status != "skipped" {
		return nil, exitErr(ExitNoRunnable, "task %q is %s; open requires a failed or skipped task", issueID, issue.Status)
	}

	priorStatus := issue.Status
	summary := fmt.Sprintf("reset %s/%s to open (was %s)", issueSetID, issueID, priorStatus)
	if err := AppendProgress(d, m.Dir, issue.File, "RESET", summary); err != nil {
		return nil, manualRepairErr(err)
	}

	m.Issues[idx].Status = "open"
	m.Issues[idx].FailedAfter = nil
	if err := WriteManifestAtomic(d, m); err != nil {
		return nil, manualRepairErr(fmt.Errorf("update manifest after reset progress: %w", err))
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after reset: %v", err)
	}

	return &ResetIssueResult{IssueSetID: issueSetID, IssueID: issueID, Refresh: afterRefresh}, nil
}
