package cmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/spf13/cobra"
)

//go:embed all:skills/pop
var skillFiles embed.FS

//go:embed extensions/pi/pop-status-sync.ts
var piExtensionFile []byte

//go:embed extensions/opencode/pop-status-sync.ts
var opencodeExtensionFile []byte

// installComponentCollectOutcomes installs one component for agent and returns
// per-skill (or status-wiring) outcome lines.
func installComponentCollectOutcomes(d *integrateDeps, home, agent string, comp integrationComponent) ([]integrateOutcome, error) {
	id := comp.id

	if comp.install != nil {
		dryD := withDryRun(d)
		if err := comp.install(dryD, home, agent); err != nil {
			return nil, err
		}
		quietD := *d
		quietD.stdout = nil
		if err := comp.install(&quietD, home, agent); err != nil {
			return nil, err
		}
		label := installLabel(!dryD.installed, dryD.installed && dryD.changed)
		return []integrateOutcome{statusWiringOutcome(agent, label)}, nil
	}

	prefix := d.resolveSkillsPrefix()

	installedBefore, err := fileComponentInstalledNames(d, home, id, agent)
	if err != nil {
		return nil, fmt.Errorf("installed check for %s/%s: %w", agent, id, err)
	}
	staleBefore := true
	if len(installedBefore) > 0 {
		if staleBefore, err = fileComponentStaleResolved(d, home, id, agent, installedBefore); err != nil {
			return nil, fmt.Errorf("stale check for %s/%s: %w", agent, id, err)
		}
	}

	installD := *d
	installD.agentName = agent
	if !d.overwriteConflicts {
		installD.stdout = nil
	}
	if err := installFileComponent(&installD, home, id, agent); err != nil {
		return nil, err
	}

	overwritten := overwrittenSkillPaths(prefix, id, agent, installD.overwrotePaths)
	postConflict, err := preInstallSkillConflicts(&installD, home, agent, id, prefix)
	if err != nil {
		return nil, fmt.Errorf("conflict check for %s/%s: %w", agent, id, err)
	}

	return fileComponentOutcomesInCatalogOrder(
		agent, id, prefix, installedBefore, staleBefore, installD.prunedStale,
		nil, postConflict, overwritten,
	), nil
}

// conflictSkipLabel formats the reasoned outcome for an Integration conflict
// that was skipped, naming the exact command that resolves it.
func conflictSkipLabel(agent, conflictPath string) string {
	return fmt.Sprintf("skipped (conflict at %s; run 'pop integrate %s --overwrite-conflicts' to replace it)", conflictPath, agent)
}

// reportOverwriteDestroyed prints a loud per-item report after hard-deleting an
// unowned entry during --overwrite-conflicts. No backup is kept.
func reportOverwriteDestroyed(out io.Writer, conflictPath string) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "  OVERWRITE: destroyed %s (not owned by pop — no backup kept)\n", conflictPath)
}

// integrateDeps holds the filesystem operations the integrate command depends
// on. Production code uses defaultIntegrateDeps; tests inject mocks.
//
// The same struct drives two modes:
//
//   - Real mode: writeFile/mkdirAll/removeAll mutate the filesystem.
//   - Dry-run mode (DryRun=true, constructed via withDryRun): writeFile
//     compares the would-be bytes against what's on disk and records flags
//     instead of writing. The install functions do not branch on DryRun —
//     they just run unchanged and the deps layer short-circuits side effects.
//
// The dry-run output fields are written only in dry-run mode:
//
//   - installed: set to true when any pop artifact already exists for this
//     agent (a file would be overwritten by the real run).
//   - changed:   set to true when at least one write would produce bytes
//     that differ from what's on disk.
//
// Agent-specific install logic that knows something the deps shim can't see
// (e.g. "there are pop hooks inside settings.json") may set `installed`
// directly, guarded by `d.DryRun`. See installClaudeHooks for an example.
type integrateDeps struct {
	userHomeDir func() (string, error)
	readFile    func(string) ([]byte, error)
	writeFile   func(string, []byte, os.FileMode) error
	mkdirAll    func(string, os.FileMode) error
	removeAll   func(string) error
	stdout      io.Writer

	// logf emits a debug log line. Production wires debug.Log; tests can
	// override to capture what was logged without needing POP_LOG set.
	logf func(string, ...any)

	// stdin is the wizard's prompt input. Production uses os.Stdin; tests
	// supply a scripted reader. Nil disables prompting (declines every step),
	// which keeps the dry-run/refresh deps inert.
	stdin io.Reader

	// File-based component installer (link installer, ADR 0011). dataDir
	// resolves pop's data directory root (the parent of integrations/);
	// symlink/readlink/lstatMode manage the agent-location symlinks and the
	// ownership check.
	dataDir   func() (string, error)
	symlink   func(target, link string) error
	readlink  func(string) (string, error)
	lstatMode func(string) (os.FileMode, error)

	// readDirNames lists immediate entry names under a directory. Used by
	// stale-name cleanup after prefix or base-name changes (ADR 0063).
	readDirNames func(string) ([]string, error)

	// skillsPrefix is the resolved skill-name prefix for rendered skills (the
	// `<prefix>` in `<prefix><base>`, ADR 0063). A nil pointer means "unset" →
	// config.DefaultSkillsPrefix (`pop-`); a non-nil pointer (including an empty
	// string) is used verbatim, so skills_prefix = "" installs bare base names.
	skillsPrefix *string

	// Dry-run mode: set DryRun=true to turn writeFile into a comparator.
	// `installed` and `changed` are output fields filled in during the run.
	DryRun    bool
	changed   bool
	installed bool

	// Explicit-install conflict overwrite (ADR 0011): when overwriteConflicts is
	// true, unowned entries may be destroyed after per-item confirmation
	// (assumeYes or interactive prompt). Refresh never sets these fields.
	overwriteConflicts bool
	assumeYes          bool
	interactive        bool
	agentName          string

	// overwrotePaths records agent-location paths hard-deleted during an
	// overwrite-conflicts run; used for outcome labelling.
	overwrotePaths []string

	// prunedStale records resolved install names removed during the latest
	// file-based install; drives removed (stale) outcome lines.
	prunedStale []string
}

