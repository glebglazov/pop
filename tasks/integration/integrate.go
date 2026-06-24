package integration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/binding"
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
	In            io.Reader
	AgentPreset   string
	AgentPresets  []string
	AgentExplicit bool
	AgentCmd      string
}

// JournalEntry is the integration module's journal event shape. Queue callers
// map it into queue.JournalEntry in IntegrateHooks.
type JournalEntry struct {
	Event       string
	Project     string
	SetID       string
	RuntimePath string
	MergeStatus string
	Target      string
	SourceRef   string
	Source      string
	Agent       string
	Reason      string
}

// IntegrateHooks injects queue-owned journal side effects.
type IntegrateHooks struct {
	AppendJournal func(JournalEntry) error
}

// Integrate merges a DONE set that was previously reported as clean into its
// working branch, then tears down the set worktree and branch.
func Integrate(d *Deps, cfg *config.Config, setID string, out io.Writer, hooks IntegrateHooks) (IntegrationResult, error) {
	return IntegrateWithOptions(d, cfg, setID, out, IntegrationOptions{In: tasks.NonInteractiveReader{}}, hooks)
}

// IntegrateWithOptions merges a completed set into its working branch. Clean
// sets are integrated directly; conflicting sets offer attended agent
// assistance and keep the worktree/branch unless the agent resolves the merge.
func IntegrateWithOptions(d *Deps, cfg *config.Config, setID string, out io.Writer, opts IntegrationOptions, hooks IntegrateHooks) (IntegrationResult, error) {
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

	store, err := Load(d.tasksDeps())
	if err != nil {
		return IntegrationResult{}, err
	}
	key, rec, ok, err := findRecord(store, setID)
	if err != nil {
		return IntegrationResult{}, err
	}
	if !ok {
		// No Mergeability record, but a set can be in the Integration backlog by
		// virtue of its non-trunk Worktree binding alone (ADR-0051): a set whose
		// final task was completed by hand pre-fix never had Mergeability
		// computed. Recompute it from the binding so integrate acts on the
		// binding, not a stale record gate. Falls through to the no-op message
		// only when the set genuinely has no integrable worktree binding.
		computed, cerr := recomputeMergeabilityFromBinding(d, setID)
		if cerr != nil {
			return IntegrationResult{}, cerr
		}
		if computed {
			store, err = Load(d.tasksDeps())
			if err != nil {
				return IntegrationResult{}, err
			}
			key, rec, ok, err = findRecord(store, setID)
			if err != nil {
				return IntegrationResult{}, err
			}
		}
		if !ok {
			fmt.Fprintf(out, "queue: %s is already integrated or is not awaiting integration\n", setID)
			return IntegrationResult{SetID: setID, Noop: true}, nil
		}
	}
	if rec.Status != StatusClean {
		if rec.Status == StatusConflicts {
			return integrateConflictingSet(d, cfg, key, rec, out, opts, hooks)
		}
		return IntegrationResult{}, fmt.Errorf("queue: %s mergeability is %s; refusing integration", setID, mergeabilityLabel(rec.Status))
	}
	return integrateCleanRecord(d, cfg, key, rec, out, "human", hooks)
}

// recomputeMergeabilityFromBinding computes and records Mergeability for a set
// that sits in the Integration backlog by virtue of a non-trunk Worktree
// binding but has no Mergeability record yet (ADR-0051), and reports whether a
// record now exists. No-op (false, nil) when the set has no binding, when more
// than one binding matches the set id (ambiguous across repos — left to the
// operator), or when the worktree resolves to no integrable main worktree.
func recomputeMergeabilityFromBinding(d *Deps, setID string) (bool, error) {
	if d == nil {
		d = DefaultDeps()
	}
	td := d.tasksDeps()
	bindings, err := binding.AllBindings(td)
	if err != nil {
		return false, err
	}
	var match *binding.Binding
	var matchKey string
	for k, b := range bindings {
		if binding.SetIDFromKey(k) != setID {
			continue
		}
		if match != nil {
			return false, nil // same set bound in multiple repos: ambiguous
		}
		bcopy := b
		match = &bcopy
		matchKey = k
	}
	if match == nil || strings.TrimSpace(match.RuntimePath) == "" {
		return false, nil
	}
	// Compute directly against the binding's checkout and store under the
	// binding's own scoped key. We deliberately do not route through
	// RecordImplementMergeability: it re-derives the store key from the
	// worktree path's repository identity, which need not match the key the
	// binding was provisioned under, so it would silently find nothing. Reusing
	// matchKey also keeps the record and binding co-keyed, so integration's
	// teardown removes both.
	mainPath, bare, err := binding.ResolveTrunkPath(td, nil, match.RuntimePath)
	if err != nil {
		return false, err
	}
	if bare || mainPath == "" {
		return false, nil
	}
	merge, err := d.computeMergeability(mainPath, match.RuntimePath)
	if err != nil {
		return false, err
	}
	merge.Project = match.Project
	merge.RuntimePath = match.RuntimePath
	merge.SetID = setID
	if merge.CheckedAt.IsZero() {
		merge.CheckedAt = time.Now().UTC()
	}
	if !AwaitsIntegration(merge) {
		return false, nil // trunk-equivalent: nothing to integrate
	}
	store, err := Load(td)
	if err != nil {
		return false, err
	}
	store.Put(matchKey, merge)
	if err := Save(td, store); err != nil {
		return false, err
	}
	return true, nil
}

