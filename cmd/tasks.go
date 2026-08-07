package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/tasks/implement"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var (
	taskProject               string
	taskPath                  string
	taskDefPath               string
	taskRuntimePath           string
	taskAgentPreset           string
	taskAgentPresets          []string
	taskAgentCmd              string
	taskAgentOutput           tasks.AgentOutputMode
	taskRunYes                bool
	taskInWorktree            bool
	taskForceRebind           bool
	taskAllowDirty            tasks.DirtyRuntimeStrategy = tasks.DirtyRuntimeContinue
	taskMaxTries              int
	taskTimeout               string
	taskVerifyTimeout         string
	taskVerifyAgents          []string
	taskVerifyEffort          string
	taskVerifyAccept          string
	taskVerifyRemediate       string
	taskImplementVerifyAgents []string
	taskImplementVerifyEffort string
	taskStatusArchived        bool
	taskAutoDrainOff          bool
	taskRegisterManaged       bool
	taskRegisterTrunk         string
	taskRegisterAutoDrain     bool
	taskBindWorktreeForce     bool
	taskBindWorktreeManaged   bool
	taskBindWorktreeTrunk     string
	taskUnbindWorktreeYes     bool
	taskFoldYes               bool
	taskStreamFull            bool
	taskStreamRaw             bool
	taskStreamLast            bool
	taskStreamToolDetail      bool
	taskSpendJSON             bool
	taskAgentsModels          bool
)

var taskCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Discover and manage local task sets",
}

var taskStatusCmd = &cobra.Command{
	Use:   "status [TASK_SET]",
	Short: "Show discovered task sets and their statuses, or one set's per-task breakdown",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTaskStatus,
}

// taskRegisterCmd's help is a flag reference and nothing more. Authoring
// doctrine — templates, typing, effort, slicing — is `pop tasks authoring-guide`,
// read once per authoring session by a machine, where -h is hit constantly by a
// human wanting flag syntax (ADR-0183).
var taskRegisterCmd = &cobra.Command{
	Use:   "register [TASK_SET]",
	Short: "Register newly authored task sets so they become visible and schedulable, then show status",
	Long: `Register newly authored task sets so they become visible and schedulable, then
show status. Writing a set's files only drafts it; this is the verb that makes
it Work.

With no flags the set is bound to the current checkout as it stands. --managed
instead forks an isolated worktree from the Trunk worktree and binds the set to
that, refusing when no trunk resolves (--trunk <path> names one, needed once per
bare repo). --auto-drain is independent of both: it sets the set's consent bit
so the Work daemon may drain the set unattended, and "pop tasks auto-drain" and
the dashboard's "a" toggle remain authoritative afterwards.

Re-registering an already-registered set never rebinds it; move it with
"pop tasks bind-worktree <task-set-name> --force" from inside the target
checkout.

A set that comes back MALFORMED is a fix loop, not a failure: the diagnostics
name every problem at once, so fix what they name and re-run until the set reads
READY (or DEFERRED, when every open task is HITL).

For the shape of the files themselves — layout, templates, manifest fields and
the typing rules — run "pop tasks authoring-guide".`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTaskRegister,
}

var taskArchiveCmd = &cobra.Command{
	Use:   "archive [TASK_SET]",
	Short: "Hide a registered task set from default task status and selection",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTaskArchive,
}

var taskUnarchiveCmd = &cobra.Command{
	Use:   "unarchive [TASK_SET]",
	Short: "Restore an archived task set to default task status and selection",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTaskUnarchive,
}

var taskSetPriorityCmd = &cobra.Command{
	Use:   "set-priority TASK_SET PRIORITY",
	Short: "Set a registered task-set priority",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskSetPriority,
}

var taskAutoDrainCmd = &cobra.Command{
	Use:   "auto-drain TASK_SET",
	Short: "Set (or clear with --off) a registered task set's auto-drain consent bit",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskAutoDrain,
}

var taskImplementCmd = &cobra.Command{
	Use:   "implement [TASK_SET | TASK_SET/FILE.md]",
	Short: "Implement tasks through a coding agent: drain a task set, or run one targeted task",
	Args:  cobra.MaximumNArgs(1),
	Run:   runTaskImplement,
}

var taskVerifyCmd = &cobra.Command{
	Use:   "verify TASK_SET",
	Short: "Run an independent Verifier agent over a task set and record a PASS/FIXABLE/NEEDS-HUMAN verdict",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskVerify,
}

var taskAssistCmd = &cobra.Command{
	Use:   "assist TASK_SET",
	Short: "Open an Assist session on a task set at its current status (no drain, no Verifier)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskAssist,
}

var taskResetTaskCmd = &cobra.Command{
	Use:   "open [TASK_SET | TASK_SET/FILE.md]",
	Short: "Reset failed, skipped, or done tasks back to open: one targeted task, or pick a set's tasks interactively",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskResetTask,
}

var taskCompleteTaskCmd = &cobra.Command{
	Use:   "complete [TASK_SET | TASK_SET/FILE.md]",
	Short: "Manually mark tasks done without running an agent: one targeted task, or pick a set's tasks interactively",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskCompleteTask,
}

var taskSkipTaskCmd = &cobra.Command{
	Use:   "skip [TASK_SET | TASK_SET/FILE.md]",
	Short: "Defer open tasks to skipped, unblocking dependents: one targeted task, or pick a set's tasks interactively",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskSkipTask,
}

var taskStreamCmd = &cobra.Command{
	Use:   "stream TASK_SET[/FILE.md]",
	Short: "Show per-task attempt stream replay derived from captured attempt streams",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskStream,
}

var taskSpendCmd = &cobra.Command{
	Use:   "spend [TASK_SET]",
	Short: "Roll up run spend across recent task sets or break one set down per task",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTaskSpend,
}

var taskShowPathCmd = &cobra.Command{
	Use:   "show-path [TASK_SET]",
	Short: "Print this repository's task storage directory, creating it on demand",
	Args:  cobra.MaximumNArgs(1),
	Run:   runTaskShowPath,
}

var taskTransferCmd = &cobra.Command{
	Use:   "transfer",
	Short: "Move task sets between machines or repositories via portable archives",
}

var taskExportCmd = &cobra.Command{
	Use:   "export TASK_SET [TASK_SET...]",
	Short: "Export one or more task sets into a single tar.gz archive",
	Args:  cobra.MinimumNArgs(1),
	Run:   runTaskExport,
}

var taskImportCmd = &cobra.Command{
	Use:   "import ARCHIVE",
	Short: "Import a task set export into this repository's task storage",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskImport,
}

var (
	taskExportOutput string
	taskImportAs     string
)

var taskMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move legacy thoughts/issues task sets in this worktree into task storage",
	Args:  cobra.NoArgs,
	Run:   runTaskMigrate,
}

var taskAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "List agent PATH availability, attended assistance, and resolved effort ladders",
	Args:  cobra.NoArgs,
	RunE:  runTaskAgents,
}

