package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/glebglazov/pop/integrate"
	"github.com/spf13/cobra"
)

var integrateUpdateExisting bool
var integratePaneSkill bool
var integrateTaskSkills bool
var integrateNoPaneSkill bool
var integrateNoTaskSkills bool
var integrateVerbose bool
var integrateOverwriteConflicts bool
var integrateYes bool

var integrateCmd = &cobra.Command{
	Use:   "integrate <agent>...",
	Short: "Install pop status wiring for a coding agent",
	Long: `Install pop's status wiring for one or more coding agents.

The status wiring makes the agent report pane status to pop's monitor; it
changes no agent behavior. Optional skills (the pane skill and the task
planning skills) resolve from the merged [integrations] skills list in pop
config (embedded defaults, then your config.toml, then anything you have
stated in pop's override layer).

Run with no flags to install the core status wiring plus every optional
component in the merged baseline — no prompts, TTY or not. Re-running
re-asserts the full merged baseline (bare integrate takes back every
component you declined).

  --no-pane-skills
                Remove the pane skill if it is currently installed (pop-owned
                artifacts only) and state the decline in pop's override layer,
                so it holds for later runs.

  --no-task-skills
                Remove the task planning skills if currently installed
                (pop-owned only) and state the decline. Same semantics as
                --no-pane-skills.

  --overwrite-conflicts
                On install, prompt to destroy unowned entries that block
                pop's integration artifacts. Plain integrate skips unowned
                conflicts and names this command.

The --pane-skill and --task-skills flags are no longer supported; configure
[integrations] skills in ~/.config/pop/config.toml instead.

Supported agents:
  claude    Install pane monitoring hooks in ~/.claude/settings.json.
  codex     Install pane monitoring hooks in ~/.codex/hooks.json.
  pi        Install a pane monitoring extension at
            ~/.pi/agent/extensions/pop-status-sync.ts.
  opencode  Install a pane monitoring plugin at
            ~/.config/opencode/plugins/pop-status-sync.ts.
  cursor    Install pane monitoring hooks in ~/.cursor/hooks.json.
  kimi      Merge pane monitoring [[hooks]] into ~/.kimi-code/config.toml
            (or $KIMI_CODE_HOME/config.toml), preserving the rest of the file.

Multiple agents can be integrated in a single invocation (e.g. 'pop integrate
claude pi cursor'); each is installed in order with the same component flags
applied uniformly to all.

Re-running the command for an agent is idempotent: existing pop status wiring
for that agent is refreshed to the current version, and unrelated hooks are
preserved.

With --update-existing, no agent argument is expected: pop detects which
agents are already integrated and refreshes them to the current binary's
embedded content. Agents that are not installed are left alone. This is
the command that 'make install' and the Homebrew post_install hook run
after copying a new binary into place.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if integrateUpdateExisting {
			if len(args) > 0 {
				return fmt.Errorf("--update-existing does not accept an agent argument")
			}
			return nil
		}
		if len(args) < 1 {
			return fmt.Errorf("requires at least 1 argument: agent name (%s)", strings.Join(integrate.Agents, ", "))
		}
		return nil
	},
	ValidArgs: integrate.Agents,
	RunE:      runIntegrate,
}

var integrateRemoveCmd = &cobra.Command{
	Use:   "remove <agent> [component...]",
	Short: "Remove pop integration components for an agent",
	Long: `Remove pop integration components for an agent.

With no component identifiers, every pop component currently installed for the
agent is removed. With identifiers, exactly that set is removed. Valid
identifiers: status-wiring, pane-skills, task-skills.

Removal only ever deletes artifacts pop owns: status wiring strips pop's hook
entries while preserving unrelated hooks (claude, codex, cursor), deletes the
pop-owned status-sync extension (pi, opencode), or prunes pop's [[hooks]] blocks
from kimi's config.toml and leaves the rest of the file alone; file-based skills
delete only pop-owned symlinks and their render-tree entries — a same-named entry
pop does not own is left untouched and reported.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("requires an agent name (%s)", strings.Join(integrate.Agents, ", "))
		}
		return nil
	},
	ValidArgs: integrate.Agents,
	RunE: func(cmd *cobra.Command, args []string) error {
		var ids []integrate.ComponentID
		for _, a := range args[1:] {
			ids = append(ids, integrate.ComponentID(a))
		}
		d := integrate.DefaultDeps()
		_, err := integrate.Remove(d, integrate.Request{
			Agent:            args[0],
			RemoveComponents: ids,
		})
		return err
	},
}

