package queue

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
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
	Kept        bool
	Outcome     string
}

// IntegrationOptions controls the attended conflict-resolution path.
type IntegrationOptions struct {
	In          io.Reader
	AgentPreset string
	AgentCmd    string
}

// Integrate merges a DONE set that was previously reported as clean into its
// working branch, then tears down the set worktree and branch.
func Integrate(d *Deps, cfg *config.Config, setID string, out io.Writer) (IntegrationResult, error) {
	return IntegrateWithOptions(d, cfg, setID, out, IntegrationOptions{In: tasks.NonInteractiveReader{}})
}

// IntegrateWithOptions merges a completed set into its working branch. Clean
// sets are integrated directly; conflicting sets offer attended agent
// assistance and keep the worktree/branch unless the agent resolves the merge.
func IntegrateWithOptions(d *Deps, cfg *config.Config, setID string, out io.Writer, opts IntegrationOptions) (IntegrationResult, error) {
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
	if rec.Status != MergeabilityClean {
		if rec.Status == MergeabilityConflicts {
			return integrateConflictingSet(d, cfg, key, rec, out, opts)
		}
		return IntegrationResult{}, fmt.Errorf("queue: %s mergeability is %s; refusing integration", setID, mergeabilityLabel(rec.Status))
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

	branch, err := resolveSetBranch(d, rec.RuntimePath)
	if err != nil {
		return IntegrationResult{}, err
	}
	if err := mergeBranch(d, scan.RuntimePath, branch); err != nil {
		return IntegrationResult{}, err
	}
	if err := teardownIntegratedBranch(d, scan.RuntimePath, rec.RuntimePath, branch); err != nil {
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
	return IntegrationResult{SetID: rec.SetID, Project: rec.Project, RuntimePath: rec.RuntimePath, Branch: branch, Outcome: "integrated"}, nil
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

func integrateConflictingSet(d *Deps, cfg *config.Config, key string, rec MergeabilityRecord, out io.Writer, opts IntegrationOptions) (IntegrationResult, error) {
	scan, err := resolveIntegrationScan(d, cfg, rec)
	if err != nil {
		return IntegrationResult{}, err
	}
	branch, err := resolveSetBranch(d, rec.RuntimePath)
	if err != nil {
		return IntegrationResult{}, err
	}
	result := IntegrationResult{SetID: rec.SetID, Project: rec.Project, RuntimePath: rec.RuntimePath, Branch: branch, Kept: true, Outcome: "kept"}
	fmt.Fprintf(out, "queue: %s has merge conflicts between %s and %s\n", rec.SetID, shortRef(rec.Target), shortRef(rec.Source))
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventIntegrationConflict,
		Project:     rec.Project,
		SetID:       rec.SetID,
		RuntimePath: rec.RuntimePath,
		MergeStatus: rec.Status,
		Target:      rec.Target,
		SourceRef:   rec.Source,
		Source:      "human",
	}); err != nil {
		return IntegrationResult{}, err
	}

	agentPreset, err := integrationAgentPreset(cfg, opts.AgentPreset)
	if err != nil {
		return IntegrationResult{}, err
	}
	prompt := BuildConflictResolutionPrompt(rec, scan.RuntimePath, branch)
	invocation, err := tasks.ResolveAgentAssistanceInvocation(agentPreset, opts.AgentCmd, prompt, scan.RuntimePath)
	if err != nil {
		return IntegrationResult{}, err
	}
	action, err := promptConflictIntegrationAction(out, opts.In, rec, invocation)
	if err != nil {
		return IntegrationResult{}, err
	}
	if action != conflictActionAssist {
		fmt.Fprintf(out, "queue: kept conflicted worktree %s on branch %s; %s remains awaiting integration\n", rec.RuntimePath, branch, rec.SetID)
		if err := appendIntegrationOutcome(d, rec, branch, "declined"); err != nil {
			return IntegrationResult{}, err
		}
		result.Outcome = "declined"
		return result, nil
	}

	lock, err := d.acquireRuntimeLock(scan.RuntimePath)
	if err != nil {
		return IntegrationResult{}, err
	}
	defer func() { _ = lock.Release() }()

	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventIntegrationAttended,
		Project:     rec.Project,
		SetID:       rec.SetID,
		RuntimePath: rec.RuntimePath,
		Agent:       invocation.AgentPreset,
		Source:      "human",
		SourceRef:   branch,
	}); err != nil {
		return IntegrationResult{}, err
	}
	if err := startConflictedMerge(d, scan.RuntimePath, branch); err != nil {
		return IntegrationResult{}, err
	}
	fmt.Fprintf(out, "Starting conflict assistance: %s\n", invocation.Display)
	exitCode, err := runQueueAttendedAssistance(d, opts.In, scan.RuntimePath, out, invocation)
	if err != nil {
		if outcomeErr := appendIntegrationOutcome(d, rec, branch, "start_failed"); outcomeErr != nil {
			return IntegrationResult{}, outcomeErr
		}
		fmt.Fprintf(out, "queue: could not start conflict assistance: %v\n", err)
		return result, nil
	}
	if exitCode != 0 {
		fmt.Fprintf(out, "queue: conflict assistance exited with status %d; checking merge state\n", exitCode)
	}
	resolved, err := finishResolvedMerge(d, scan.RuntimePath, branch)
	if err != nil {
		return IntegrationResult{}, err
	}
	if !resolved {
		if err := appendIntegrationOutcome(d, rec, branch, "unresolved"); err != nil {
			return IntegrationResult{}, err
		}
		fmt.Fprintf(out, "queue: conflict unresolved; kept worktree %s and branch %s for inspection\n", rec.RuntimePath, branch)
		result.Outcome = "unresolved"
		return result, nil
	}

	if err := teardownIntegratedBranch(d, scan.RuntimePath, rec.RuntimePath, branch); err != nil {
		return IntegrationResult{}, err
	}
	state, err := EnsureDaemonState(d.Tasks)
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
	if err := appendIntegrationOutcome(d, rec, branch, "resolved"); err != nil {
		return IntegrationResult{}, err
	}
	fmt.Fprintf(out, "queue: resolved and integrated %s into %s; removed worktree %s\n", rec.SetID, scan.RuntimePath, rec.RuntimePath)
	return IntegrationResult{SetID: rec.SetID, Project: rec.Project, RuntimePath: rec.RuntimePath, Branch: branch, Outcome: "resolved"}, nil
}