var taskBindWorktreeCmd = &cobra.Command{
	Use:   "bind-worktree <set>",
	Short: "Adopt the current checkout (or, with --managed, a managed worktree) as a set's drain target",
	Long: `Set a task set's drain target.

Default mode adopts the current checkout: run from inside the target checkout
and pop drains the named set there. The binding is recorded as provisioned
when the checkout lives under pop's managed-worktree root (pop owns the
directory), and as adopted otherwise; adopted checkouts are kept on abandon.
Use --force to re-point a set that is already bound elsewhere.

--managed forks a pop-owned worktree from the Trunk worktree and records a
provisioned binding before returning — the same eager provisioning as
` + "`register --managed`" + `. It does not need to run from any particular
checkout, only inside the repo. A bare repo with no configured Trunk worktree
requires --trunk <path> on first use (persisted to config.runtime.toml). A set
already bound elsewhere still requires --force, which drops the old binding
forget-only (the old checkout is retained) before provisioning the new one.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskBindWorktree,
}

var taskUnbindWorktreeCmd = &cobra.Command{
	Use:   "unbind-worktree <set>",
	Short: "Release a worktree binding without integrating",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskUnbindWorktree,
}

var taskFoldCmd = &cobra.Command{
	Use:   "fold <set>",
	Short: "Fold a finished set's branch onto the Trunk worktree and release its checkout",
	Long: `Fold a DONE or AWAITING-APPROVAL task set's work onto the Trunk worktree and
release its checkout (ADR-0148, ADR-0156).

Fold rebases the set's branch onto trunk inside the set's own checkout (plain
rebase — merge commits inside the set branch are flattened), then advances
trunk by fast-forward only — trunk is never left mid-rebase and never gains a
pop-authored merge commit. On success it releases the Worktree binding and
applies reference-counted managed-worktree teardown (confirm-gated; --yes skips
only that confirmation). Folding an AWAITING-APPROVAL set is the sign-off: it
completes remaining open HITL tasks after a successful land. It does not push,
does not fetch, and does not archive the set.

Refuses when the set is not DONE or AWAITING-APPROVAL (or is NEEDS-VERIFY under
enabled verification), either worktree is dirty, either carries a live claim,
or the set is bound to the Trunk worktree itself.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskFold,
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskStatusCmd)
	taskRegisterCmd.Flags().BoolVar(&taskRegisterManaged, "managed", false, "Provision a pop-managed worktree forked from the Trunk worktree and bind each newly registered set before returning")
	taskRegisterCmd.Flags().StringVar(&taskRegisterTrunk, "trunk", "", "Mark <path> as this repository's Trunk worktree in config.runtime.toml (required for bare repos on first managed register or bind-worktree)")
	taskRegisterCmd.Flags().BoolVar(&taskRegisterAutoDrain, "auto-drain", false, "Enable the auto-drain consent bit on each newly registered set (default off); `pop tasks auto-drain` and the dashboard `a` toggle remain authoritative afterward")
	taskCmd.AddCommand(taskRegisterCmd)
	taskCmd.AddCommand(taskArchiveCmd)
	taskCmd.AddCommand(taskUnarchiveCmd)
	taskCmd.AddCommand(taskSetPriorityCmd)
	taskAutoDrainCmd.Flags().BoolVar(&taskAutoDrainOff, "off", false, "Clear the auto-drain bit instead of setting it")
	taskCmd.AddCommand(taskAutoDrainCmd)
	taskCmd.AddCommand(taskImplementCmd)
	taskCmd.AddCommand(taskVerifyCmd)
	taskCmd.AddCommand(taskAssistCmd)
	taskCmd.AddCommand(taskResetTaskCmd)
	taskCmd.AddCommand(taskCompleteTaskCmd)
	taskCmd.AddCommand(taskSkipTaskCmd)
	taskCmd.AddCommand(taskStreamCmd)
	taskStreamCmd.Flags().BoolVar(&taskStreamFull, "full", false, "Print all tool payloads verbatim without truncation")
	taskStreamCmd.Flags().BoolVar(&taskStreamRaw, "raw", false, "Decompress and write raw JSONL without rendering (ignores --full)")
	taskStreamCmd.Flags().BoolVar(&taskStreamLast, "last", false, "Show only the most recent attempt per task")
	taskStreamCmd.Flags().BoolVar(&taskStreamToolDetail, "tool-detail", false, "Deepen the timing breakdown to argument-level tool facts")
	taskCmd.AddCommand(taskSpendCmd)
	taskSpendCmd.Flags().BoolVar(&taskSpendJSON, "json", false, "Emit spend data as JSON instead of a table")
	taskCmd.AddCommand(taskShowPathCmd)
	taskCheckoutCmd.Flags().BoolVar(&taskCheckoutLocality, "locality", false, "Print exactly one word, trunk or worktree (the default with no flags)")
	taskCheckoutCmd.Flags().BoolVar(&taskCheckoutJSON, "json", false, "Print the whole checkout as JSON: path, locality, branch, trunk_path (omitted when unresolvable), bare, managed")
	taskCmd.AddCommand(taskCheckoutCmd)
	taskCmd.AddCommand(taskTransferCmd)
	taskTransferCmd.AddCommand(taskExportCmd)
	taskTransferCmd.AddCommand(taskImportCmd)
	taskCmd.AddCommand(taskMigrateCmd)
	taskAgentsCmd.Flags().BoolVar(&taskAgentsModels, "models", false, "Also list each preset's curated model aliases, recommended first")
	taskCmd.AddCommand(taskAgentsCmd)
	taskBindWorktreeCmd.Flags().BoolVar(&taskBindWorktreeForce, "force", false, "Re-point a set already bound elsewhere")
	taskBindWorktreeCmd.Flags().BoolVarP(&taskRunYes, "yes", "y", false, "Skip managed-worktree delete confirmation when rebinding")
	taskBindWorktreeCmd.Flags().BoolVar(&taskBindWorktreeManaged, "managed", false, "Provision a pop-managed worktree forked from the Trunk worktree and bind the set before returning")
	taskBindWorktreeCmd.Flags().StringVar(&taskBindWorktreeTrunk, "trunk", "", "Mark <path> as this repository's Trunk worktree in config.runtime.toml (required for bare repos on first managed bind-worktree)")
	taskCmd.AddCommand(taskBindWorktreeCmd)
	taskUnbindWorktreeCmd.Flags().BoolVar(&taskUnbindWorktreeYes, "yes", false, "Skip confirmation prompt")
	taskCmd.AddCommand(taskUnbindWorktreeCmd)
	taskFoldCmd.Flags().BoolVarP(&taskFoldYes, "yes", "y", false, "Skip managed-worktree delete confirmation after fold")
	taskFoldCmd.Flags().StringArrayVar(&taskAgentPresets, "agent", nil, "Agent preset for fold-conflict assistance (claude, opencode, cursor, codex, pi, kimi), optionally followed by extra agent args")
	taskFoldCmd.Flags().StringVar(&taskAgentCmd, "agent-cmd", "", "Trusted shell prefix for fold-conflict assistance; generated prompt passed as final positional argument")
	taskCmd.AddCommand(taskFoldCmd)

	taskCmd.PersistentFlags().StringVar(&taskProject, "project", "", "Select project by exact picker-visible name")
	taskCmd.PersistentFlags().StringVar(&taskPath, "path", "", "Select project by path (normalized to git checkout root)")
	taskCmd.PersistentFlags().StringVar(&taskDefPath, "task-definition-path", "", "Exact task definition directory (not normalized to git root)")

	taskStatusCmd.Flags().BoolVar(&taskStatusArchived, "archived", false, "Show archived task sets only")
	taskArchiveCmd.Flags().BoolVarP(&taskRunYes, "yes", "y", false, "Archive Done task sets without opening the picker")

	taskImplementCmd.Flags().StringVar(&taskRuntimePath, "task-runtime-path", "", "Git checkout root for task execution (normalized to checkout root)")
	taskImplementCmd.Flags().Var(&taskAllowDirty, "allow-dirty", "Dirty runtime strategy: continue (default), commit-and-continue, stash-and-continue")
	taskImplementCmd.Flags().Lookup("allow-dirty").NoOptDefVal = string(tasks.DirtyRuntimeContinue)
	taskImplementCmd.Flags().StringArrayVar(&taskAgentPresets, "agent", nil, "Agent preset (claude, opencode, cursor, codex, pi, kimi), optionally followed by extra agent args, e.g. \"claude --model opus4.8\"; repeat to define an ordered quota fallback list")
	taskImplementCmd.Flags().StringVar(&taskAgentCmd, "agent-cmd", "", "Trusted shell prefix; final positional argument names the file holding the generated prompt")
	taskImplementCmd.Flags().Var(&taskAgentOutput, "agent-output", "Agent output mode: auto (default), text")
	taskImplementCmd.Flags().IntVar(&taskMaxTries, "max-tries", tasks.DefaultMaxTries, "Maximum started attempts per task")
	taskImplementCmd.Flags().StringVar(&taskTimeout, "timeout", "45m", "Maximum duration per attempt")
	taskImplementCmd.Flags().BoolVarP(&taskRunYes, "yes", "y", false, "Skip confirmation prompt")
	taskImplementCmd.Flags().BoolVar(&taskInWorktree, "in-worktree", false, "Provision a managed worktree forked from the current checkout and drain there")
	taskImplementCmd.Flags().BoolVar(&taskForceRebind, "force-rebind", false, "Re-point a bound set to the current checkout (or retarget --in-worktree)")
	taskImplementCmd.Flags().StringArrayVar(&taskImplementVerifyAgents, "verify-agent", nil, "Verifier agent preset for the in-drain verify phase; repeat to define an ordered fallback list (steers verification independently of --agent)")
	taskImplementCmd.Flags().StringVar(&taskImplementVerifyEffort, "verify-effort", "", "Verifier model-strength tier for the in-drain verify phase: light, standard, or heavy (default heavy)")

	taskVerifyCmd.Flags().StringVar(&taskRuntimePath, "task-runtime-path", "", "Git checkout root for task execution (normalized to checkout root)")
	taskVerifyCmd.Flags().StringVar(&taskVerifyTimeout, "timeout", "45m", "Maximum duration for the Verifier attempt")
	taskVerifyCmd.Flags().StringArrayVar(&taskVerifyAgents, "agent", nil, "Verifier agent preset; repeat to define an ordered quota/missing-binary fallback list")
	taskVerifyCmd.Flags().StringVar(&taskVerifyEffort, "effort", "", "Verifier model-strength tier: light, standard, or heavy (default heavy)")
	taskVerifyCmd.Flags().StringVar(&taskVerifyAccept, "accept", "", "Accept a non-PASS verdict: record a human-authored PASS at the current work SHA carrying this note (skips the Verifier); the note feeds forward as context into later verifier prompts")
	taskVerifyCmd.Flags().StringVar(&taskVerifyRemediate, "remediate", "", "Remediate a non-PASS verdict: spawn a Remediation task from the set's findings carrying this note (skips the Verifier), even from NEEDS-HUMAN or past the remediation depth cap; the Drain then picks it up")

	taskAssistCmd.Flags().StringVar(&taskRuntimePath, "task-runtime-path", "", "Git checkout root for task execution (normalized to checkout root)")
	taskAssistCmd.Flags().StringArrayVar(&taskAgentPresets, "agent", nil, "Agent preset for attended assistance (claude, opencode, cursor, codex, pi, kimi), optionally followed by extra agent args")
	taskAssistCmd.Flags().StringVar(&taskAgentCmd, "agent-cmd", "", "Trusted shell prefix; generated prompt passed as final positional argument")

	taskExportCmd.Flags().StringVarP(&taskExportOutput, "output", "o", "", "Output archive path (default: <task-set-id>.tar.gz in the current directory)")
	taskImportCmd.Flags().StringVar(&taskImportAs, "as", "", "Install under a different task set identifier")
}

