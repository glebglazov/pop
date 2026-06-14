package queue

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// IntegrationResult describes the outcome of a human-triggered clean set
// integration.
type IntegrationResult struct {
	SetID       string
	Project     string
	RuntimePath string
	Branch      string
	Noop        bool
}

// Integrate merges a DONE set that was previously reported as clean into its
// working branch, then tears down the set worktree and branch.
func Integrate(d *Deps, cfg *config.Config, setID string, out io.Writer) (IntegrationResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return IntegrationResult{}, fmt.Errorf("set id is required")
	}
	if out == nil {
		out = io.Discard
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}

	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return IntegrationResult{}, err
	}
	key, rec, ok, err := findIntegrationRecord(state, setID)
	if err != nil {
		return IntegrationResult{}, err
	}
	if !ok {
		fmt.Fprintf(out, "queue: %s is already integrated or is not awaiting integration\n", setID)
		return IntegrationResult{SetID: setID, Noop: true}, nil
	}
	if rec.Status == MergeabilityConflicts {
		return IntegrationResult{}, fmt.Errorf("queue: %s has conflicts; clean integration is deferred to conflict handling", setID)
	}
	if rec.Status != MergeabilityClean {
		return IntegrationResult{}, fmt.Errorf("queue: %s mergeability is %s; refusing clean integration", setID, mergeabilityLabel(rec.Status))
	}

	scan, err := resolveIntegrationScan(d, cfg, rec)
	if err != nil {
		return IntegrationResult{}, err
	}
	lock, err := d.acquireRuntimeLock(scan.RuntimePath)
	if err != nil {
		return IntegrationResult{}, err
	}
	defer func() { _ = lock.Release() }()

	branch, err := integrateCleanBranch(d, scan.RuntimePath, rec.RuntimePath)
	if err != nil {
		return IntegrationResult{}, err
	}
	delete(state.Mergeability, key)
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		return IntegrationResult{}, err
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventIntegrated,
		Project:     rec.Project,
		SetID:       rec.SetID,
		RuntimePath: rec.RuntimePath,
		Source:      "human",
		SourceRef:   branch,
	}); err != nil {
		return IntegrationResult{}, err
	}
	fmt.Fprintf(out, "queue: integrated %s into %s and removed worktree %s\n", rec.SetID, scan.RuntimePath, rec.RuntimePath)
	return IntegrationResult{SetID: rec.SetID, Project: rec.Project, RuntimePath: rec.RuntimePath, Branch: branch}, nil
}

func findIntegrationRecord(state *DaemonState, setID string) (string, MergeabilityRecord, bool, error) {
	if state == nil || len(state.Mergeability) == 0 {
		return "", MergeabilityRecord{}, false, nil
	}
	var keys []string
	for key, rec := range state.Mergeability {
		if rec.SetID == setID {
			keys = append(keys, key)
		}
	}
	switch len(keys) {
	case 0:
		return "", MergeabilityRecord{}, false, nil
	case 1:
		return keys[0], state.Mergeability[keys[0]], true, nil
	default:
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "queue: set %q is ambiguous; awaiting integration in:", setID)
		for _, key := range keys {
			rec := state.Mergeability[key]
			fmt.Fprintf(&b, "\n  %s (%s)", rec.Project, rec.RuntimePath)
		}
		return "", MergeabilityRecord{}, false, fmt.Errorf("%s", b.String())
	}
}

func resolveIntegrationScan(d *Deps, cfg *config.Config, rec MergeabilityRecord) (projectScan, error) {
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return projectScan{}, err
	}
	var matches []projectScan
	for _, p := range projects {
		if rec.Project != "" && p.Name != rec.Project {
			continue
		}
		scan, err := resolveScan(d, p)
		if err != nil {
			return projectScan{}, err
		}
		matches = append(matches, scan)
	}
	switch len(matches) {
	case 0:
		return projectScan{}, fmt.Errorf("queue: project %q for set %s is not configured", rec.Project, rec.SetID)
	case 1:
		return matches[0], nil
	default:
		return projectScan{}, fmt.Errorf("queue: project %q for set %s is ambiguous", rec.Project, rec.SetID)
	}
}

func integrateCleanBranch(d *Deps, workingPath, runtimePath string) (string, error) {
	branch, err := d.Tasks.Git.CommandInDir(runtimePath, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("resolve set branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("resolve set branch: worktree %s is detached", runtimePath)
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "merge", branch); err != nil {
		return "", fmt.Errorf("git merge %s: %w", branch, err)
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "worktree", "remove", runtimePath); err != nil {
		return "", fmt.Errorf("remove worktree %s: %w", runtimePath, err)
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "branch", "-d", branch); err != nil {
		return "", fmt.Errorf("delete branch %s: %w", branch, err)
	}
	return branch, nil
}