type conflictIntegrationAction int

const (
	conflictActionKeep conflictIntegrationAction = iota
	conflictActionAssist
)

func promptConflictIntegrationAction(out io.Writer, in io.Reader, rec MergeabilityRecord, invocation *tasks.AgentAssistanceInvocation) (conflictIntegrationAction, error) {
	display := out
	if display == nil {
		display = io.Discard
	}
	fmt.Fprintln(display)
	fmt.Fprintf(display, "Conflict-blocked: %s needs human judgement before integration can continue.\n", rec.SetID)
	fmt.Fprintln(display, "  1. Get agent assistance (default)")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  2. Keep worktree and exit")
	fmt.Fprint(display, "Choose [1]: ")

	if in == nil {
		in = os.Stdin
	}
	if _, ok := in.(tasks.NonInteractiveReader); ok {
		return conflictActionKeep, nil
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return conflictActionKeep, fmt.Errorf("read conflict integration selection: %w", err)
	}
	if err == io.EOF && answer == "" {
		return conflictActionKeep, nil
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "1":
		return conflictActionAssist, nil
	case "2", "q", "quit", "exit":
		return conflictActionKeep, nil
	default:
		fmt.Fprintln(display, "Choose 1 or 2.")
		return promptConflictIntegrationAction(out, in, rec, invocation)
	}
}

func BuildConflictResolutionPrompt(rec MergeabilityRecord, workingPath, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are assisting a human with a Pop queue integration conflict.\n\n")
	fmt.Fprintf(&b, "Task set: %s\n", rec.SetID)
	if rec.Project != "" {
		fmt.Fprintf(&b, "Project: %s\n", rec.Project)
	}
	fmt.Fprintf(&b, "Working checkout: %s\n", workingPath)
	fmt.Fprintf(&b, "Set worktree: %s\n", rec.RuntimePath)
	fmt.Fprintf(&b, "Set branch: %s\n", branch)
	fmt.Fprintf(&b, "Target commit: %s\n", rec.Target)
	fmt.Fprintf(&b, "Source commit: %s\n\n", rec.Source)
	fmt.Fprintf(&b, "Pop has started `git merge %s` in the working checkout and stopped for conflicts.\n", branch)
	fmt.Fprintf(&b, "Resolve the conflicts in the working checkout, stage the resolved files, and leave the merge ready for Pop to commit, or commit the merge yourself.\n")
	fmt.Fprintf(&b, "Do not delete the set worktree or branch; Pop will remove them after it verifies the merge landed.\n")
	return b.String()
}