func defaultIntegrateDeps() *integrateDeps {
	d := &integrateDeps{
		userHomeDir: os.UserHomeDir,
		readFile:    os.ReadFile,
		writeFile:   os.WriteFile,
		mkdirAll:    os.MkdirAll,
		removeAll:   os.RemoveAll,
		stdout:      os.Stdout,
		logf:        debug.Log,
		stdin:       os.Stdin,
		dataDir:     popDataDir,
		symlink:     os.Symlink,
		readlink:    os.Readlink,
		lstatMode: func(p string) (os.FileMode, error) {
			fi, err := os.Lstat(p)
			if err != nil {
				return 0, err
			}
			return fi.Mode(), nil
		},
		readDirNames: osReadDirNames,
	}
	d.skillsPrefix = loadSkillsPrefix()
	return d
}

// osReadDirNames lists the immediate entry names under dir, sorted. A missing
// directory is not an error — it reports no entries.
func osReadDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// loadSkillsPrefix resolves [integrations] skills_prefix from merged config.
func loadSkillsPrefix() *string {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Log("loadSkillsPrefix: config load failed (%v); using default prefix %q", err, config.DefaultSkillsPrefix)
		return nil
	}
	p := cfg.ResolveSkillsPrefix()
	return &p
}

// resolveSkillsPrefix returns the resolved skill-name prefix for this deps.
func (d *integrateDeps) resolveSkillsPrefix() string {
	if d == nil || d.skillsPrefix == nil {
		return config.DefaultSkillsPrefix
	}
	return *d.skillsPrefix
}

// popDataDir returns pop's data directory root, respecting XDG_DATA_HOME.
// File-based integration artifacts live under <dataDir>/integrations/.
func popDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "pop"), nil
}

// dryRunIntegrateDeps returns an integrateDeps that reports what would change
// on disk without performing any writes. See the integrateDeps doc for the
// semantics of `installed` and `changed`.
func dryRunIntegrateDeps() *integrateDeps {
	return withDryRun(defaultIntegrateDeps())
}

// withDryRun wraps a base integrateDeps with dry-run behavior. It is exposed
// as a separate function so tests can layer dry-run on top of a fake FS
// without touching the real filesystem.
func withDryRun(base *integrateDeps) *integrateDeps {
	d := &integrateDeps{
		userHomeDir: base.userHomeDir,
		readFile:    base.readFile,
		dataDir:     base.dataDir,
		logf:        base.logf,
		skillsPrefix: base.skillsPrefix,
		readDirNames: base.readDirNames,
		DryRun:      true,
	}
	// File-component refresh inspects the link installer's render tree and the
	// agent-location symlinks to decide installed/stale/conflict, so the dry-run
	// deps pass through the base's read-only link seams (readlink, lstatMode,
	// dataDir is already copied above). symlink is the sole write op on this
	// path and stays a no-op — checks never create links, and any real refresh
	// runs through the separate real deps.
	d.symlink = func(string, string) error { return nil }
	d.readlink = base.readlink
	d.lstatMode = base.lstatMode
	// writeFile compares the proposed bytes against what's on disk.
	// Existing file → installed; different content → changed.
	// Missing file → neither (creating new files on an agent that isn't
	// installed yet is not an "update"; the auto-updater should skip).
	d.writeFile = func(path string, data []byte, _ os.FileMode) error {
		existing, err := d.readFile(path)
		if err == nil {
			d.installed = true
			if !bytes.Equal(existing, data) {
				d.changed = true
			}
		}
		return nil
	}
	// mkdirAll is a no-op in dry-run; directory creation is not a meaningful
	// change signal on its own (the file write inside catches the real state).
	d.mkdirAll = func(string, os.FileMode) error { return nil }
	// removeAll is a no-op in dry-run. We intentionally do not probe the
	// target with os.Stat here so the dry-run path stays injectable by
	// tests (which swap readFile/writeFile on a fake FS but do not stub
	// os.Stat). In practice, installed/changed detection relies on the
	// writeFile comparator — every install step that removes a directory
	// is followed by writes into that directory, which provide the signal.
	d.removeAll = func(string) error { return nil }
	// Suppress output from install functions during dry-run.
	d.stdout = nil
	return d
}

// integrateUpdateExisting, when true, tells integrateCmd to refresh any
// already-installed agent integrations to match the current binary's
// embedded content instead of installing for a specific agent.
var integrateUpdateExisting bool

// integratePaneSkill is the --pane-skill component flag. When set, the pane
// skill is installed for the agent alongside the core status wiring.
var integratePaneSkill bool

// integrateTaskSkills is the --task-skills component flag. When set,
// the task planning skills (grill-with-docs, grill-consolidate, to-prd,
// to-tasks, wayfinder, prototype, research) are
// installed for the agent alongside the core status wiring.
var integrateTaskSkills bool

// integrateNoPaneSkill is the --no-pane-skill component flag. When set, the
// pane skill is removed if it is currently installed (pop-owned artifacts
// only) and the opt-out is recorded in the outcome. Mutually exclusive with
// --pane-skill in practice: if both are set, --pane-skill takes effect.
var integrateNoPaneSkill bool

// integrateNoTaskSkills is the --no-task-skills component flag. When set,
// the task planning skills are removed if currently installed (pop-owned
// artifacts only) and the opt-out is recorded. Mutually exclusive with
// --task-skills: --task-skills takes effect when both are set.
var integrateNoTaskSkills bool

// integrateVerbose enables verbose output: "already current" no-ops and
// "skipped (opted out)" outcomes on the --update-existing path are shown
// instead of suppressed.
var integrateVerbose bool