func taskResolveInput() tasks.ResolveInput {
	return tasks.ResolveInput{
		ProjectName:        taskProject,
		Path:               taskPath,
		DefinitionOverride: taskDefPath,
		RuntimeOverride:    taskRuntimePath,
		CWD:                cmdLayerDeps().WorkDir(),
	}
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	var taskSetID string
	if len(args) > 0 {
		taskSetID = args[0]
	}
	return runTaskStatusWith(cmdLayerDeps().tasksDeps(), os.Stdout, taskSetID)
}

func runTaskRegister(cmd *cobra.Command, args []string) error {
	var taskSetID string
	if len(args) > 0 {
		taskSetID = args[0]
	}
	return runTaskRegisterWith(cmdLayerDeps().tasksDeps(), os.Stdout, taskSetID)
}

// runTaskRegisterWith is the sole entry point that registers discovered task
// sets (ADR-0061): it activates newly authored on-disk sets — assigning order and
// eagerly binding the current checkout (ADR-0115) — warns about any retired
// manifest keys, and then prints status exactly like `pop tasks status`. Run from
// inside the repo so the cwd is a valid checkout. A read (status/dashboard) never
// registers.
func runTaskRegisterWith(d *tasks.Deps, w io.Writer, taskSetID string) error {
	resolved, err := tasks.ResolvePathsWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput())
	if err != nil {
		return fmt.Errorf("tasks register: %w", err)
	}

	runtimePath, runtimeErr := tasks.ResolveRuntimePathWith(d, resolved.ProjectPath, taskRuntimePath)
	cfg, cfgErr := taskConfigLoad(taskConfigPath())

	var trunkPath string
	if taskRegisterManaged {
		if runtimeErr != nil {
			return fmt.Errorf("tasks register: %w", runtimeErr)
		}
		if cfgErr != nil {
			return fmt.Errorf("tasks register: %w", cfgErr)
		}
		var trunkErr error
		trunkPath, trunkErr = resolveManagedTrunk(d, cfg, runtimePath, taskRegisterTrunk)
		if trunkErr != nil {
			return fmt.Errorf("tasks register: %w", trunkErr)
		}
	}

	result, err := tasks.RegisterWith(d, resolved.DefinitionPath, tasks.StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return fmt.Errorf("tasks register: %w", err)
	}

	if taskRegisterManaged && len(result.NewRegistrationIDs) > 0 {
		if err := eagerProvisionManagedNewRegistrations(d, taskProjectDeps(), cfg, trunkPath, runtimePath, result.NewRegistrationIDs); err != nil {
			_ = tasks.RemoveRegisteredTaskSets(d, result.DefinitionPath, result.NewRegistrationIDs)
			return fmt.Errorf("tasks register: %w", err)
		}
	}

	// Retired manifest keys (worktree/auto_drain) still register successfully but
	// are ignored (ADR-0115): warn so a legacy manifest's author learns the keys no
	// longer take effect. The set is never MALFORMED for carrying them.
	warnDeprecatedManifestKeys(w, result)

	// --auto-drain sets the consent bit on each set this register just activated,
	// reusing the same primitive `pop tasks auto-drain` writes through. Off by
	// default: unattended draining stays opt-in per invocation. It runs only for
	// first-time registrations, mirroring eagerBindNewRegistrations, and the
	// dashboard `a` toggle / `pop tasks auto-drain` remain authoritative afterward.
	if taskRegisterAutoDrain {
		applyRegisterAutoDrain(d, resolved.DefinitionPath, result.NewRegistrationIDs, w)
	}

	// Resolve the runtime checkout once (see runTaskStatusWith): Binding-first
	// per-set resolution feeds the SHA-gated Verify-verdict pass (ADR-0146);
	// the overview's runtime-lock/checkout badges still describe the current
	// checkout. Register prints status exactly like `pop tasks status`.
	if runtimeErr == nil {
		if cfg == nil {
			cfg, _ = taskConfigLoad(taskConfigPath())
		}
		applyBindingFirstVerifyVerdicts(d, result, cfg, runtimePath)
		// Eagerly bind the current checkout to each newly-registered set
		// (ADR-0115): the binding is materialized and visible the moment the set
		// registers, with no drain required. --managed provisions its own
		// worktree from the Trunk worktree instead (ADR-0147).
		if !taskRegisterManaged {
			eagerBindNewRegistrations(d, cfg, runtimePath, result.NewRegistrationIDs, w)
			// Said once, right after the bind that provokes it: a set that both
			// consents to unattended draining and just bound to the Trunk
			// worktree is the one consequential shape the here-by-default rule
			// produces (ADR-0192).
			if taskRegisterAutoDrain && len(result.NewRegistrationIDs) > 0 {
				warnTrunkAutoDrain(d, w, runtimePath)
			}
		}
	}

	// With a set argument, drill into that one set's per-task breakdown after
	// registering; absent, render the whole-repo overview.
	if strings.TrimSpace(taskSetID) != "" {
		id, err := tasks.ResolveTaskSetTarget(result, taskSetID)
		if err != nil {
			return fmt.Errorf("tasks register: %w", err)
		}
		tasks.RenderTaskSetDetail(w, id, tasks.FindRow(result, id), result.Manifests[id])
		return nil
	}

	if runtimeErr == nil {
		result.RuntimeLock = tasks.ReadRuntimeLockStatus(d, runtimePath)
		if linked, err := binding.IsLinkedWorktree(d, runtimePath); err == nil {
			cs := &tasks.CheckoutStatus{Path: runtimePath, Worktree: linked}
			if linked {
				cs.Branch = binding.CurrentBranch(d, runtimePath)
			}
			result.Checkout = cs
		}
	}

	attachWorktreeDirectiveErrors(d, resolved.ProjectPath, result.Rows)

	tasks.Render(w, result)
	return nil
}

// eagerBindNewRegistrations adopts the current checkout as the Worktree binding
// for each set this register just activated (ADR-0115). It reuses the operator
// bind-worktree adopt path (binding.BindWorktree): the Provisioned bit is
// derived from checkoutPath, so a checkout under the managed-worktree root is
// recorded as provisioned while any other checkout is adopted. It runs only for
// first-time registrations, so re-registering a set — including from a different
// checkout — never rebinds; a set that already carries a binding is skipped, and
// explicit `pop tasks bind-worktree --force` remains the rebind path. Binding is
// best-effort: a failure is warned, not fatal, so a checkout register cannot
// itself bind (e.g. detached HEAD) still registers.
func eagerBindNewRegistrations(d *tasks.Deps, cfg *config.Config, checkoutPath string, newSetIDs []string, w io.Writer) {
	for _, setID := range newSetIDs {
		if _, _, bound, err := binding.FindBySetID(d, setID); err != nil {
			fmt.Fprintf(w, "warning: could not check worktree binding for %s: %v\n", setID, err)
			continue
		} else if bound {
			continue
		}
		if _, err := binding.BindWorktree(d, taskProjectDeps(), cfg, setID, checkoutPath, binding.BindWorktreeOptions{}, binding.LifecycleHooks{}, io.Discard); err != nil {
			fmt.Fprintf(w, "warning: could not bind %s to current checkout: %v\n", setID, err)
		}
	}
}