func integrationAgentPreset(cfg *config.Config, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	if cfg == nil {
		return tasks.DefaultAgentPreset, nil
	}
	qcfg, err := cfg.ResolveQueue()
	if err != nil {
		return "", err
	}
	if len(qcfg.Agents) == 0 {
		return tasks.DefaultAgentPreset, nil
	}
	return qcfg.Agents[0], nil
}

func runQueueAttendedAssistance(d *Deps, stdin io.Reader, runtimePath string, out io.Writer, invocation *tasks.AgentAssistanceInvocation) (int, error) {
	if d.Tasks.Runner == nil {
		d.Tasks.Runner = tasks.RealCommandRunner{}
	}
	if attended, ok := d.Tasks.Runner.(tasks.AttendedCommandRunner); ok {
		return attended.RunAttended(context.Background(), runtimePath, stdin, out, out, invocation.Command.Name, invocation.Command.Args...)
	}
	return d.Tasks.Runner.Run(context.Background(), runtimePath, out, out, invocation.Command.Name, invocation.Command.Args...)
}

func appendIntegrationOutcome(d *Deps, rec MergeabilityRecord, branch, outcome string) error {
	return AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventIntegrationOutcome,
		Project:     rec.Project,
		SetID:       rec.SetID,
		RuntimePath: rec.RuntimePath,
		Reason:      outcome,
		Source:      "human",
		SourceRef:   branch,
	})
}

func resolveSetBranch(d *Deps, runtimePath string) (string, error) {
	branch, err := d.Tasks.Git.CommandInDir(runtimePath, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("resolve set branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("resolve set branch: worktree %s is detached", runtimePath)
	}
	return branch, nil
}

func mergeBranch(d *Deps, workingPath, branch string) error {
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "merge", branch); err != nil {
		return fmt.Errorf("git merge %s: %w", branch, err)
	}
	return nil
}

func startConflictedMerge(d *Deps, workingPath, branch string) error {
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "merge", branch); err == nil {
		return nil
	}
	if hasMergeHead(d, workingPath) {
		return nil
	}
	return fmt.Errorf("git merge %s did not leave a merge in progress", branch)
}

func finishResolvedMerge(d *Deps, workingPath, branch string) (bool, error) {
	if !hasMergeHead(d, workingPath) {
		return branchMerged(d, workingPath, branch), nil
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "add", "-A"); err != nil {
		return false, fmt.Errorf("stage resolved merge: %w", err)
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "diff", "--cached", "--check"); err != nil {
		return false, nil
	}
	unmerged, err := d.Tasks.Git.CommandInDir(workingPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, fmt.Errorf("inspect unresolved conflicts: %w", err)
	}
	if strings.TrimSpace(unmerged) != "" {
		return false, nil
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "commit", "--no-edit"); err != nil {
		return false, fmt.Errorf("commit resolved merge: %w", err)
	}
	return branchMerged(d, workingPath, branch), nil
}

func hasMergeHead(d *Deps, workingPath string) bool {
	_, err := d.Tasks.Git.CommandInDir(workingPath, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

func branchMerged(d *Deps, workingPath, branch string) bool {
	_, err := d.Tasks.Git.CommandInDir(workingPath, "merge-base", "--is-ancestor", branch, "HEAD")
	return err == nil
}

func teardownIntegratedBranch(d *Deps, workingPath, runtimePath, branch string) error {
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "worktree", "remove", runtimePath); err != nil {
		return fmt.Errorf("remove worktree %s: %w", runtimePath, err)
	}
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "branch", "-d", branch); err != nil {
		return fmt.Errorf("delete branch %s: %w", branch, err)
	}
	return nil
}

func shortRef(ref string) string {
	if ref == "" {
		return "unknown"
	}
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}
