package workload

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ResetIssueOptions configures resetting one failed issue.
type ResetIssueOptions struct {
	ResolveInput
	PRDID   string
	IssueID string
}

// ResetIssueResult is the outcome of resetting a failed issue.
type ResetIssueResult struct {
	Refresh *RefreshResult
}

// ResetIssue returns one failed issue to open status.
func ResetIssue(opts ResetIssueOptions) (*ResetIssueResult, error) {
	return ResetIssueWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// ResetIssueWith resets a failed issue using injected dependencies.
func ResetIssueWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts ResetIssueOptions) (*ResetIssueResult, error) {
	if opts.PRDID == "" || opts.IssueID == "" {
		return nil, exitErr(ExitSetup, "reset-issue requires --prd and --issue")
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	m := refresh.Manifests[opts.PRDID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "PRD %q has no issue manifest", opts.PRDID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "PRD %q is malformed", opts.PRDID)
	}

	idx := -1
	for i, issue := range m.Issues {
		if issue.ID == opts.IssueID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, exitErr(ExitNoRunnable, "%s", unknownIssueMessage(m, opts.IssueID))
	}

	issue := m.Issues[idx]
	if issue.Status != "failed" {
		return nil, exitErr(ExitNoRunnable, "issue %q is %s; reset requires a failed issue", opts.IssueID, issue.Status)
	}

	summary := fmt.Sprintf("reset %s/%s to open", opts.PRDID, opts.IssueID)
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

	return &ResetIssueResult{Refresh: afterRefresh}, nil
}
