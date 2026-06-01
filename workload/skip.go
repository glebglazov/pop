package workload

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// SkipIssueOptions configures deferring one open issue.
type SkipIssueOptions struct {
	ResolveInput
	IssuePath string
}

// SkipIssueResult is the outcome of skipping an issue.
type SkipIssueResult struct {
	IssueSetID string
	IssueID    string
	Refresh    *RefreshResult
}

// SkipIssue defers one open issue to skipped status.
func SkipIssue(opts SkipIssueOptions) (*SkipIssueResult, error) {
	return SkipIssueWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// SkipIssueWith defers an open issue using injected dependencies.
func SkipIssueWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SkipIssueOptions) (*SkipIssueResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	issueSetID, issueID, err := ResolveIssueTarget(d, refresh, opts.CWD, opts.IssuePath)
	if err != nil {
		return nil, err
	}
	if issueSetID == "" || issueID == "" {
		return nil, exitErr(ExitSetup, "skip-issue requires an issue path")
	}

	m := refresh.Manifests[issueSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "Issue set %q has no issue manifest", issueSetID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "Issue set %q is malformed", issueSetID)
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
	if issue.Status != "open" {
		return nil, exitErr(ExitNoRunnable, "issue %q is %s; skip requires an open issue", issueID, issue.Status)
	}

	summary := fmt.Sprintf("skipped %s/%s", issueSetID, issueID)
	if err := AppendProgress(d, m.Dir, issue.File, "SKIP", summary); err != nil {
		return nil, manualRepairErr(err)
	}

	m.Issues[idx].Status = "skipped"
	m.Issues[idx].FailedAfter = nil
	if err := WriteManifestAtomic(d, m); err != nil {
		return nil, manualRepairErr(fmt.Errorf("update manifest after skip progress: %w", err))
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after skip: %v", err)
	}

	return &SkipIssueResult{IssueSetID: issueSetID, IssueID: issueID, Refresh: afterRefresh}, nil
}