func init() {
	integrateCmd.Flags().BoolVar(&integrateUpdateExisting, "update-existing", false,
		"Refresh already-installed agent integrations to match the current binary (no agent argument)")
	integrateCmd.Flags().BoolVar(&integratePaneSkill, "pane-skill", false,
		"Install the pane skill (lets the agent drive tmux panes) alongside the status wiring")
	integrateCmd.Flags().BoolVar(&integrateTaskSkills, "task-skills", false,
		"Install the task planning skills (grill-with-docs, to-spec, to-tasks) alongside the status wiring")
	integrateCmd.Flags().BoolVar(&integrateNoPaneSkill, "no-pane-skills", false,
		"Remove the pane skill if installed (pop-owned only) and record the opt-out")
	integrateCmd.Flags().BoolVar(&integrateNoTaskSkills, "no-task-skills", false,
		"Remove the task planning skills if installed (pop-owned only) and record the opt-out")
	integrateCmd.Flags().BoolVar(&integrateVerbose, "verbose", false,
		"Show all outcomes including already-current no-ops and opted-out components")
	integrateCmd.Flags().BoolVar(&integrateOverwriteConflicts, "overwrite-conflicts", false,
		"On explicit install, prompt to destroy unowned entries that block pop's integration artifacts")
	integrateCmd.Flags().BoolVarP(&integrateYes, "yes", "y", false,
		"Assume yes to all integrate prompts (including conflict overwrites)")
	integrateCmd.AddCommand(integrateRemoveCmd)
	rootCmd.AddCommand(integrateCmd)
}

func positiveIntegrateFlagError(flag string) error {
	return fmt.Errorf("%s is no longer supported: configure optional components via [integrations] skills in pop config, or run 'pop integrate <agent>' to install the merged baseline", flag)
}

func runIntegrate(cmd *cobra.Command, args []string) error {
	if integrateUpdateExisting {
		if integrateOverwriteConflicts {
			return fmt.Errorf("--overwrite-conflicts cannot be used with --update-existing")
		}
		return runIntegrateUpdateExisting()
	}
	if integratePaneSkill {
		return positiveIntegrateFlagError("--pane-skill")
	}
	if integrateTaskSkills {
		return positiveIntegrateFlagError("--task-skills")
	}

	var explicitOptOuts map[integrate.ComponentID]bool
	if integrateNoPaneSkill {
		if explicitOptOuts == nil {
			explicitOptOuts = make(map[integrate.ComponentID]bool)
		}
		explicitOptOuts[integrate.ComponentPaneSkill] = true
	}
	if integrateNoTaskSkills {
		if explicitOptOuts == nil {
			explicitOptOuts = make(map[integrate.ComponentID]bool)
		}
		explicitOptOuts[integrate.ComponentTaskSkills] = true
	}

	core, ok := integrate.LookupComponent(integrate.ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	for _, agent := range args {
		agent = strings.ToLower(agent)
		if !core.AgentSupported(agent) {
			return fmt.Errorf("unknown agent %q (expected: %s)", agent, strings.Join(integrate.Agents, ", "))
		}
	}

	cd := cmdLayerDeps().configDeps()
	bareIntegrate := len(explicitOptOuts) == 0
	if err := integrate.ApplyComponentOptOuts(cd, bareIntegrate, explicitOptOuts); err != nil {
		return fmt.Errorf("component opt-outs: %w", err)
	}

	baseline, err := integrate.BaselineLoader(cd)
	if err != nil {
		return err
	}

	for _, agent := range args {
		d := integrate.DefaultDeps()
		if stdinIsInteractive() {
			d.ConfirmOverwrite = func(path string) bool {
				ok, err := integrate.PromptOverwriteConflict(os.Stdin, os.Stdout, path)
				return err == nil && ok
			}
		}
		report, err := integrate.Install(d, integrate.Request{
			Agent:              agent,
			Components:         baseline,
			ExplicitOptOuts:    explicitOptOuts,
			OverwriteConflicts: integrateOverwriteConflicts,
			AssumeYes:          integrateYes,
			Verbose:            integrateVerbose,
		})
		if err != nil {
			return err
		}
		integrate.PrintOutcomes(os.Stdout, report.Outcomes, integrateVerbose, true)
	}
	return nil
}

func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func runIntegrateUpdateExisting() error {
	err := integrate.RunUpdateExistingWith(
		buildRevision(),
		cmdLayerDeps().configDeps(),
		integrate.DefaultDeps,
		os.Stdout,
		os.Stderr,
		integrateVerbose,
	)
	refreshMonitorDaemonIfRunning()
	return err
}
