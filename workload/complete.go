package workload

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// CompleteIssueOptions configures manually completing one issue.
type CompleteIssueOptions struct {
	ResolveInput
	IssuePath string
}

// CompleteIssueResult is the outcome of manually completing an issue.
type CompleteIssueResult struct {
	IssueSetID string
	IssueID    string
	Refresh    *RefreshResult
}

// CompleteIssue manually marks one issue Done without running an agent.
func CompleteIssue(opts CompleteIssueOptions) (*CompleteIssueResult, error) {
	return CompleteIssueWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// CompleteIssueWith manually completes an issue using injected dependencies.
func CompleteIssueWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts CompleteIssueOptions) (*CompleteIssueResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	issueSetID, issueID, err := ResolveIssueFileTarget(refresh, opts.IssuePath)
	if err != nil {
		return nil, err
	}
	if issueSetID == "" || issueID == "" {
		return nil, exitErr(ExitSetup, "complete-issue requires an issue path")
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
	if issue.Status == "done" {
		return nil, exitErr(ExitNoRunnable, "issue %q is already done", issueID)
	}

	for _, blocker := range issue.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return nil, exitErr(ExitNoRunnable, "issue %q blocked by %s; complete it first", issueID, blocker)
		}
	}

	priorStatus := issue.Status
	summary := fmt.Sprintf("manually completed %s/%s (was %s)", issueSetID, issueID, priorStatus)
	if err := AppendProgress(d, m.Dir, issue.File, "COMPLETE", summary); err != nil {
		return nil, manualRepairErr(err)
	}

	m.Issues[idx].Status = "done"
	m.Issues[idx].FailedAfter = nil
	if err := WriteManifestAtomic(d, m); err != nil {
		return nil, manualRepairErr(fmt.Errorf("update manifest after complete progress: %w", err))
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after complete: %v", err)
	}

	return &CompleteIssueResult{IssueSetID: issueSetID, IssueID: issueID, Refresh: afterRefresh}, nil
}