// integrateOverwriteConflicts opts into the explicit-install overwrite flow:
// per-item confirmation (or --yes) before destroying unowned entries that
// shadow pop's integration artifacts. Invalid with --update-existing.
var integrateOverwriteConflicts bool

// integrateYes skips all integrate confirmation prompts, including conflict
// overwrites when combined with --overwrite-conflicts.
var integrateYes bool

var integrateCmd = &cobra.Command{
	Use:   "integrate <agent>...",
	Short: "Install pop status wiring for a coding agent",
	Long: `Install pop's status wiring for one or more coding agents.

The status wiring makes the agent report pane status to pop's monitor; it
changes no agent behavior. Optional skills (the pane skill and the task
planning skills) resolve from the merged [integrations] skills list in pop
config (embedded defaults, then config.runtime.toml, then user config).

Run with no flags to install the core status wiring plus every optional
component in the merged baseline — no prompts, TTY or not. Re-running
re-asserts the full merged baseline (bare integrate clears runtime
overrides).

  --no-pane-skills
                Remove the pane skill if it is currently installed (pop-owned
                artifacts only) and record the opt-out in config.runtime.toml.

  --no-task-skills
                Remove the task planning skills if currently installed
                (pop-owned only) and record the opt-out. Same semantics as
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
			return fmt.Errorf("requires at least 1 argument: agent name (claude, codex, pi, opencode, or cursor)")
		}
		return nil
	},
	ValidArgs: []string{"claude", "codex", "pi", "opencode", "cursor"},
	RunE:      runIntegrate,
}

// integrateRemoveCmd is the removal form of integrate: `pop integrate remove
// <agent> [component...]`. With no component identifiers it removes every pop
// component currently installed for the agent; with identifiers it removes
// exactly that set. Only pop-owned artifacts are deleted (ADR 0011): a
// same-named entry pop does not own is left untouched and reported.
var integrateRemoveCmd = &cobra.Command{
	Use:   "remove <agent> [component...]",
	Short: "Remove pop integration components for an agent",
	Long: `Remove pop integration components for an agent.

With no component identifiers, every pop component currently installed for the
agent is removed. With identifiers, exactly that set is removed. Valid
identifiers: status-wiring, pane-skills, task-skills.

Removal only ever deletes artifacts pop owns: status wiring strips pop's hook
entries while preserving unrelated hooks (claude, codex, cursor) or deletes the
pop-owned status-sync extension (pi, opencode); file-based skills delete only
pop-owned symlinks and their render-tree entries — a same-named entry pop does
not own is left untouched and reported.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return fmt.Errorf("requires an agent name (claude, codex, pi, opencode, or cursor)")
		}
		return nil
	},
	ValidArgs: []string{"claude", "codex", "pi", "opencode", "cursor"},
	RunE: func(cmd *cobra.Command, args []string) error {
		var ids []ComponentID
		for _, a := range args[1:] {
			ids = append(ids, ComponentID(a))
		}
		return runIntegrateRemoveComponents(defaultIntegrateDeps(), args[0], ids)
	},
}

func init() {
	integrateCmd.Flags().BoolVar(&integrateUpdateExisting, "update-existing", false,
		"Refresh already-installed agent integrations to match the current binary (no agent argument)")
	integrateCmd.Flags().BoolVar(&integratePaneSkill, "pane-skill", false,
		"Install the pane skill (lets the agent drive tmux panes) alongside the status wiring")
	integrateCmd.Flags().BoolVar(&integrateTaskSkills, "task-skills", false,
		"Install the task planning skills (grill-with-docs, to-prd, to-tasks) alongside the status wiring")
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

func integrationSkillAliasForOptOut(id ComponentID) (string, bool) {
	switch id {
	case ComponentPaneSkill:
		return config.IntegrationSkillPane, true
	case ComponentTaskSkills:
		return config.IntegrationSkillTasks, true
	default:
		return "", false
	}
}

func integrationComponentForSkillAlias(alias string) (ComponentID, bool) {
	switch alias {
	case config.IntegrationSkillPane:
		return ComponentPaneSkill, true
	case config.IntegrationSkillTasks:
		return ComponentTaskSkills, true
	default:
		return "", false
	}
}

// integrationBaselineLoader loads pop config and returns optional skill
// components from the merged [integrations] skills list. Status wiring is
// not included — callers always install it separately.
var integrationBaselineLoader = func() ([]ComponentID, error) {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		return nil, err
	}
	seen := map[ComponentID]bool{}
	var baseline []ComponentID
	for _, alias := range skills {
		id, ok := integrationComponentForSkillAlias(alias)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		baseline = append(baseline, id)
	}
	return baseline, nil
}

func positiveIntegrateFlagError(flag string) error {
	return fmt.Errorf("%s is no longer supported: configure optional components via [integrations] skills in pop config, or run 'pop integrate <agent>' to install the merged baseline", flag)
}

// applyIntegrateRuntimeConfig mutates config.runtime.toml once per integrate
// invocation. Bare integrate clears runtime [integrations] overrides; --no-*
// removes the corresponding skill aliases from the runtime layer (ADR 0065).
func applyIntegrateRuntimeConfig(bareIntegrate bool, explicitOptOuts map[ComponentID]bool) error {
	if bareIntegrate {
		return config.ClearRuntimeIntegrations()
	}
	if len(explicitOptOuts) == 0 {
		return nil
	}
	var aliases []string
	for id := range explicitOptOuts {
		alias, ok := integrationSkillAliasForOptOut(id)
		if !ok {
			continue
		}
		aliases = append(aliases, alias)
	}
	return config.RemoveRuntimeIntegrationSkills(aliases...)
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

	var explicitOptOuts map[ComponentID]bool
	if integrateNoPaneSkill {
		if explicitOptOuts == nil {
			explicitOptOuts = make(map[ComponentID]bool)
		}
		explicitOptOuts[ComponentPaneSkill] = true
	}
	if integrateNoTaskSkills {
		if explicitOptOuts == nil {
			explicitOptOuts = make(map[ComponentID]bool)
		}
		explicitOptOuts[ComponentTaskSkills] = true
	}

	// Validate all agents upfront before installing any, so a mix of valid and
	// invalid names does not partially install the valid ones.
	core, ok := lookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	for _, agent := range args {
		agent = strings.ToLower(agent)
		if !core.supported(agent) {
			return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
		}
	}

	bareIntegrate := len(explicitOptOuts) == 0
	if err := applyIntegrateRuntimeConfig(bareIntegrate, explicitOptOuts); err != nil {
		return fmt.Errorf("runtime config: %w", err)
	}

	baseline, err := integrationBaselineLoader()
	if err != nil {
		return err
	}

	// Install each agent in order with the same merged baseline applied uniformly.
	for _, agent := range args {
		d := defaultIntegrateDeps()
		if err := runIntegrateComponents(d, agent, baseline, stdinIsInteractive(), integrateVerbose, explicitOptOuts, integrateOverwriteConflicts, integrateYes); err != nil {
			return err
		}
	}
	return nil
}

// stdinIsInteractive reports whether stdin is a terminal. Mirrors the task
// execution-confirmation detection: a non-terminal stdin (pipe, redirect, CI)
// is treated as non-interactive.
func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// runIntegrateComponents is the entry point for `pop integrate <agent>`. It
// installs the core status wiring plus every component in baseline (from merged
// [integrations] skills), with no prompting. Prints one outcome line per
// (agent, component) pair: added, updated, already-current, skipped.
//
// explicitOptOuts lists components actively declined via --no-* flags. For a
// component in this set that is not in the install set, removeOptOutCollectOutcome
// is called: if installed (pop-owned), it is removed and "removed (opted out)"
// is reported; if not installed, "skipped (opted out)" is reported.
func runIntegrateComponents(d *integrateDeps, agent string, baseline []ComponentID, interactive bool, verbose bool, explicitOptOuts map[ComponentID]bool, overwriteConflicts, assumeYes bool) error {
	agent = strings.ToLower(agent)
	d.overwriteConflicts = overwriteConflicts
	d.assumeYes = assumeYes
	d.interactive = interactive
	d.agentName = agent

	core, ok := lookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	// The status-wiring support set is exactly the known agents, so this
	// doubles as the unknown-agent guard.
	if !core.supported(agent) {
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}

	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Build the install set: status-wiring is always included; baseline lists
	// optional skill components from merged config.
	installSet := map[ComponentID]bool{ComponentStatusWiring: true}
	for _, id := range baseline {
		installSet[id] = true
	}

	// Collect outcome lines per supported (agent, component) pair.
	var outcomes []integrateOutcome
	for _, comp := range integrationCatalog {
		if !comp.supported(agent) {
			continue
		}
		if installSet[comp.id] {
			compOutcomes, err := installComponentCollectOutcomes(d, home, agent, comp)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, compOutcomes...)
		} else if explicitOptOuts[comp.id] {
			compOutcomes, err := optOutRemoveOutcomes(d, home, agent, comp.id)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, compOutcomes...)
		} else if comp.id != ComponentStatusWiring {
			compOutcomes, err := optOutSkipOutcomes(d, agent, comp.id)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, compOutcomes...)
		}
	}

	printIntegrateOutcomes(d.stdout, outcomes, verbose, true /* explicit path */)
	return nil
}

// removeOptOutCollectOutcome is retained for tests that call it directly.
func removeOptOutCollectOutcome(d *integrateDeps, home, agent string, id ComponentID) ([]integrateOutcome, error) {
	return optOutRemoveOutcomes(d, home, agent, id)
}

// runIntegrateWith installs the status-wiring component for the given agent.
//
// Bare `pop integrate <agent>` installs only the status wiring — the core
// component implied by the integrate verb (ADR 0010). The pane skill and the
// task planning skills are explicit opt-ins landed by later slices, so no
// skill files are written on this path. Component knowledge comes from the
// catalog; this function does not hardcode the agent fan-out.
func runIntegrateWith(d *integrateDeps, agent string) error {
	agent = strings.ToLower(agent)

	comp, ok := lookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	// The status-wiring support set is exactly the known agents, so this
	// doubles as the unknown-agent guard.
	if !comp.supported(agent) {
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}

	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	return comp.install(d, home, agent)
}

// ----- Claude integration ----------------------------------------------------

type hookSpec struct {
	event   string
	command string
}

// popHooks defines the hook commands installed into Claude's settings.json.
// Each entry is a (event, command) pair; the matcher is left empty so the
// hook fires for every tool / event.
//
// The topic hook is a *separate* UserPromptSubmit entry alongside the
// set-status one (ADR 0023): it pipes the same payload to `set-topic --derive`
// to derive a pane topic from the prompt. SessionStart also clears the Topic
// so a new agent session in the same pane can re-derive on its first prompt.
// It installs whenever the core status wiring installs — no extra opt-in —
// and rides the same idempotent install/remove/refresh paths (both commands
// match isPopHookCommand).
var popHooks = []hookSpec{
	{"SessionStart", "pop pane set-status clear 2>/dev/null || true"},
	{"SessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-status working 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-topic --derive 2>/dev/null || true"},
	{"PreToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
	{"Notification", "pop pane set-status unread 2>/dev/null || true"},
}

// installClaudeHooks merges pop's hook entries into ~/.claude/settings.json,
// preserving any unrelated existing hooks. Old pop hooks are removed first
// (matched via isPopHook) so re-running the command is idempotent.
func installClaudeHooks(d *integrateDeps, home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	return installJSONHooks(d, settingsPath, popHooks)
}

func installJSONHooks(d *integrateDeps, settingsPath string, hooksToInstall []hookSpec) error {
	settings := make(map[string]interface{})
	data, err := d.readFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	// Strip any previously installed pop hooks before adding the current set.
	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		cleaned := removePopHooks(eventHooks)
		// Dry-run "installed" detection for claude: settings.json exists for
		// every claude user, so file-presence is not a reliable signal that
		// pop is installed. Finding any existing pop hooks is — they could
		// only have gotten there via a prior `pop integrate claude` run.
		if d.DryRun && len(cleaned) < len(eventHooks) {
			d.installed = true
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	for _, h := range hooksToInstall {
		hookEntry := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": h.command,
				},
			},
		}
		eventHooks, _ := hooks[h.event].([]interface{})
		eventHooks = append(eventHooks, hookEntry)
		hooks[h.event] = eventHooks
	}

	if err := d.mkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize settings: %w", err)
	}

	if err := d.writeFile(settingsPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsPath, err)
	}

	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(hooksToInstall), settingsPath)
	}
	return nil
}

// ----- Codex integration -----------------------------------------------------

var codexPopHooks = []hookSpec{
	{"SessionStart", "pop pane set-status clear 2>/dev/null || true"},
	{"SessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"UserPromptSubmit", "pop pane set-status working 2>/dev/null || true"},
	// Topic hook: a separate UserPromptSubmit entry alongside set-status,
	// riding core status wiring (ADR 0023). --label codex selects codex's
	// payload adapter; codex exposes no transcript_path.
	{"UserPromptSubmit", "pop pane set-topic --derive --label codex 2>/dev/null || true"},
	{"PreToolUse", "pop pane set-status working 2>/dev/null || true"},
	{"PermissionRequest", "pop pane set-status unread 2>/dev/null || true"},
	{"Stop", "pop pane set-status unread 2>/dev/null || true"},
}

func installCodexHooks(d *integrateDeps, home string) error {
	hooksPath := filepath.Join(home, ".codex", "hooks.json")
	return installJSONHooks(d, hooksPath, codexPopHooks)
}

// ----- Pi integration --------------------------------------------------------

// installPiExtension writes the embedded pi extension TypeScript file. Pi
// auto-discovers any *.ts file under ~/.pi/agent/extensions/ at startup.
func installPiExtension(d *integrateDeps, home string) error {
	extDir := filepath.Join(home, ".pi", "agent", "extensions")
	if err := d.mkdirAll(extDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", extDir, err)
	}
	extPath := filepath.Join(extDir, "pop-status-sync.ts")
	if err := d.writeFile(extPath, piExtensionFile, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", extPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed pi extension at %s\n", extPath)
	}
	return nil
}

// injectFrontmatterName guarantees the YAML frontmatter contains a `name:`
// field set to the given value. If the file already has a name, it is
// replaced. If there is no frontmatter at all, one is created.
func injectFrontmatterName(content, name string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter — wrap the content in one.
		return fmt.Sprintf("---\nname: %s\n---\n%s", name, content)
	}
	// Find the closing `---`.
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		// Malformed frontmatter — leave it alone.
		return content
	}
	// Replace existing name: line if present.
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "name:") {
			lines[i] = "name: " + name
			return strings.Join(lines, "\n")
		}
	}
	// Otherwise insert `name:` directly after the opening `---`.
	out := append([]string{lines[0], "name: " + name}, lines[1:]...)
	return strings.Join(out, "\n")
}

const popOwnedField = "pop-owned"

func injectOwnershipMarker(content string) string {
	return setFrontmatterField(content, popOwnedField, "true")
}

func setFrontmatterField(content, key, value string) string {
	field := key + ": " + value
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fmt.Sprintf("---\n%s\n---\n%s", field, content)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return content
	}
	prefix := key + ":"
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			lines[i] = field
			return strings.Join(lines, "\n")
		}
	}
	out := append([]string{lines[0], field}, lines[1:]...)
	return strings.Join(out, "\n")
}

func frontmatterHasOwnershipMarker(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	prefix := popOwnedField + ":"
	for i := 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "---" {
			return false
		}
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(t, prefix)) == "true"
		}
	}
	return false
}

// ----- Opencode integration --------------------------------------------------

// installOpencodePlugin writes the embedded opencode plugin TypeScript file.
// Opencode auto-discovers any *.ts file under ~/.config/opencode/plugins/ at startup.
func installOpencodePlugin(d *integrateDeps, home string) error {
	pluginDir := filepath.Join(home, ".config", "opencode", "plugins")
	if err := d.mkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", pluginDir, err)
	}
	pluginPath := filepath.Join(pluginDir, "pop-status-sync.ts")
	if err := d.writeFile(pluginPath, opencodeExtensionFile, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", pluginPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed opencode plugin at %s\n", pluginPath)
	}
	return nil
}

// ----- Cursor integration ----------------------------------------------------

type cursorHookSpec struct {
	event   string
	command string
}

// cursorPopHooks defines the hook commands installed into Cursor's hooks.json.
// Event names follow the Cursor CLI hooks schema (camelCase).
var cursorPopHooks = []cursorHookSpec{
	{"sessionStart", "pop pane set-status clear --label cursor 2>/dev/null || true"},
	{"sessionStart", "pop pane set-topic --clear 2>/dev/null || true"},
	{"beforeSubmitPrompt", "pop pane set-status working --label cursor 2>/dev/null || true"},
	// Topic hook: a separate beforeSubmitPrompt entry alongside set-status,
	// riding core status wiring (ADR 0023). --label cursor selects cursor's
	// payload adapter; cursor exposes no transcript_path.
	{"beforeSubmitPrompt", "pop pane set-topic --derive --label cursor 2>/dev/null || true"},
	{"preToolUse", "pop pane set-status working --label cursor 2>/dev/null || true"},
	{"afterAgentResponse", "pop pane set-status unread --label cursor 2>/dev/null || true"},
	{"stop", "pop pane set-status unread --label cursor 2>/dev/null || true"},
}

// installCursorHooks merges pop's hook entries into ~/.cursor/hooks.json,
// preserving any unrelated existing hooks. Old pop hooks are removed first
// (matched via isCursorPopHook) so re-running the command is idempotent.
func installCursorHooks(d *integrateDeps, home string) error {
	hooksPath := filepath.Join(home, ".cursor", "hooks.json")

	settings := make(map[string]interface{})
	data, err := d.readFile(hooksPath)
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", hooksPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", hooksPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	if _, ok := settings["version"]; !ok {
		settings["version"] = 1
	}

	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		cleaned := removeCursorPopHooks(eventHooks)
		if d.DryRun && len(cleaned) < len(eventHooks) {
			d.installed = true
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	for _, h := range cursorPopHooks {
		hookEntry := map[string]interface{}{
			"command": h.command,
		}
		eventHooks, _ := hooks[h.event].([]interface{})
		eventHooks = append(eventHooks, hookEntry)
		hooks[h.event] = eventHooks
	}

	if err := d.mkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize hooks: %w", err)
	}

	if err := d.writeFile(hooksPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", hooksPath, err)
	}

	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Installed %d hook(s) in %s\n", len(cursorPopHooks), hooksPath)
	}
	return nil
}

// ----- Shared helpers --------------------------------------------------------

// removePopHooks filters out hook entries whose commands look like pop
// monitoring commands. Used to deduplicate when re-installing.
func removePopHooks(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		if !isPopHook(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// isPopHook returns true if any command in the hook entry references one of
// the pop pane-monitoring commands. Handles the nested Claude/Codex format.
func isPopHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	innerHooks, _ := entryMap["hooks"].([]interface{})
	for _, h := range innerHooks {
		hMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if cmd, _ := hMap["command"].(string); isPopHookCommand(cmd) {
			return true
		}
	}
	return false
}

// removeCursorPopHooks filters out Cursor-format hook entries whose commands
// look like pop monitoring commands.
func removeCursorPopHooks(entries []interface{}) []interface{} {
	var result []interface{}
	for _, entry := range entries {
		if !isCursorPopHook(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// isCursorPopHook returns true if a Cursor-format hook entry references one
// of the pop pane-monitoring commands.
func isCursorPopHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	cmd, _ := entryMap["command"].(string)
	return isPopHookCommand(cmd)
}

func isPopHookCommand(cmd string) bool {
	return strings.Contains(cmd, "pop monitor") ||
		strings.Contains(cmd, "pop pane set-status") ||
		strings.Contains(cmd, "pop pane set-topic") ||
		strings.Contains(cmd, "pop-status")
}

// ----- App state (state.json) ------------------------------------------------

// appState holds cross-run markers persisted at ~/.local/share/pop/state.json.
// Currently used only as a staleness marker for auto-updating integrations.
type appState struct {
	// BuildRevision is the vcs.revision of the binary that last successfully
	// ran ensureIntegrations. An empty value means no check has run yet.
	BuildRevision string `json:"build_revision"`
}

// defaultStatePath returns the path to state.json, respecting XDG_DATA_HOME.
// Mirrors the pattern used by history.DefaultHistoryPath and
// monitor.DefaultStatePath.
func defaultStatePath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		debug.Error("defaultStatePath: UserHomeDir: %v", err)
		return filepath.Join(".local", "share", "pop", "state.json")
	}
	return filepath.Join(home, ".local", "share", "pop", "state.json")
}

// loadAppState reads state.json. A missing or corrupt file is treated as an
// empty state, so the auto-updater re-checks everything on the next launch.
func loadAppState() *appState {
	path := defaultStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			debug.Error("loadAppState: %v", err)
		}
		return &appState{}
	}
	var s appState
	if err := json.Unmarshal(data, &s); err != nil {
		debug.Error("loadAppState: unmarshal: %v", err)
		return &appState{}
	}
	return &s
}

// saveAppState writes state.json, creating parent directories as needed.
func saveAppState(s *appState) error {
	path := defaultStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ----- Auto-update integrations ---------------------------------------------

// integrationAgents is the hardcoded list of agents that ensureIntegrations
// checks on each startup. Small enough that a registry is overkill; changes
// here must also update the integrateCmd ValidArgs list.
var integrationAgents = []string{"claude", "codex", "pi", "opencode", "cursor"}

// ensureIntegrations checks whether installed integration components are stale
// against the currently running binary's VCS revision and reconciles them to
// the merged Integration baseline. Returns warnings to surface in the picker
// for any failures.
//
// Behavior:
//   - "dev" builds (no VCS revision) are skipped entirely — nothing to track.
//   - state.json is read once; if its revision matches the current binary,
//     nothing runs (the common fast path after first check on a given build).
//   - For each integrated agent, refresh re-renders pop-owned baseline
//     components, installs any baseline-listed component that is missing (when
//     no conflict), and skips components omitted from the merged baseline.
//     Conflicts are skipped without overwriting; refresh never prompts and
//     never removes opted-out artifacts.
//   - state.json is stamped with the current revision only when every stale
//     component refreshed successfully. Partial failures leave the old
//     revision in place so the next launch retries.
//   - Warnings are returned only for components demonstrably installed but
//     failing to check or update. Everything else is silent.
func ensureIntegrations() []string {
	return ensureIntegrationsForRevision(buildRevision())
}

// integrationUpdateResult reports what updateStaleIntegrations did during
// one pass: per-component outcomes for CLI display and warnings to surface.
type integrationUpdateResult struct {
	Outcomes []integrateOutcome
	Warnings []string
}

// updateStaleIntegrations is the pure core of the per-component refresh flow.
// For each integrated agent it reconciles every supported catalog component
// against the merged Integration baseline: re-render installed pop-owned
// artifacts, install missing baseline-listed components, skip baseline
// omissions and conflicts without overwriting. Non-integrated agents are left
// alone. Each pair with a reportable state produces one outcome.
//
// The function does not read or write state.json, does not gate on the
// binary revision, and does not emit output. Callers layer those behaviors
// on top (see ensureIntegrationsForRevisionWith and runIntegrateUpdateExistingWith).
func updateStaleIntegrations(newDry, newReal func() *integrateDeps) integrationUpdateResult {
	baseline, err := integrationBaselineLoader()
	if err != nil {
		debug.Error("updateStaleIntegrations: baseline: %v", err)
		return integrationUpdateResult{
			Warnings: []string{fmt.Sprintf("failed to load integration config: %v", err)},
		}
	}
	baselineSet := baselineComponentSet(baseline)

	var result integrationUpdateResult

	for _, agent := range integrationAgents {
		integrated, err := agentIntegratedViaStatusWiring(newDry, agent)
		if err != nil {
			debug.Error("updateStaleIntegrations: integrated check %s: %v", agent, err)
			continue
		}
		if !integrated {
			continue
		}

		agentUpdated := false
		for _, comp := range integrationCatalog {
			compOutcomes, warning := refreshComponent(newDry, newReal, agent, comp.id, baselineSet)
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			if len(compOutcomes) > 0 {
				result.Outcomes = append(result.Outcomes, compOutcomes...)
				for _, o := range compOutcomes {
					if o.Label == "updated" || o.Label == "added" {
						agentUpdated = true
					}
				}
			}
		}
		if agentUpdated {
			debug.Log("updateStaleIntegrations: updated %s integration", agent)
		}
	}

	return result
}

func baselineComponentSet(baseline []ComponentID) map[ComponentID]bool {
	set := make(map[ComponentID]bool, len(baseline))
	for _, id := range baseline {
		set[id] = true
	}
	return set
}

// agentIntegratedViaStatusWiring reports whether an agent has pop status wiring
// installed. Refresh only reconciles agents that are already integrated.
func agentIntegratedViaStatusWiring(newDry func() *integrateDeps, agent string) (bool, error) {
	dryDeps := newDry()
	if err := runIntegrateWith(dryDeps, agent); err != nil {
		return false, err
	}
	return dryDeps.installed, nil
}

// refreshComponent reconciles a single (agent, component) pair against the
// merged Integration baseline, returning an outcome and any warning to surface.
// A component not supported by the agent is skipped silently (nil outcome, no
// warning). Callers must only invoke this for agents already integrated via
// status wiring.
func refreshComponent(newDry, newReal func() *integrateDeps, agent string, id ComponentID, baselineSet map[ComponentID]bool) ([]integrateOutcome, string) {
	comp, ok := lookupComponent(id)
	if !ok {
		return nil, ""
	}
	if !comp.supported(agent) {
		return nil, ""
	}
	switch id {
	case ComponentStatusWiring:
		outcome, warning := refreshStatusWiring(newDry, newReal, agent)
		if outcome == nil {
			return nil, warning
		}
		return []integrateOutcome{*outcome}, warning
	default:
		if !baselineSet[id] {
			outcomes, err := optOutSkipOutcomes(newReal(), agent, id)
			if err != nil {
				debug.Error("refreshComponent: opt-out outcomes %s/%s: %v", agent, id, err)
				return nil, ""
			}
			return outcomes, ""
		}
		return refreshFileComponent(newDry, newReal, agent, id)
	}
}

// refreshStatusWiring refreshes the status-wiring component for an integrated
// agent. It dry-runs the install to learn changed state and, only when stale,
// performs the real install. Warnings are returned solely for an agent
// demonstrably installed but failing to check or update.
func refreshStatusWiring(newDry, newReal func() *integrateDeps, agent string) (*integrateOutcome, string) {
	dryDeps := newDry()
	if err := runIntegrateWith(dryDeps, agent); err != nil {
		debug.Error("refreshStatusWiring: dry-run %s: %v", agent, err)
		if dryDeps.installed {
			return nil, fmt.Sprintf("failed to check %s integration: %v", agent, err)
		}
		return nil, ""
	}
	if !dryDeps.installed {
		return nil, ""
	}
	if !dryDeps.changed {
		o := statusWiringOutcome(agent, "already current")
		return &o, ""
	}

	realDeps := newReal()
	realDeps.stdout = nil
	if err := runIntegrateWith(realDeps, agent); err != nil {
		debug.Error("refreshStatusWiring: update %s: %v", agent, err)
		return nil, fmt.Sprintf("failed to update %s integration (see pop.log)", agent)
	}
	o := statusWiringOutcome(agent, "updated")
	return &o, ""
}

// refreshFileComponent reconciles a baseline-listed file-based skill component
// for an integrated agent. It inspects the link installer's render tree and
// the agent-location symlinks (through the read-only dry-run deps) to decide:
//
//   - conflict (an unowned entry shadows pop's) → "skipped (conflict)";
//   - not installed → install and report "added";
//   - installed but current → "already current";
//   - installed and stale → re-render and re-link via installFileComponent,
//     which also migrates any lingering copy-mode artifact to a symlink.
//
// Warnings follow the status-wiring contract: only an installed component that
// fails its staleness check or its re-install warns; everything else is silent.
func refreshFileComponent(newDry, newReal func() *integrateDeps, agent string, id ComponentID) ([]integrateOutcome, string) {
	checkDeps := newDry()
	home, err := checkDeps.userHomeDir()
	if err != nil {
		debug.Error("refreshFileComponent: home %s/%s: %v", agent, id, err)
		return nil, ""
	}

	prefix := checkDeps.resolveSkillsPrefix()
	preConflict, err := preInstallSkillConflicts(checkDeps, home, agent, id, prefix)
	if err != nil {
		debug.Error("refreshFileComponent: conflict check %s/%s: %v", agent, id, err)
		return nil, ""
	}

	installedBefore, err := fileComponentInstalledNames(checkDeps, home, id, agent)
	if err != nil {
		debug.Error("refreshFileComponent: installed check %s/%s: %v", agent, id, err)
		return nil, ""
	}
	if len(preConflict) > 0 {
		return fileComponentOutcomesInCatalogOrder(
			agent, id, prefix, installedBefore, false, nil, preConflict, nil, nil,
		), ""
	}
	if len(installedBefore) == 0 {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s not installed — adding", agent, id)
		}
		realDeps := newReal()
		realDeps.stdout = nil
		if err := installFileComponent(realDeps, home, id, agent); err != nil {
			debug.Error("refreshFileComponent: add %s/%s: %v", agent, id, err)
			return nil, fmt.Sprintf("failed to add %s %s integration (see pop.log)", agent, id)
		}
		return fileComponentOutcomesInCatalogOrder(
			agent, id, prefix, nil, true, realDeps.prunedStale, preConflict, nil, nil,
		), ""
	}

	staleBefore, err := fileComponentStaleResolved(checkDeps, home, id, agent, installedBefore)
	if err != nil {
		debug.Error("refreshFileComponent: stale check %s/%s: %v", agent, id, err)
		return nil, fmt.Sprintf("failed to check %s %s integration: %v", agent, id, err)
	}
	if !staleBefore {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s installed and current — no-op", agent, id)
		}
		return fileComponentOutcomesInCatalogOrder(
			agent, id, prefix, installedBefore, false, nil, preConflict, nil, nil,
		), ""
	}

	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s stale — refreshing", agent, id)
	}
	realDeps := newReal()
	realDeps.stdout = nil
	if err := installFileComponent(realDeps, home, id, agent); err != nil {
		debug.Error("refreshFileComponent: update %s/%s: %v", agent, id, err)
		return nil, fmt.Sprintf("failed to update %s %s integration (see pop.log)", agent, id)
	}
	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s refreshed", agent, id)
	}
	postConflict, err := preInstallSkillConflicts(realDeps, home, agent, id, prefix)
	if err != nil {
		debug.Error("refreshFileComponent: post conflict check %s/%s: %v", agent, id, err)
	}
	return fileComponentOutcomesInCatalogOrder(
		agent, id, prefix, installedBefore, true, realDeps.prunedStale,
		nil, postConflict, nil,
	), ""
}

// stampRevisionIfSuccess writes the given revision to state.json, but only
// when no warnings were produced. Partial failures deliberately leave the
// previous revision in place so the next launch retries. A "dev" revision
// is never stamped — dev builds have no stable staleness marker.
func stampRevisionIfSuccess(rev string, result integrationUpdateResult) {
	if len(result.Warnings) > 0 || rev == "dev" {
		return
	}
	state := loadAppState()
	if state.BuildRevision == rev {
		return // already stamped
	}
	state.BuildRevision = rev
	if err := saveAppState(state); err != nil {
		debug.Error("stampRevisionIfSuccess: save state: %v", err)
	}
}

// ensureIntegrationsForRevision is the testable core of ensureIntegrations,
// parameterized by the binary revision so tests can drive it directly without
// having to stamp a VCS revision into the test binary.
func ensureIntegrationsForRevision(rev string) []string {
	return ensureIntegrationsForRevisionWith(rev, dryRunIntegrateDeps, defaultIntegrateDeps)
}

// ensureIntegrationsForRevisionWith is the fully testable variant: callers
// provide factories that construct dry-run and real deps. Tests use this to
// share a fake filesystem between the check and update phases and to inject
// write failures for the retry-semantics cases.
func ensureIntegrationsForRevisionWith(rev string, newDry, newReal func() *integrateDeps) []string {
	if rev == "dev" {
		return nil // nothing to track
	}
	state := loadAppState()
	if state.BuildRevision == rev {
		return nil // already checked this binary
	}

	result := updateStaleIntegrations(newDry, newReal)
	stampRevisionIfSuccess(rev, result)
	return result.Warnings
}

// runIntegrateUpdateExisting is the CLI entry point behind
// `pop integrate --update-existing`. Unlike the runtime auto-update
// (ensureIntegrations), it always runs regardless of state.json and prints
// its outcome to stdout/stderr so install-time hooks can surface the result
// in the user's shell.
//
// Invariants:
//   - Silent on no-op (no installed agents, or everything already current).
//   - One "✓ Updated <agent> integration" line per agent that was refreshed.
//   - One "⚠ ..." line per warning, written to stderr.
//   - On full success, state.json is stamped with the current revision so
//     the runtime auto-update short-circuits on subsequent launches.
//   - Always exits 0. Non-fatal by design: a broken integration must not
//     block `make install` or `brew install` from completing.
func runIntegrateUpdateExisting() error {
	err := runIntegrateUpdateExistingWith(
		buildRevision(),
		dryRunIntegrateDeps,
		defaultIntegrateDeps,
		os.Stdout,
		os.Stderr,
		integrateVerbose,
	)
	// Run by `make install` with the freshly installed binary: reap any stale
	// daemon now so new daemon commands (e.g. set-topic) work on the next hook,
	// not only after the next interactive picker (ADR 0021).
	refreshMonitorDaemonIfRunning()
	return err
}

// runIntegrateUpdateExistingWith is the fully testable variant. Tests inject
// fake deps factories, a fixed revision, bytes.Buffer writers, and a verbose flag.
func runIntegrateUpdateExistingWith(
	rev string,
	newDry, newReal func() *integrateDeps,
	stdout, stderr io.Writer,
	verbose bool,
) error {
	result := updateStaleIntegrations(newDry, newReal)

	printIntegrateOutcomes(stdout, result.Outcomes, verbose, false /* update-existing path */)

	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "⚠ %s\n", w)
	}

	stampRevisionIfSuccess(rev, result)
	return nil
}