// applyRegisterAutoDrain enables the auto-drain consent bit for each set this
// register just activated (--auto-drain). It writes through
// tasks.SetTaskSetAutoDrain — the same primitive SetAutoDrainWith uses to back
// `pop tasks auto-drain` — rather than a parallel writer. Best-effort: a
// failure is warned, not fatal, so it cannot itself fail a register.
func applyRegisterAutoDrain(d *tasks.Deps, defPath string, newSetIDs []string, w io.Writer) {
	for _, setID := range newSetIDs {
		if _, err := tasks.SetTaskSetAutoDrain(d, defPath, setID, true); err != nil {
			fmt.Fprintf(w, "warning: could not enable auto-drain for %s: %v\n", setID, err)
		}
	}
}

// warnTrunkAutoDrain names the hazard the here-by-default registration rule
// deliberately makes ordinary (ADR-0192): the set now consents to unattended
// draining and is bound to the Trunk worktree, so the Work daemon may commit and
// open panes on the branch the human is standing on. Callers gate on the consent
// bit and on no managed worktree having been asked for; this adds the locality
// half, read through checkoutLocality — the same predicate `pop tasks checkout`
// renders, so the warning cannot disagree with the binding it describes. A
// checkout whose locality will not resolve is silent: this is output, never a
// reason to fail a registration.
func warnTrunkAutoDrain(d *tasks.Deps, w io.Writer, checkoutPath string) {
	locality, _, err := checkoutLocality(d, checkoutPath)
	if err != nil || locality != localityTrunk {
		return
	}
	fmt.Fprintf(w, "warning: auto-drain is on and this set is bound to the Trunk worktree at %s: the Work daemon may drain it unattended here, committing and opening panes on the branch you are standing on; re-register with 'managed' (--managed) for an isolated worktree instead\n", checkoutPath)
}

// warnDeprecatedManifestKeys prints a deprecation warning for each set whose
// manifest still carries a retired set-level key (worktree/auto_drain), naming
// the ignored keys (ADR-0115). Binding is now materialized eagerly at register
// and auto-drain is a CLI/dashboard concern; the keys are read no longer. Sets
// are iterated in id order for stable output.
func warnDeprecatedManifestKeys(w io.Writer, result *tasks.RefreshResult) {
	ids := make([]string, 0, len(result.Manifests))
	for id := range result.Manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := result.Manifests[id]
		if m == nil || len(m.DeprecatedKeys) == 0 {
			continue
		}
		fmt.Fprintf(w, "warning: %s: manifest key(s) %s are no longer read and are ignored; binding is set at register and auto-drain via 'pop tasks auto-drain' or the dashboard (ADR-0115)\n",
			id, strings.Join(m.DeprecatedKeys, ", "))
	}
}

var taskConfigLoad = func(path string) (*config.Config, error) {
	return config.Load(path)
}

// taskConfigPath returns the global config.toml path using cmd-layer deps so
// tests with isolated XDG_CONFIG_HOME route consistently.
func taskConfigPath() string {
	return config.DefaultConfigPathWith(cmdLayerDeps().configDeps())
}

func runTaskStatusWith(d *tasks.Deps, w io.Writer, taskSetID string) error {
	resolved, err := tasks.ResolvePathsWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput())
	if err != nil {
		return fmt.Errorf("tasks status: %w", err)
	}

	var result *tasks.RefreshResult
	if taskStatusArchived {
		result, err = tasks.RefreshArchivedWith(d, resolved.DefinitionPath, tasks.StatePathFor(resolved.DefinitionPath))
	} else {
		result, err = tasks.RefreshWith(d, resolved.DefinitionPath, tasks.StatePathFor(resolved.DefinitionPath))
	}
	if err != nil {
		return fmt.Errorf("tasks status: %w", err)
	}

	// Binding-first per-set resolution (ADR-0146) feeds the SHA-gated Verify
	// verdict pass so a bound set derives status at its Worktree binding — the
	// same checkout the Queue dashboard and `pop work status` use — rather than
	// the invoking cwd. Overview runtime-lock and checkout badges still describe
	// the current checkout.
	runtimePath, runtimeErr := tasks.ResolveRuntimePathWith(d, resolved.ProjectPath, taskRuntimePath)
	if runtimeErr == nil {
		cfg, _ := taskConfigLoad(config.DefaultConfigPath())
		applyBindingFirstVerifyVerdicts(d, result, cfg, runtimePath)
	}

	// A set argument drills into that one set's per-task breakdown; absent, the
	// no-arg overview lists every set. ResolveTaskSetTarget rejects file and
	// path forms and errors with the valid identifiers on an unknown set.
	if strings.TrimSpace(taskSetID) != "" {
		id, err := tasks.ResolveTaskSetTarget(result, taskSetID)
		if err != nil {
			return fmt.Errorf("tasks status: %w", err)
		}
		tasks.RenderTaskSetDetail(w, id, tasks.FindRow(result, id), result.Manifests[id])
		return nil
	}

	if runtimeErr == nil {
		result.RuntimeLock = tasks.ReadRuntimeLockStatus(d, runtimePath)
		if linked, err := binding.IsLinkedWorktree(d, runtimePath); err == nil {
			cs := &tasks.CheckoutStatus{Path: runtimePath, Worktree: linked}
			if linked {
				cs.Branch = binding.CurrentBranch(d, runtimePath)
			}
			result.Checkout = cs
		}
	}

	attachWorktreeDirectiveErrors(d, resolved.ProjectPath, result.Rows)

	tasks.Render(w, result)
	return nil
}

// attachWorktreeDirectiveErrors surfaces an unsatisfiable worktree directive
// (ADR-0059) as a config/registration-class error on each Ready set's status row.
// The probe is read-only — it never provisions — so a `managed` set with no
// resolvable trunk, or a `name` set with no such worktree on this machine, shows
// the fault in `pop tasks status` without the drain ever running. Only the two
// directive sentinels become a config error; incidental resolution failures are
// ignored so status still renders.
func attachWorktreeDirectiveErrors(d *tasks.Deps, checkout string, rows []tasks.Row) {
	cfg, _ := taskConfigLoad(config.DefaultConfigPath())
	for i := range rows {
		if rows[i].Status != tasks.StatusReady {
			continue
		}
		err := binding.ProbeWorktreeDirective(d, taskProjectDeps(), cfg, checkout, rows[i].ID)
		if errors.Is(err, binding.ErrNoResolvableTrunk) || errors.Is(err, binding.ErrNamedWorktreeNotFound) {
			rows[i].ConfigError = err.Error()
		}
	}
}

func runTaskArchive(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return runTaskArchiveWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0])
	}
	return runTaskArchiveSelectionWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stdin, taskRunYes)
}

func runTaskArchiveWith(d *tasks.Deps, w io.Writer, taskSetID string) error {
	return runTaskArchiveWithConfirm(d, w, os.Stdin, taskRunYes, taskSetID)
}

func runTaskArchiveWithConfirm(d *tasks.Deps, w io.Writer, stdin io.Reader, yes bool, taskSetID string) error {
	cfg, err := taskConfigLoad(config.DefaultConfigPath())
	if err != nil {
		return err
	}
	if err := binding.PrepareManagedWorktreesForArchive(d, taskProjectDeps(), cfg, []string{taskSetID}, binding.ArchiveConfirmOptions{
		Yes: yes,
		In:  stdin,
		Out: w,
	}); errors.Is(err, binding.ErrArchiveCancelled) {
		return nil
	} else if err != nil {
		return fmt.Errorf("tasks archive: %w", err)
	}
	result, err := tasks.ArchiveTaskSetWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), taskSetID)
	if err != nil {
		return fmt.Errorf("tasks archive: %w", err)
	}
	fmt.Fprintf(w, "Archived task set %s\n\n", result.TaskSetID)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskArchiveSelectionWith(d *tasks.Deps, w io.Writer, stdin io.Reader, yes bool) error {
	ctx, err := tasks.LoadArchiveSetSelectionWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput())
	if err != nil {
		return fmt.Errorf("tasks archive: %w", err)
	}

	var selectedIDs []string
	if yes {
		selectedIDs = tasks.DoneArchiveSetIDs(ctx.Rows)
		if len(selectedIDs) == 0 {
			fmt.Fprintln(w, "No done task sets to archive.")
			return nil
		}
	} else {
		if !taskStdinInteractive(stdin) {
			return &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
				"archiving task sets needs an interactive terminal; pass --yes to archive Done sets or target one task set by bare identifier")}
		}
		items := make([]ui.MultiSelectItem, len(ctx.Rows))
		for i, row := range ctx.Rows {
			items[i] = ui.MultiSelectItem{
				Label:   archiveSetRowLabel(row),
				Checked: row.Checked,
			}
		}
		selection, err := runTaskMultiSelect("Archive task sets", items)
		if err != nil {
			return err
		}
		if !selection.Confirmed {
			return nil
		}
		for _, idx := range selection.Checked {
			if idx >= 0 && idx < len(ctx.Rows) {
				selectedIDs = append(selectedIDs, ctx.Rows[idx].TaskSetID)
			}
		}
		if len(selectedIDs) == 0 {
			return nil
		}
	}

	cfg, err := taskConfigLoad(config.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("tasks archive: %w", err)
	}
	if err := binding.PrepareManagedWorktreesForArchive(d, taskProjectDeps(), cfg, selectedIDs, binding.ArchiveConfirmOptions{
		Yes: yes,
		In:  stdin,
		Out: w,
	}); errors.Is(err, binding.ErrArchiveCancelled) {
		return nil
	} else if err != nil {
		return fmt.Errorf("tasks archive: %w", err)
	}

	result, err := tasks.ArchiveTaskSetsWith(d, taskProjectDeps(), taskConfigLoad, tasks.ArchiveTaskSetsOptions{
		ResolveInput: taskResolveInput(),
		TaskSetIDs:   selectedIDs,
	})
	if err != nil {
		return fmt.Errorf("tasks archive: %w", err)
	}
	fmt.Fprintf(w, "Archived task set")
	if len(result.TaskSetIDs) != 1 {
		fmt.Fprint(w, "s")
	}
	fmt.Fprintf(w, " %s\n\n", strings.Join(result.TaskSetIDs, ", "))
	tasks.Render(w, result.Refresh)
	return nil
}

