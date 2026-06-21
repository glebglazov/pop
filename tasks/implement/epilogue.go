package implement

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/tasks"
)

// recordMergeabilityOnDone computes and records Mergeability when an
// implement worktree drain reaches Done (ADR-0036). Errors are best-effort:
// mergeability is advisory and the set is already successfully drained.
func recordMergeabilityOnDone(d *Deps, result *tasks.RunTaskSetResult, stderr io.Writer) {
	if result == nil || result.RuntimePath == "" {
		return
	}
	id := integration.DefaultDeps()
	id.Tasks = d.tasksDeps()
	if err := integration.RecordImplementMergeability(id, result.ProjectPath, result.RuntimePath, result.TaskSetID, ""); err != nil {
		if stderr == nil {
			stderr = io.Discard
		}
		fmt.Fprintf(stderr, "warning: mergeability check: %v\n", err)
	}
}

// OfferIntegration presents the integration offer after a set drains to Done in
// a worktree, when stdin is a TTY and --yes is not set. It reads the
// Mergeability record just recorded by recordMergeabilityOnDone and asks
// "integrate into <working branch> now?". A trunk drain has no Mergeability
// record and is silently skipped (ADR-0036). Conflicts route to the attended
// agent path (same as `pop queue integrate`).
//
// With --yes: never prompts. Integrates automatically only when
// auto_merge_clean=true (ADR-0035/0036) and the set is clean; conflicts always
// park in the Integration backlog regardless of the flag.
func OfferIntegration(d *Deps, result *tasks.RunTaskSetResult, opts WholeSetOptions) {
	if result == nil || !result.TaskSetDone || result.RuntimePath == "" {
		return
	}
	out := opts.Output
	if out == nil {
		out = io.Discard
	}
	if opts.Yes {
		tryAutoIntegrateYes(d, result, opts, out)
		return
	}
	if !d.stdinInteractive(opts.ConfirmIn) {
		return
	}

	id := integration.DefaultDeps()
	id.Tasks = d.tasksDeps()

	rec, ok, err := integration.Lookup(id, result.TaskSetID)
	if err != nil || !ok {
		return // trunk drain or state error: no offer
	}

	workingBranch, err := integration.MainWorktreeBranch(id, result.RuntimePath)
	if err != nil || workingBranch == "" {
		return // bare repo or detached HEAD: no offer
	}

	cfg, _ := d.loadConfig(config.DefaultConfigPath())

	var mergeDesc string
	switch rec.Status {
	case integration.StatusClean:
		mergeDesc = "merges clean"
	case integration.StatusConflicts:
		mergeDesc = "has conflicts"
	default:
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Integrate %s into %s? (%s) [y/n]: ", result.TaskSetID, workingBranch, mergeDesc)

	reader := bufio.NewReader(opts.ConfirmIn)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return
	}

	if _, intErr := integration.IntegrateWithOptions(id, cfg, result.TaskSetID, out, integration.IntegrationOptions{
		In:            opts.ConfirmIn,
		AgentPreset:   opts.AgentPreset,
		AgentPresets:  opts.AgentPresets,
		AgentExplicit: opts.AgentExplicit,
		AgentCmd:      opts.AgentCmd,
	}, integration.IntegrateHooks{}); intErr != nil {
		fmt.Fprintf(out, "integrate: %v\n", intErr)
	}
}

// tryAutoIntegrateYes is the --yes integration path (AFK). It integrates a
// clean worktree drain only when the repository opted in with
// auto_merge_clean=true (ADR-0035/0036). Conflicts always park in the
// Integration backlog; no prompt is ever shown.
func tryAutoIntegrateYes(d *Deps, result *tasks.RunTaskSetResult, opts WholeSetOptions, out io.Writer) {
	if result == nil || result.RuntimePath == "" {
		return
	}

	id := integration.DefaultDeps()
	id.Tasks = d.tasksDeps()

	rec, ok, err := integration.Lookup(id, result.TaskSetID)
	if err != nil || !ok {
		return // trunk drain or state error: park in backlog
	}
	if rec.Status != integration.StatusClean {
		return // conflicts always park in backlog
	}

	cfg, _ := d.loadConfig(config.DefaultConfigPath())
	repoConfig, _ := cfg.ResolveRepoConfig(&config.Deps{FS: d.projectDeps().FS}, result.ProjectPath)
	if !repoConfig.AutoMergeClean {
		return // opt-in not set: park in backlog
	}

	if _, intErr := integration.IntegrateWithOptions(id, cfg, result.TaskSetID, out, integration.IntegrationOptions{
		In: tasks.NonInteractiveReader{},
	}, integration.IntegrateHooks{}); intErr != nil {
		fmt.Fprintf(out, "integrate: %v\n", intErr)
	}
}