// IntegrateKnownRecord merges a record already keyed in the store. Used by the
// queue supervisor auto-merge path.
func IntegrateKnownRecord(d *Deps, cfg *config.Config, key string, rec Record, out io.Writer, source string, hooks IntegrateHooks) (IntegrationResult, error) {
	if source == "" {
		source = "human"
	}
	return integrateCleanRecord(d, cfg, key, rec, out, source, hooks)
}

func integrateCleanRecord(d *Deps, cfg *config.Config, key string, rec Record, out io.Writer, source string, hooks IntegrateHooks) (IntegrationResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if source == "" {
		source = "human"
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
	provisioned := bindingShouldTeardown(d.tasksDeps(), key)
	if provisioned {
		if err := teardownIntegratedBranch(d, scan.RuntimePath, rec.RuntimePath, branch); err != nil {
			return IntegrationResult{}, err
		}
	}
	if err := DeleteRecord(d.tasksDeps(), key); err != nil {
		return IntegrationResult{}, err
	}
	if err := binding.Delete(d.tasksDeps(), key); err != nil {
		return IntegrationResult{}, err
	}
	if hooks.AppendJournal != nil {
		if err := hooks.AppendJournal(JournalEntry{
			Event:       "integrated",
			Project:     rec.Project,
			SetID:       rec.SetID,
			RuntimePath: rec.RuntimePath,
			Source:      source,
			SourceRef:   branch,
		}); err != nil {
			return IntegrationResult{}, err
		}
	}
	if provisioned {
		fmt.Fprintf(out, "queue: integrated %s into %s and removed worktree %s\n", rec.SetID, scan.RuntimePath, rec.RuntimePath)
	} else {
		fmt.Fprintf(out, "queue: integrated %s into %s (adopted checkout retained)\n", rec.SetID, scan.RuntimePath)
	}
	return IntegrationResult{SetID: rec.SetID, Project: rec.Project, RuntimePath: rec.RuntimePath, Branch: branch, Outcome: "integrated"}, nil
}

type integrationScan struct {
	RuntimePath string
}

func resolveIntegrationScan(d *Deps, cfg *config.Config, rec Record) (integrationScan, error) {
	projects, err := tasks.ListPickerProjectsWith(d.projectDeps(), cfg)
	if err != nil {
		return integrationScan{}, err
	}
	var matches []integrationScan
	for _, p := range projects {
		if rec.Project != "" && p.Name != rec.Project {
			continue
		}
		resolved, _, err := tasks.ResolveScanPaths(d.tasksDeps(), p.Path)
		if err != nil {
			return integrationScan{}, err
		}
		matches = append(matches, integrationScan{RuntimePath: resolved.ProjectPath})
	}
	switch len(matches) {
	case 0:
		return integrationScan{}, fmt.Errorf("queue: project %q for set %s is not configured", rec.Project, rec.SetID)
	case 1:
		return matches[0], nil
	default:
		return integrationScan{}, fmt.Errorf("queue: project %q for set %s is ambiguous", rec.Project, rec.SetID)
	}
}

func integrateConflictingSet(d *Deps, cfg *config.Config, key string, rec Record, out io.Writer, opts IntegrationOptions, hooks IntegrateHooks) (IntegrationResult, error) {
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
	if hooks.AppendJournal != nil {
		if err := hooks.AppendJournal(JournalEntry{
			Event:       "integration_conflict",
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
	}

	agentPreset, err := integrationAgentPreset(cfg, opts)
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
		if err := appendIntegrationOutcome(hooks, rec, branch, "declined"); err != nil {
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

	if hooks.AppendJournal != nil {
		if err := hooks.AppendJournal(JournalEntry{
			Event:       "integration_attended",
			Project:     rec.Project,
			SetID:       rec.SetID,
			RuntimePath: rec.RuntimePath,
			Agent:       invocation.AgentPreset,
			Source:      "human",
			SourceRef:   branch,
		}); err != nil {
			return IntegrationResult{}, err
		}
	}
	if err := startConflictedMerge(d, scan.RuntimePath, branch); err != nil {
		return IntegrationResult{}, err
	}
	fmt.Fprintf(out, "Starting conflict assistance: %s\n", invocation.Display)
	exitCode, err := runAttendedAssistance(d, opts.In, scan.RuntimePath, out, invocation)
	if err != nil {
		if outcomeErr := appendIntegrationOutcome(hooks, rec, branch, "start_failed"); outcomeErr != nil {
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
		if err := appendIntegrationOutcome(hooks, rec, branch, "unresolved"); err != nil {
			return IntegrationResult{}, err
		}
		fmt.Fprintf(out, "queue: conflict unresolved; kept worktree %s and branch %s for inspection\n", rec.RuntimePath, branch)
		result.Outcome = "unresolved"
		return result, nil
	}

	provisioned := bindingShouldTeardown(d.tasksDeps(), key)
	if provisioned {
		if err := teardownIntegratedBranch(d, scan.RuntimePath, rec.RuntimePath, branch); err != nil {
			return IntegrationResult{}, err
		}
	}
	if err := DeleteRecord(d.tasksDeps(), key); err != nil {
		return IntegrationResult{}, err
	}
	if err := binding.Delete(d.tasksDeps(), key); err != nil {
		return IntegrationResult{}, err
	}
	if hooks.AppendJournal != nil {
		if err := hooks.AppendJournal(JournalEntry{
			Event:       "integrated",
			Project:     rec.Project,
			SetID:       rec.SetID,
			RuntimePath: rec.RuntimePath,
			Source:      "human",
			SourceRef:   branch,
		}); err != nil {
			return IntegrationResult{}, err
		}
	}
	if err := appendIntegrationOutcome(hooks, rec, branch, "resolved"); err != nil {
		return IntegrationResult{}, err
	}
	if provisioned {
		fmt.Fprintf(out, "queue: resolved and integrated %s into %s; removed worktree %s\n", rec.SetID, scan.RuntimePath, rec.RuntimePath)
	} else {
		fmt.Fprintf(out, "queue: resolved and integrated %s into %s (adopted checkout retained)\n", rec.SetID, scan.RuntimePath)
	}
	return IntegrationResult{SetID: rec.SetID, Project: rec.Project, RuntimePath: rec.RuntimePath, Branch: branch, Outcome: "resolved"}, nil
}

// MainWorktreeBranch returns the branch checked out in the Trunk worktree —
// the merge target for an implement worktree drain (ADR-0036).
func MainWorktreeBranch(d *Deps, runtimePath string) (string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	td := d.tasksDeps()
	mainPath, bare, err := binding.ResolveTrunkPath(td, nil, runtimePath)
	if err != nil || bare || mainPath == "" {
		return "", err
	}
	out, err := td.Git.CommandInDir(mainPath, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

type conflictIntegrationAction int

const (
	conflictActionKeep conflictIntegrationAction = iota
	conflictActionAssist
)

func promptConflictIntegrationAction(out io.Writer, in io.Reader, rec Record, invocation *tasks.AgentAssistanceInvocation) (conflictIntegrationAction, error) {
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

// BuildConflictResolutionPrompt builds the agent prompt for conflict resolution.
func BuildConflictResolutionPrompt(rec Record, workingPath, branch string) string {
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

func integrationAgentPreset(cfg *config.Config, opts IntegrationOptions) (string, error) {
	presets := tasks.ResolveDefaultAgentPresets(opts.AgentPresets, opts.AgentPreset, opts.AgentExplicit, cfg)
	if len(presets) == 0 {
		return tasks.DefaultAgentPreset, nil
	}
	return presets[0], nil
}

func runAttendedAssistance(d *Deps, stdin io.Reader, runtimePath string, out io.Writer, invocation *tasks.AgentAssistanceInvocation) (int, error) {
	td := d.tasksDeps()
	if td.Runner == nil {
		td.Runner = tasks.RealCommandRunner{}
	}
	if attended, ok := td.Runner.(tasks.AttendedCommandRunner); ok {
		return attended.RunAttended(context.Background(), runtimePath, stdin, out, out, invocation.Command.Name, invocation.Command.Args...)
	}
	return td.Runner.Run(context.Background(), runtimePath, out, out, invocation.Command.Name, invocation.Command.Args...)
}

func appendIntegrationOutcome(hooks IntegrateHooks, rec Record, branch, outcome string) error {
	if hooks.AppendJournal == nil {
		return nil
	}
	return hooks.AppendJournal(JournalEntry{
		Event:       "integration_outcome",
		Project:     rec.Project,
		SetID:       rec.SetID,
		RuntimePath: rec.RuntimePath,
		Reason:      outcome,
		Source:      "human",
		SourceRef:   branch,
	})
}

func resolveSetBranch(d *Deps, runtimePath string) (string, error) {
	td := d.tasksDeps()
	branch, err := td.Git.CommandInDir(runtimePath, "branch", "--show-current")
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
	td := d.tasksDeps()
	if _, err := td.Git.CommandInDir(workingPath, "merge", branch); err != nil {
		return fmt.Errorf("git merge %s: %w", branch, err)
	}
	return nil
}

func startConflictedMerge(d *Deps, workingPath, branch string) error {
	td := d.tasksDeps()
	if _, err := td.Git.CommandInDir(workingPath, "merge", branch); err == nil {
		return nil
	}
	if hasMergeHead(d, workingPath) {
		return nil
	}
	return fmt.Errorf("git merge %s did not leave a merge in progress", branch)
}

func finishResolvedMerge(d *Deps, workingPath, branch string) (bool, error) {
	td := d.tasksDeps()
	if !hasMergeHead(d, workingPath) {
		return branchMerged(d, workingPath, branch), nil
	}
	if _, err := td.Git.CommandInDir(workingPath, "add", "-A"); err != nil {
		return false, fmt.Errorf("stage resolved merge: %w", err)
	}
	if _, err := td.Git.CommandInDir(workingPath, "diff", "--cached", "--check"); err != nil {
		return false, nil
	}
	unmerged, err := td.Git.CommandInDir(workingPath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, fmt.Errorf("inspect unresolved conflicts: %w", err)
	}
	if strings.TrimSpace(unmerged) != "" {
		return false, nil
	}
	if _, err := td.Git.CommandInDir(workingPath, "commit", "--no-edit"); err != nil {
		return false, fmt.Errorf("commit resolved merge: %w", err)
	}
	return branchMerged(d, workingPath, branch), nil
}

func hasMergeHead(d *Deps, workingPath string) bool {
	td := d.tasksDeps()
	_, err := td.Git.CommandInDir(workingPath, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

func branchMerged(d *Deps, workingPath, branch string) bool {
	td := d.tasksDeps()
	_, err := td.Git.CommandInDir(workingPath, "merge-base", "--is-ancestor", branch, "HEAD")
	return err == nil
}

func teardownIntegratedBranch(d *Deps, workingPath, runtimePath, branch string) error {
	return binding.TeardownWorktree(d.tasksDeps(), workingPath, runtimePath, branch, false)
}

func bindingShouldTeardown(td *tasks.Deps, key string) bool {
	return binding.ShouldTeardown(td, key)
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

func mergeabilityLabel(status string) string {
	switch status {
	case StatusClean:
		return "merges clean"
	case StatusConflicts:
		return "conflicts"
	default:
		return "mergeability unknown"
	}
}

// ResolveIntegrationScan resolves the working checkout for integration. Exported
// for queue abandon confirmation paths.
func ResolveIntegrationScan(d *Deps, cfg *config.Config, rec Record) (projectPath string, err error) {
	scan, err := resolveIntegrationScan(d, cfg, rec)
	if err != nil {
		return "", err
	}
	return scan.RuntimePath, nil
}

// SetIDsFromStore returns sorted set IDs present in the mergeability store.
func SetIDsFromStore(td *tasks.Deps) ([]string, error) {
	store, err := Load(td)
	if err != nil || store == nil || len(store.Records) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(store.Records))
	for _, rec := range store.Records {
		ids = append(ids, rec.SetID)
	}
	sort.Strings(ids)
	return ids, nil
}