func archiveSetRowLabel(r tasks.ArchiveSetSelectionRow) string {
	return fmt.Sprintf("%-10s %s", "["+string(r.Status)+"]", r.TaskSetID)
}

func runTaskUnarchive(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return runTaskUnarchiveWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0])
	}
	return runTaskUnarchiveSelectionWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stdin)
}

func runTaskUnarchiveWith(d *tasks.Deps, w io.Writer, taskSetID string) error {
	result, err := tasks.UnarchiveTaskSetWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), taskSetID)
	if err != nil {
		return fmt.Errorf("tasks unarchive: %w", err)
	}
	fmt.Fprintf(w, "Unarchived task set %s\n\n", result.TaskSetID)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskUnarchiveSelectionWith(d *tasks.Deps, w io.Writer, stdin io.Reader) error {
	ctx, err := tasks.LoadUnarchiveSetSelectionWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput())
	if err != nil {
		return fmt.Errorf("tasks unarchive: %w", err)
	}

	if !taskStdinInteractive(stdin) {
		return &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"unarchiving task sets needs an interactive terminal; target one task set by bare identifier, e.g. `pop tasks unarchive <task-set>`")}
	}

	items := make([]ui.MultiSelectItem, len(ctx.Rows))
	for i, row := range ctx.Rows {
		items[i] = ui.MultiSelectItem{
			Label:   archiveSetRowLabel(row),
			Checked: row.Checked,
		}
	}
	selection, err := runTaskMultiSelect("Unarchive task sets", items)
	if err != nil {
		return err
	}
	if !selection.Confirmed {
		return nil
	}
	var selectedIDs []string
	for _, idx := range selection.Checked {
		if idx >= 0 && idx < len(ctx.Rows) {
			selectedIDs = append(selectedIDs, ctx.Rows[idx].TaskSetID)
		}
	}
	if len(selectedIDs) == 0 {
		return nil
	}

	result, err := tasks.UnarchiveTaskSetsWith(d, taskProjectDeps(), taskConfigLoad, tasks.UnarchiveTaskSetsOptions{
		ResolveInput: taskResolveInput(),
		TaskSetIDs:   selectedIDs,
	})
	if err != nil {
		return fmt.Errorf("tasks unarchive: %w", err)
	}
	fmt.Fprintf(w, "Unarchived task set")
	if len(result.TaskSetIDs) != 1 {
		fmt.Fprint(w, "s")
	}
	fmt.Fprintf(w, " %s\n\n", strings.Join(result.TaskSetIDs, ", "))
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskSetPriority(cmd *cobra.Command, args []string) error {
	return runTaskSetPriorityWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0], args[1])
}

func runTaskSetPriorityWith(d *tasks.Deps, w io.Writer, taskSetID, priorityArg string) error {
	priority, err := strconv.Atoi(priorityArg)
	if err != nil {
		return fmt.Errorf("tasks set-priority: invalid priority %q: %w", priorityArg, err)
	}

	result, err := tasks.SetPriorityWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), taskSetID, priority)
	if err != nil {
		return fmt.Errorf("tasks set-priority: %w", err)
	}

	tasks.RenderPriorityUpdate(w, result.TaskSetID, result.OldPriority, result.NewPriority)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskAutoDrain(cmd *cobra.Command, args []string) error {
	return runTaskAutoDrainWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0], !taskAutoDrainOff)
}

func runTaskAutoDrainWith(d *tasks.Deps, w io.Writer, taskSetID string, enabled bool) error {
	result, err := tasks.SetAutoDrainWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), taskSetID, enabled)
	if err != nil {
		return fmt.Errorf("tasks auto-drain: %w", err)
	}

	tasks.RenderAutoDrainUpdate(w, result.TaskSetID, result.AutoDrain)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskVerify(cmd *cobra.Command, args []string) error {
	return runTaskVerifyWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0],
		cmd.Flags().Changed("accept"), taskVerifyAccept,
		cmd.Flags().Changed("remediate"), taskVerifyRemediate)
}

func runTaskAssist(cmd *cobra.Command, args []string) error {
	return runTaskAssistWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stdin, args[0])
}

func runTaskAssistWith(d *tasks.Deps, w io.Writer, stdin io.Reader, taskSetID string) error {
	resolveInput, err := bindingFirstVerifyResolveInput(d, taskSetID)
	if err != nil {
		return fmt.Errorf("tasks assist: %w", err)
	}
	if err := tasks.AssistTaskSetWith(d, taskProjectDeps(), taskConfigLoad, tasks.AssistOptions{
		ResolveInput: resolveInput,
		TaskSetID:    taskSetID,
		AgentPreset:  selectedTaskAgentPreset(),
		AgentCmd:     taskAgentCmd,
		Output:       w,
		Input:        stdin,
		Fold:         assistFold(d),
	}); err != nil {
		return fmt.Errorf("tasks assist: %w", err)
	}
	return nil
}

// assistFold gives the Assist menu the same fold `pop tasks fold` performs, run
// in-process. Assist used to re-exec `pop tasks fold`, which routed its refusal
// through the root command's error screen and left the menu with nothing but an
// exit status to report.
func assistFold(d *tasks.Deps) tasks.AssistFold {
	return func(setID string, in io.Reader, out io.Writer) error {
		cfgPath := cfgFile
		if cfgPath == "" {
			cfgPath = config.DefaultConfigPath()
		}
		cfg, err := taskConfigLoad(cfgPath)
		if err != nil {
			return err
		}
		_, err = binding.Fold(d, taskProjectDeps(), cfg, setID, binding.FoldOptions{
			In:          in,
			AgentPreset: selectedTaskAgentPreset(),
			AgentCmd:    taskAgentCmd,
		}, binding.LifecycleHooks{}, out)
		return err
	}
}

func runTaskVerifyWith(d *tasks.Deps, w io.Writer, taskSetID string, accept bool, acceptNote string, remediate bool, remediateNote string) error {
	if accept && remediate {
		return fmt.Errorf("tasks verify: --accept and --remediate are mutually exclusive")
	}
	timeout, err := time.ParseDuration(taskVerifyTimeout)
	if err != nil {
		return fmt.Errorf("tasks verify: invalid --timeout %q: %w", taskVerifyTimeout, err)
	}
	// One disposition is active at a time (guarded above), so the single Note
	// field carries whichever note was supplied.
	note := acceptNote
	if remediate {
		note = remediateNote
	}
	resolveInput, err := bindingFirstVerifyResolveInput(d, taskSetID)
	if err != nil {
		return fmt.Errorf("tasks verify: %w", err)
	}
	if _, err := tasks.VerifyTaskSetWith(d, taskProjectDeps(), taskConfigLoad, tasks.VerifyOptions{
		ResolveInput: resolveInput,
		TaskSetID:    taskSetID,
		Agents:       append([]string(nil), taskVerifyAgents...),
		Effort:       taskVerifyEffort,
		Timeout:      timeout,
		Output:       w,
		Accept:       accept,
		Remediate:    remediate,
		Note:         note,
	}); err != nil {
		return fmt.Errorf("tasks verify: %w", err)
	}
	return nil
}

// bindingFirstVerifyResolveInput pins verify's RuntimeOverride through
// Binding-first runtime resolution (ADR-0146) so accept / remediate / re-run
// all act at the set's Worktree binding when bound — not the invoking cwd.
// An explicit --task-runtime-path still wins.
func bindingFirstVerifyResolveInput(d *tasks.Deps, taskSetID string) (tasks.ResolveInput, error) {
	in := taskResolveInput()
	resolved, err := tasks.ResolvePathsWith(d, taskProjectDeps(), taskConfigLoad, in)
	if err != nil {
		return in, err
	}
	runtime, err := binding.ResolveCommandRuntime(d, resolved.ProjectPath, taskSetID, in.RuntimeOverride)
	if err != nil {
		return in, err
	}
	in.RuntimeOverride = runtime
	return in, nil
}

// applyBindingFirstVerifyVerdicts re-derives status through Binding-first
// per-set runtime resolution (ADR-0146), matching the Queue dashboard.
func applyBindingFirstVerifyVerdicts(d *tasks.Deps, result *tasks.RefreshResult, cfg *config.Config, currentCheckout string) {
	resolver, _, err := binding.CommandRuntimeResolver(d, currentCheckout)
	if err != nil {
		tasks.ApplyVerifyVerdicts(d, result, cfg, currentCheckout)
		return
	}
	tasks.ApplyVerifyVerdictsWith(d, result, cfg, resolver)
}

func runTaskImplement(cmd *cobra.Command, args []string) {
	var target string
	if len(args) > 0 {
		target = args[0]
	}
	// Explicitness, not the resolved value, decides whether --agent supplies
	// the fallback list or config/default fallback should be used.
	agentExplicit := cmd.Flags().Changed("agent")
	maxTriesExplicit := cmd.Flags().Changed("max-tries")
	var err error
	if isTaskFileTarget(target) {
		err = runTaskRunTaskWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stderr, os.Stdin, target, agentExplicit, maxTriesExplicit)
	} else {
		err = runTaskRunTasksWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stderr, os.Stdin, target, agentExplicit, maxTriesExplicit)
	}
	handleTaskExit(err)
}

// isTaskFileTarget reports whether a Task target reference names a single task —
// a Task-set-relative file reference such as "<task-set>/<file>.md" — rather than
// a bare Task set identifier. The ".md" suffix is the discriminator: it is exactly
// the file-reference form, so a single task runs only when a file names it; a bare
// set identifier or an empty target (no argument) drains an auto-selected set.
// Malformed forms still route in and are rejected by the executor's own validation.
func isTaskFileTarget(target string) bool {
	return strings.HasSuffix(target, ".md")
}

// taskBindCheckout returns the binding hook `pop tasks implement` passes to the
// executor. It adopts the run's current checkout into the binding model
// (ADR-0036): a worktree-locus run records a binding via the shared module, with
// the Provisioned bit derived from the checkout's location, while a trunk-locus run
// records nothing. `implement` never provisions a worktree — auto-provisioning
// stays the Queue's path.
func taskBindCheckout(d *tasks.Deps) func(setID, projectPath, runtimePath string) error {
	return func(setID, projectPath, runtimePath string) error {
		cfg, _ := taskConfigLoad(config.DefaultConfigPath())
		_, err := binding.AdoptCurrentCheckout(d, taskProjectDeps(), cfg, projectPath, runtimePath, setID)
		return err
	}
}

// taskPreSeedTopic returns the drain pre-seed hook `pop tasks implement` passes
// to the executor (ADR-0058): at drain spawn it slugifies the task Title into
// pop's canonical Topic format and writes the current pane's @pop_topic, so the
// agent's `set-topic --derive` hook no-ops and the drained pane carries an
// accurate Topic with no model call.
func taskPreSeedTopic() func(taskTitle string) {
	maxWords := config.DefaultTopicWords
	if cfg, err := taskConfigLoad(config.DefaultConfigPath()); err == nil && cfg != nil {
		maxWords = cfg.PaneMonitoringTopicWords()
	}
	return preSeedTopicFromTitle(defaultTmuxMod, maxWords)
}

func runTaskRunTaskWith(d *tasks.Deps, stdout, stderr io.Writer, stdin io.Reader, taskPath string, agentExplicit, maxTriesExplicit bool) error {
	timeout, err := time.ParseDuration(taskTimeout)
	if err != nil {
		return fmt.Errorf("tasks implement: invalid --timeout %q: %w", taskTimeout, err)
	}
	result, err := tasks.RunTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.RunTaskOptions{
		ResolveInput:     taskResolveInput(),
		TaskPathOverride: taskPath,
		AgentPreset:      selectedTaskAgentPreset(),
		AgentPresets:     selectedTaskAgentPresets(),
		AgentExplicit:    agentExplicit,
		AgentCmd:         taskAgentCmd,
		AgentOutput:      taskAgentOutput,
		AllowDirty:       taskAllowDirty,
		MaxTries:         taskMaxTries,
		MaxTriesExplicit: maxTriesExplicit,
		Timeout:          timeout,
		Yes:              taskRunYes,
		ConfirmIn:        stdin,
		ConfirmOut:       stderr,
		Output:           stdout,
		BindCheckout:     taskBindCheckout(d),
		PreSeedTopic:     taskPreSeedTopic(),
	})
	if err != nil {
		return err
	}
	return tasks.QuotaPausedExit(result != nil && result.QuotaPaused)
}

func runTaskRunTasksWith(d *tasks.Deps, stdout, stderr io.Writer, stdin io.Reader, taskSetPath string, agentExplicit, maxTriesExplicit bool) error {
	timeout, err := time.ParseDuration(taskTimeout)
	if err != nil {
		return fmt.Errorf("tasks implement: invalid --timeout %q: %w", taskTimeout, err)
	}
	impl := implement.DefaultDeps()
	impl.Tasks = d
	impl.Project = taskProjectDeps()
	impl.LoadConfig = taskConfigLoad
	impl.StdinInteractive = taskStdinInteractive
	_, err = implement.RunWholeSetWith(impl, implement.WholeSetOptions{
		ResolveInput:     taskResolveInput(),
		TaskSetOverride:  taskSetPath,
		InWorktree:       taskInWorktree,
		ForceRebind:      taskForceRebind,
		AgentPreset:      selectedTaskAgentPreset(),
		AgentPresets:     selectedTaskAgentPresets(),
		AgentExplicit:    agentExplicit,
		AgentCmd:         taskAgentCmd,
		AgentOutput:      taskAgentOutput,
		AllowDirty:       taskAllowDirty,
		MaxTries:         taskMaxTries,
		MaxTriesExplicit: maxTriesExplicit,
		Timeout:          timeout,
		VerifyAgents:     append([]string(nil), taskImplementVerifyAgents...),
		VerifyEffort:     taskImplementVerifyEffort,
		Yes:              taskRunYes,
		ConfirmIn:        stdin,
		ConfirmOut:       stderr,
		Output:           stdout,
		PreSeedTopic:     taskPreSeedTopic(),
	})
	return err
}

func selectedTaskAgentPresets() []string {
	if len(taskAgentPresets) > 0 {
		return append([]string(nil), taskAgentPresets...)
	}
	if strings.TrimSpace(taskAgentPreset) != "" {
		return []string{taskAgentPreset}
	}
	return nil
}

func selectedTaskAgentPreset() string {
	if specs := selectedTaskAgentPresets(); len(specs) > 0 {
		return specs[0]
	}
	return tasks.DefaultAgentPreset
}

func runTaskResetTask(cmd *cobra.Command, args []string) {
	target := args[0]
	var err error
	if isTaskFileTarget(target) {
		// A <task-set>/<file>.md reference reopens exactly one task, no prompt.
		err = runTaskResetTaskWith(cmdLayerDeps().tasksDeps(), os.Stdout, target)
	} else {
		// A whole-set target opens the interactive Multi-task selection.
		err = runTaskOpenTasksWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stdin, target)
	}
	handleTaskExit(err)
}

func runTaskResetTaskWith(d *tasks.Deps, w io.Writer, taskPath string) error {
	result, err := tasks.ResetTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.ResetTaskOptions{
		ResolveInput: taskResolveInput(),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	tasks.RenderTaskReset(w, result.TaskSetID, result.TaskID)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskOpenTasksWith(d *tasks.Deps, w io.Writer, stdin io.Reader, target string) error {
	ctx, err := tasks.LoadOpenSelectionWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), target)
	if err != nil {
		return err
	}

	// A whole-set target with no interactive TTY is rejected with a pointer to
	// the file-reference form, never a silent mass mutation (ADR 0020).
	if !taskStdinInteractive(stdin) {
		return &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"reopening a whole task set needs an interactive terminal; target one task with %s/<file>.md instead", ctx.TaskSetID)}
	}

	items := make([]ui.MultiSelectItem, len(ctx.Rows))
	for i, r := range ctx.Rows {
		items[i] = ui.MultiSelectItem{
			Label:      selectionRowLabel(r),
			Locked:     r.Locked,
			LockedMark: r.LockedMark,
		}
	}

	selection, err := runTaskMultiSelect(fmt.Sprintf("Reopen tasks in %s", ctx.TaskSetID), items)
	if err != nil {
		return err
	}
	if !selection.Confirmed {
		return nil // Esc cancels: zero writes.
	}

	var selectedIDs []string
	for _, idx := range selection.Checked {
		if idx >= 0 && idx < len(ctx.Rows) {
			selectedIDs = append(selectedIDs, ctx.Rows[idx].TaskID)
		}
	}
	if len(selectedIDs) == 0 {
		return nil // Empty selection: clean no-op exit.
	}

	result, err := tasks.OpenTasksWith(d, taskProjectDeps(), taskConfigLoad, tasks.OpenTasksOptions{
		ResolveInput:    taskResolveInput(),
		TaskSetTarget:   target,
		SelectedTaskIDs: selectedIDs,
	})
	if err != nil {
		return err
	}

	tasks.RenderTaskOpenBatch(w, result.TaskSetID, result.Transitions)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskCompleteTask(cmd *cobra.Command, args []string) {
	target := args[0]
	var err error
	if isTaskFileTarget(target) {
		// A <task-set>/<file>.md reference moves exactly one task, no prompt.
		err = runTaskCompleteTaskWith(cmdLayerDeps().tasksDeps(), os.Stdout, target)
	} else {
		// A whole-set target opens the interactive Multi-task selection.
		err = runTaskCompleteTasksWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stdin, target)
	}
	handleTaskExit(err)
}

func runTaskCompleteTaskWith(d *tasks.Deps, w io.Writer, taskPath string) error {
	result, err := tasks.CompleteTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.CompleteTaskOptions{
		ResolveInput: taskResolveInput(),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	tasks.RenderTaskComplete(w, result.TaskSetID, result.TaskID)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

// runTaskMultiSelect runs the interactive Multi-task selection shared by every
// whole-set verb. It is a package variable so tests can drive selection without
// a real terminal.
var runTaskMultiSelect = func(title string, items []ui.MultiSelectItem) (ui.MultiSelectResult, error) {
	return ui.RunMultiSelect(title, items)
}

// taskStdinInteractive reports whether stdin is an interactive terminal. It is a
// package variable so tests can simulate either case.
var taskStdinInteractive = func(stdin io.Reader) bool {
	f, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// taskStdoutInteractive reports whether stdout is an interactive terminal. It is a
// package variable so tests can simulate either case for pager logic.
var taskStdoutInteractive = func() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// taskOpenPager starts the user's pager (respecting $PAGER, defaulting to
// less -F -R) and returns a WriteCloser connected to the pager's stdin, plus
// a done function that closes the pipe and waits for the pager to finish.
// It is a package variable so tests can mock the pager or bypass it.
var taskOpenPager = func() (io.WriteCloser, func() error, error) {
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -F -R"
	}
	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	pw, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		pw.Close()
		return nil, nil, err
	}
	return pw, func() error {
		pw.Close()
		return cmd.Wait()
	}, nil
}

// selectionRowLabel renders one Multi-task selection row's display label,
// shared across verbs.
func selectionRowLabel(r tasks.SelectionRow) string {
	label := fmt.Sprintf("%-9s %s", "["+string(r.Status)+"]", r.File)
	if r.Title != "" {
		label += "  " + r.Title
	}
	return label
}

func runTaskCompleteTasksWith(d *tasks.Deps, w io.Writer, stdin io.Reader, target string) error {
	ctx, err := tasks.LoadCompleteSelectionWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), target)
	if err != nil {
		return err
	}

	// A whole-set target with no interactive TTY is rejected with a pointer to
	// the file-reference form, never a silent mass mutation (ADR 0020).
	if !taskStdinInteractive(stdin) {
		return &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"completing a whole task set needs an interactive terminal; target one task with %s/<file>.md instead", ctx.TaskSetID)}
	}

	items := make([]ui.MultiSelectItem, len(ctx.Rows))
	for i, r := range ctx.Rows {
		items[i] = ui.MultiSelectItem{
			Label:      selectionRowLabel(r),
			Locked:     r.Locked,
			LockedMark: r.LockedMark,
		}
	}

	selection, err := runTaskMultiSelect(fmt.Sprintf("Complete tasks in %s", ctx.TaskSetID), items)
	if err != nil {
		return err
	}
	if !selection.Confirmed {
		return nil // Esc cancels: zero writes.
	}

	var selectedIDs []string
	for _, idx := range selection.Checked {
		if idx >= 0 && idx < len(ctx.Rows) {
			selectedIDs = append(selectedIDs, ctx.Rows[idx].TaskID)
		}
	}
	if len(selectedIDs) == 0 {
		return nil // Empty selection: clean no-op exit.
	}

	result, err := tasks.CompleteTasksWith(d, taskProjectDeps(), taskConfigLoad, tasks.CompleteTasksOptions{
		ResolveInput:    taskResolveInput(),
		TaskSetTarget:   target,
		SelectedTaskIDs: selectedIDs,
	})
	if err != nil {
		return err
	}

	tasks.RenderTaskCompleteBatch(w, result.TaskSetID, result.Transitions)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskSkipTask(cmd *cobra.Command, args []string) {
	target := args[0]
	var err error
	if isTaskFileTarget(target) {
		// A <task-set>/<file>.md reference defers exactly one task, no prompt.
		err = runTaskSkipTaskWith(cmdLayerDeps().tasksDeps(), os.Stdout, target)
	} else {
		// A whole-set target opens the interactive Multi-task selection.
		err = runTaskSkipTasksWith(cmdLayerDeps().tasksDeps(), os.Stdout, os.Stdin, target)
	}
	handleTaskExit(err)
}

func runTaskSkipTaskWith(d *tasks.Deps, w io.Writer, taskPath string) error {
	result, err := tasks.SkipTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.SkipTaskOptions{
		ResolveInput: taskResolveInput(),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	tasks.RenderTaskSkip(w, result.TaskSetID, result.TaskID)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskSkipTasksWith(d *tasks.Deps, w io.Writer, stdin io.Reader, target string) error {
	ctx, err := tasks.LoadSkipSelectionWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), target)
	if err != nil {
		return err
	}

	// A whole-set target with no interactive TTY is rejected with a pointer to
	// the file-reference form, never a silent mass mutation (ADR 0020).
	if !taskStdinInteractive(stdin) {
		return &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"skipping a whole task set needs an interactive terminal; target one task with %s/<file>.md instead", ctx.TaskSetID)}
	}

	items := make([]ui.MultiSelectItem, len(ctx.Rows))
	for i, r := range ctx.Rows {
		items[i] = ui.MultiSelectItem{
			Label:      selectionRowLabel(r),
			Locked:     r.Locked,
			LockedMark: r.LockedMark,
		}
	}

	selection, err := runTaskMultiSelect(fmt.Sprintf("Skip tasks in %s", ctx.TaskSetID), items)
	if err != nil {
		return err
	}
	if !selection.Confirmed {
		return nil // Esc cancels: zero writes.
	}

	var selectedIDs []string
	for _, idx := range selection.Checked {
		if idx >= 0 && idx < len(ctx.Rows) {
			selectedIDs = append(selectedIDs, ctx.Rows[idx].TaskID)
		}
	}
	if len(selectedIDs) == 0 {
		return nil // Empty selection: clean no-op exit.
	}

	result, err := tasks.SkipTasksWith(d, taskProjectDeps(), taskConfigLoad, tasks.SkipTasksOptions{
		ResolveInput:    taskResolveInput(),
		TaskSetTarget:   target,
		SelectedTaskIDs: selectedIDs,
	})
	if err != nil {
		return err
	}

	tasks.RenderTaskSkipBatch(w, result.TaskSetID, result.Transitions)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskStream(cmd *cobra.Command, args []string) {
	if taskStdoutInteractive() {
		pw, done, err := taskOpenPager()
		if err == nil {
			err = runTaskStreamWith(cmdLayerDeps().tasksDeps(), pw, args[0])
			_ = done() // pager exit is best-effort (e.g. `q` to quit is fine)
			handleTaskExit(err)
			return
		}
		// Pager startup failure is not fatal — fall through to direct output.
	}
	err := runTaskStreamWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runTaskStreamWith(d *tasks.Deps, w io.Writer, target string) error {
	if taskStreamRaw {
		return tasks.StreamRawWith(d, taskProjectDeps(), taskConfigLoad, tasks.StreamOptions{
			ResolveInput: taskResolveInput(),
			Target:       target,
			Last:         taskStreamLast,
		}, w)
	}
	result, err := tasks.StreamWith(d, taskProjectDeps(), taskConfigLoad, tasks.StreamOptions{
		ResolveInput: taskResolveInput(),
		Target:       target,
		Last:         taskStreamLast,
	})
	if err != nil {
		return err
	}
	tasks.RenderStream(w, result, tasks.RenderStreamOptions{
		Full:       taskStreamFull,
		ToolDetail: taskStreamToolDetail,
	})
	return nil
}

func runTaskSpend(cmd *cobra.Command, args []string) error {
	target := ""
	if len(args) > 0 {
		target = args[0]
	}
	return runTaskSpendWith(cmdLayerDeps().tasksDeps(), os.Stdout, target)
}

func runTaskSpendWith(d *tasks.Deps, w io.Writer, target string) error {
	if target != "" {
		result, err := tasks.SpendSetBreakdownWith(d, taskProjectDeps(), taskConfigLoad, tasks.SpendOptions{
			ResolveInput: taskResolveInput(),
			Target:       target,
		})
		if err != nil {
			return err
		}
		if taskSpendJSON {
			return tasks.RenderSpendSetBreakdownJSON(w, result)
		}
		tasks.RenderSpendSetBreakdown(w, result)
		return nil
	}
	result, err := tasks.SpendRollupWith(d, taskProjectDeps(), taskConfigLoad, tasks.SpendOptions{
		ResolveInput: taskResolveInput(),
	})
	if err != nil {
		return err
	}
	if taskSpendJSON {
		return tasks.RenderSpendRollupJSON(w, result)
	}
	tasks.RenderSpendRollup(w, result)
	return nil
}

func runTaskShowPath(cmd *cobra.Command, args []string) {
	var taskSetID string
	if len(args) > 0 {
		taskSetID = args[0]
	}
	err := runTaskShowPathWith(cmdLayerDeps().tasksDeps(), os.Stdout, taskSetID)
	handleTaskExit(err)
}

func runTaskShowPathWith(d *tasks.Deps, w io.Writer, taskSetID string) error {
	result, err := tasks.ShowPath(d, cmdLayerDeps().WorkDir(), taskSetID)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, result.Path)
	return nil
}

func runTaskExport(cmd *cobra.Command, args []string) {
	err := runTaskExportWith(cmdLayerDeps().tasksDeps(), os.Stdout, args)
	handleTaskExit(err)
}

func runTaskExportWith(d *tasks.Deps, w io.Writer, taskSetIDs []string) error {
	result, err := tasks.ExportWith(d, taskProjectDeps(), taskConfigLoad, tasks.ExportOptions{
		ResolveInput: taskResolveInput(),
		TaskSetIDs:   taskSetIDs,
		OutputPath:   taskExportOutput,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(w, result.Path)
	return nil
}

func runTaskImport(cmd *cobra.Command, args []string) {
	err := runTaskImportWith(cmdLayerDeps().tasksDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runTaskImportWith(d *tasks.Deps, w io.Writer, archivePath string) error {
	result, err := tasks.ImportWith(d, taskProjectDeps(), taskConfigLoad, tasks.ImportOptions{
		ResolveInput: taskResolveInput(),
		ArchivePath:  archivePath,
		AsID:         taskImportAs,
	})
	if err != nil {
		return err
	}
	for _, set := range result.Sets {
		fmt.Fprintln(w, set.Path)
	}
	return nil
}

func runTaskMigrate(cmd *cobra.Command, args []string) {
	err := runTaskMigrateWith(cmdLayerDeps().tasksDeps(), os.Stdout)
	handleTaskExit(err)
}

func runTaskMigrateWith(d *tasks.Deps, w io.Writer) error {
	result, err := tasks.Migrate(d, cmdLayerDeps().WorkDir())
	if err != nil {
		return err
	}
	tasks.RenderMigrate(w, result)
	return nil
}

func runTaskAgents(cmd *cobra.Command, args []string) error {
	return runTaskAgentsWith(cmdLayerDeps().tasksDeps(), os.Stdout, taskAgentsModels)
}

func runTaskAgentsWith(d *tasks.Deps, w io.Writer, models bool) error {
	cfg, err := taskConfigLoad(config.DefaultConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tasks agents: load config: %w", err)
	}
	if os.IsNotExist(err) {
		cfg = nil
	}
	rows := tasks.AgentCatalogWithConfig(d, cfg)
	renderTaskAgents(w, rows)
	if models {
		renderTaskAgentModels(w, rows)
	}
	return nil
}

func renderTaskAgents(w io.Writer, rows []tasks.AgentCatalogRow) {
	fmt.Fprintf(w, "%-9s %-14s %-5s %-6s %s\n", "agent", "binary", "found", "assist", "effort ladder")
	for _, row := range rows {
		fmt.Fprintf(w, "%-9s %-14s %-5s %-6s %s\n", row.Agent, row.Binary, yesNo(row.Found), yesNo(row.Assistance), renderEffortLadder(row.Agent, row.EffortLadder, row.ModelSkips))
	}
}

// renderTaskAgentModels prints each preset's curated model aliases, recommended
// first. Models are a suggestion surface for whoever fills in --model, so they
// stay off the default render and appear only when asked for.
func renderTaskAgentModels(w io.Writer, rows []tasks.AgentCatalogRow) {
	fmt.Fprintf(w, "\n%-9s %s\n", "agent", "models (recommended first)")
	for _, row := range rows {
		if len(row.Models) == 0 {
			fmt.Fprintf(w, "%-9s %s\n", row.Agent, "none")
			continue
		}
		models := strings.Join(row.Models, ", ")
		if row.ModelsInstallDependent {
			models += " (install-dependent aliases)"
		}
		fmt.Fprintf(w, "%-9s %s\n", row.Agent, models)
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// renderEffortLadder renders one preset's resolved tiers. An entry the ladder is
// currently walking past carries its Effort model skip's remaining time —
// "(skipped 47m)", or "(skipped ∞)" for a permanent one (ADR-0168) — so the
// catalog answers why a drain is running the tier's tail.
func renderEffortLadder(agent string, ladder []tasks.AgentCatalogEffortTier, skips map[string]time.Time) string {
	if len(ladder) == 0 {
		return "none"
	}
	now := time.Now()
	parts := make([]string, 0, len(ladder))
	for _, tier := range ladder {
		entries := "none"
		if len(tier.Entries) > 0 {
			rendered := make([]string, 0, len(tier.Entries))
			for _, entry := range tier.Entries {
				model := entry.Model
				if agent != "cursor" && entry.Reasoning != "" {
					model += "[reasoning=" + entry.Reasoning + "]"
				}
				if until, ok := skips[entry.Model]; ok {
					model += " (skipped " + tasks.FormatModelSkipRemaining(until, now) + ")"
				}
				rendered = append(rendered, model)
			}
			entries = strings.Join(rendered, ", ")
		}
		parts = append(parts, fmt.Sprintf("%s: %s (%s)", tier.Tier, entries, tier.Source))
	}
	return strings.Join(parts, "; ")
}

func handleTaskExit(err error) {
	if err == nil {
		return
	}
	var exitErr *tasks.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Err != nil {
			fmt.Fprintln(os.Stderr, exitErr.Err)
		}
		os.Exit(exitErr.Code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(tasks.ExitSetup)
}

func runTaskBindWorktree(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := taskConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	cwd, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return fmt.Errorf("determine working directory: %w", err)
	}
	d := cmdLayerDeps().queueDeps()
	d.LoadConfig = taskConfigLoad
	opts := drain.BindWorktreeOptions{
		Force:   taskBindWorktreeForce,
		Managed: taskBindWorktreeManaged,
		Yes:     taskRunYes,
		In:      os.Stdin,
	}
	if taskBindWorktreeManaged {
		trunkPath, err := resolveManagedTrunk(d.Tasks, cfg, cwd, taskBindWorktreeTrunk)
		if err != nil {
			return fmt.Errorf("tasks bind-worktree: %w", err)
		}
		opts.TrunkPath = trunkPath
	}
	_, err = drain.BindWorktree(d, cfg, args[0], cwd, opts, os.Stdout)
	return err
}

func runTaskUnbindWorktree(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := taskConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	d := cmdLayerDeps().queueDeps()
	d.LoadConfig = taskConfigLoad
	_, err = drain.AbandonWithOptions(d, cfg, args[0], os.Stdout, drain.AbandonOptions{Yes: taskUnbindWorktreeYes, In: os.Stdin})
	return err
}

func runTaskFold(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := taskConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	_, err = binding.Fold(
		cmdLayerDeps().tasksDeps(),
		taskProjectDeps(),
		cfg,
		args[0],
		binding.FoldOptions{
			Yes:         taskFoldYes,
			In:          os.Stdin,
			AgentPreset: selectedTaskAgentPreset(),
			AgentCmd:    taskAgentCmd,
		},
		binding.LifecycleHooks{},
		os.Stdout,
	)
	if err != nil {
		return fmt.Errorf("tasks fold: %w", err)
	}
	return nil
}

func taskProjectDeps() *project.Deps {
	return cmdLayerDeps().projectDeps()
}
