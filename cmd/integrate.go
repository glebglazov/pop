package cmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// componentOutcome records what happened to one (agent, component) pair during
// an integrate run. It drives the human-readable output: one line per pair,
// grouped by agent order, with the reason stated.
type componentOutcome struct {
	Agent     string
	Component ComponentID
	Label     string // "added" | "updated" | "already current" | "skipped (opted out)" | "skipped (conflict at <path>)"
}

// verboseOnly returns true when this outcome should be suppressed unless the
// --verbose flag is on. "already current" is always a no-op. "skipped (opted
// out)" is noisy on the update-existing path (many agent×component pairs) so it
// is suppressed there too; it is always shown on the explicit install path
// (bounded list, informative about what was left out).
func (o componentOutcome) verboseOnly(updateExistingPath bool) bool {
	switch o.Label {
	case "already current":
		return true
	case "skipped (opted out)":
		return updateExistingPath
	default:
		return false
	}
}

// printComponentOutcomes writes one line per outcome to out, filtered by
// verbose and path. "already current" is always suppressed without verbose.
// "skipped (opted out)" is suppressed without verbose on the update-existing
// path. When all outcomes are suppressed a "nothing to do" summary is printed.
func printComponentOutcomes(out io.Writer, outcomes []componentOutcome, verbose, explicitPath bool) {
	if out == nil {
		return
	}
	printed := 0
	for _, o := range outcomes {
		if o.verboseOnly(!explicitPath) && !verbose {
			continue
		}
		fmt.Fprintf(out, "  %s  %s  %s\n", o.Agent, o.Component, o.Label)
		printed++
	}
	if printed == 0 {
		fmt.Fprintln(out, "nothing to do")
	}
}

// installLabel maps pre-install state to the outcome label used in output.
func installLabel(isNew, isUpdate bool) string {
	switch {
	case isNew:
		return "added"
	case isUpdate:
		return "updated"
	default:
		return "already current"
	}
}

// installComponentCollectOutcome installs one component for agent and returns
// the outcome (what happened and why). The install functions' own stdout output
// is suppressed so callers can print a single outcome line instead.
func installComponentCollectOutcome(d *integrateDeps, home, agent string, comp integrationComponent) (componentOutcome, error) {
	id := comp.id

	if comp.install != nil {
		// Status-wiring style: dry-run first to detect add vs update vs current.
		dryD := withDryRun(d)
		if err := comp.install(dryD, home, agent); err != nil {
			return componentOutcome{}, err
		}
		// Real install with output suppressed — outcome line is printed by caller.
		quietD := *d
		quietD.stdout = nil
		if err := comp.install(&quietD, home, agent); err != nil {
			return componentOutcome{}, err
		}
		label := installLabel(!dryD.installed, dryD.installed && dryD.changed)
		return componentOutcome{Agent: agent, Component: id, Label: label}, nil
	}

	// File-based component: check conflict and pre-install state before installing.
	conflictPath, conflict, err := componentConflict(d, home, id, agent)
	if err != nil {
		return componentOutcome{}, fmt.Errorf("conflict check for %s/%s: %w", agent, id, err)
	}
	if conflict {
		return componentOutcome{Agent: agent, Component: id, Label: "skipped (conflict at " + conflictPath + ")"}, nil
	}

	installed, err := fileComponentInstalled(d, home, id, agent)
	if err != nil {
		return componentOutcome{}, fmt.Errorf("installed check for %s/%s: %w", agent, id, err)
	}
	stale := true // not installed = always fresh
	if installed {
		if stale, err = fileComponentStale(d, home, id, agent); err != nil {
			return componentOutcome{}, fmt.Errorf("stale check for %s/%s: %w", agent, id, err)
		}
	}

	quietD := *d
	quietD.stdout = nil
	if err := installFileComponent(&quietD, home, id, agent); err != nil {
		return componentOutcome{}, err
	}
	label := installLabel(!installed, installed && stale)
	return componentOutcome{Agent: agent, Component: id, Label: label}, nil
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

	// File-based component installer (link installer, ADR 0011). dataDir
	// resolves pop's data directory root (the parent of integrations/);
	// symlink/readlink/lstatMode manage the agent-location symlinks and the
	// ownership check. readDirNames lists an agent-location directory so the
	// installer can prune stale-named pop-owned entries (set subtraction,
	// ADR 0063).
	dataDir      func() (string, error)
	symlink      func(target, link string) error
	readlink     func(string) (string, error)
	lstatMode    func(string) (os.FileMode, error)
	readDirNames func(string) ([]string, error)

	// skillPrefix is the resolved skill-name prefix for rendered skills (the
	// `<prefix>` in `<prefix><base>`, ADR 0063). A nil pointer means "unset" →
	// config.DefaultSkillPrefix (`pop-`); a non-nil pointer (including an empty
	// string) is used verbatim, so skill_prefix = "" installs bare base names.
	// Resolved once from [integrations] skill_prefix so the render-tree names,
	// the agent-location link names, and conflict detection all agree within a
	// run. Read through resolveSkillPrefix so a zero-value deps (tests) defaults
	// to `pop-`.
	skillPrefix *string

	// loadOptOut / saveOptOut persist the per-agent Component opt-out set
	// (negative consent, ADR 0064 slice 08): the default-on components the user
	// declined via `--no-*` at install or `pop integrate remove`. Refresh reads
	// the set so it never re-adds or updates an opted-out component; a bare
	// `pop integrate <agent>` rewrites the set to exactly the declined components,
	// so no flags clears it. Backed by state.json in production; tests inject an
	// in-memory store. A nil pointer (zero-value deps) disables persistence.
	loadOptOut func(agent string) map[ComponentID]bool
	saveOptOut func(agent string, optOut map[ComponentID]bool) error

	// Dry-run mode: set DryRun=true to turn writeFile into a comparator.
	// `installed` and `changed` are output fields filled in during the run.
	DryRun    bool
	changed   bool
	installed bool
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
		loadOptOut:   loadAgentOptOut,
		saveOptOut:   saveAgentOptOut,
	}
	d.skillPrefix = loadSkillPrefix()
	return d
}

// osReadDirNames lists the immediate entry names under dir, sorted. A missing
// directory is not an error — it reports no entries, so the stale-name prune is
// a no-op on a fresh agent.
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

// loadSkillPrefix resolves [integrations] skill_prefix from the user's config,
// returning a pointer suitable for integrateDeps.skillPrefix. A config that
// fails to load (missing file, malformed TOML) yields nil → the default
// `pop-` prefix, so a broken config never blocks integrate or changes the
// installed names.
func loadSkillPrefix() *string {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Log("loadSkillPrefix: config load failed (%v); using default prefix %q", err, config.DefaultSkillPrefix)
		return nil
	}
	p := cfg.ResolveSkillPrefix()
	return &p
}

// resolveSkillPrefix returns the resolved skill-name prefix for this deps,
// defaulting to config.DefaultSkillPrefix (`pop-`) when unset so a zero-value
// integrateDeps (constructed directly in tests) renders the canonical names.
func (d *integrateDeps) resolveSkillPrefix() string {
	if d == nil || d.skillPrefix == nil {
		return config.DefaultSkillPrefix
	}
	return *d.skillPrefix
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
		// The resolved skill prefix and the directory listing are read-only,
		// so they pass through unchanged — the dry-run check must render and
		// enumerate exactly what the real run would.
		skillPrefix:  base.skillPrefix,
		readDirNames: base.readDirNames,
		// Opt-out reads are how refresh decides whether to re-add or skip a
		// component, so the read seam passes through. Persistence never happens
		// during a dry-run, so saveOptOut is a no-op.
		loadOptOut: base.loadOptOut,
		saveOptOut: func(string, map[ComponentID]bool) error { return nil },
		DryRun:     true,
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

// integratePaneSkill is the deprecated --pane-skill flag. The pane skill now
// installs by default (ADR 0064), so the flag is a no-op kept only to print a
// deprecation notice instead of erroring on an unknown flag.
var integratePaneSkill bool

// integrateTaskSkills is the deprecated --task-skills flag. The task planning
// skills now install by default (ADR 0064); the flag is a no-op that prints a
// deprecation notice.
var integrateTaskSkills bool

// integrateNoPaneSkill is the --no-pane-skill opt-out flag: exclude the pane
// skill from this invocation's default set.
var integrateNoPaneSkill bool

// integrateNoTaskSkills is the --no-task-skills opt-out flag: exclude the task
// planning skills from this invocation's default set.
var integrateNoTaskSkills bool

// integrateVerbose enables verbose output: "already current" no-ops and
// "skipped (opted out)" outcomes on the --update-existing path are shown
// instead of suppressed.
var integrateVerbose bool

var integrateCmd = &cobra.Command{
	Use:   "integrate [agent]",
	Short: "Install pop status wiring for a coding agent",
	Long: `Install pop's full integration toolkit for a coding agent.

By default, with no flags, integrate installs everything for the agent — no
prompting (ADR 0064):

  - Status wiring: makes the agent report pane status to pop's monitor. It
    changes no agent behavior.
  - Pane skill: lets the agent drive tmux panes. It lands as a symlink into
    pop's data directory: a skill directory for claude, pi, and cursor (e.g.
    ~/.claude/skills/pop-tmux-pane) and a flat file for opencode
    (~/.config/opencode/agent/pop-tmux-pane.md).
  - Task planning skills (grill-with-docs, grill-consolidate, to-prd,
    to-tasks), each a multi-file skill directory symlinked into pop's data
    directory (e.g. ~/.claude/skills/pop-grill-with-docs/).

A component an agent cannot host is skipped silently rather than installed in a
degraded form: codex hosts neither skill, and opencode hosts the pane skill but
not the task planning skills.

Decline a component:

  --no-pane-skill   Skip the pane skill.
  --no-task-skills  Skip the task planning skills.

Declining is remembered: a component skipped with --no-* (or removed later with
'pop integrate remove') is recorded as a per-agent opt-out, so refresh never
re-adds it. Re-running a bare 'pop integrate <agent>' with no --no-* flags clears
the opt-outs and re-asserts the full default set.

The positive --pane-skill / --task-skills flags are deprecated no-ops kept for
compatibility — the components they named are now installed by default. Passing
one prints a deprecation notice and otherwise has no effect.

Conflicts are never overwritten: a same-named skill at the agent's location that
pop does not own is skipped and reported, leaving the user's version untouched.

Supported agents:
  claude    Install pane monitoring hooks in ~/.claude/settings.json.
  codex     Install pane monitoring hooks in ~/.codex/hooks.json.
  pi        Install a pane monitoring extension at
            ~/.pi/agent/extensions/pop-status-sync.ts.
  opencode  Install a pane monitoring plugin at
            ~/.config/opencode/plugins/pop-status-sync.ts.
  cursor    Install pane monitoring hooks in ~/.cursor/hooks.json.

Re-running the command for an agent is idempotent: existing pop status wiring
for that agent is refreshed to the current version, and unrelated hooks are
preserved.

With --update-existing, no agent argument is expected: pop detects which
agents are already integrated and refreshes them to the current binary's
embedded content. For each integrated agent it also installs any default
component that is missing and not opted-out, never prompting and never touching
an opted-out component. Agents with no pop integration at all are left alone.
This is the command that 'make install' and the Homebrew post_install hook run
after copying a new binary into place.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if integrateUpdateExisting {
			if len(args) > 0 {
				return fmt.Errorf("--update-existing does not accept an agent argument")
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("requires exactly 1 argument: agent name (claude, codex, pi, opencode, or cursor)")
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
identifiers: status-wiring, pane-skill, task-skills.

Removing a default component records a per-agent opt-out, so a later refresh
('pop integrate --update-existing' or the picker-launch auto-update) does not
re-add it. A bare 'pop integrate <agent>' clears the opt-outs and reinstalls
the full default set.

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
	integrateCmd.Flags().BoolVar(&integrateNoPaneSkill, "no-pane-skill", false,
		"Skip the pane skill (installed by default)")
	integrateCmd.Flags().BoolVar(&integrateNoTaskSkills, "no-task-skills", false,
		"Skip the task planning skills (installed by default)")
	integrateCmd.Flags().BoolVar(&integratePaneSkill, "pane-skill", false,
		"Deprecated: the pane skill installs by default; this flag is a no-op")
	integrateCmd.Flags().BoolVar(&integrateTaskSkills, "task-skills", false,
		"Deprecated: the task planning skills install by default; this flag is a no-op")
	integrateCmd.Flags().BoolVar(&integrateVerbose, "verbose", false,
		"Show all outcomes including already-current no-ops and opted-out components")
	integrateCmd.AddCommand(integrateRemoveCmd)
	rootCmd.AddCommand(integrateCmd)
}

func runIntegrate(cmd *cobra.Command, args []string) error {
	if integrateUpdateExisting {
		return runIntegrateUpdateExisting()
	}
	return runIntegrateInstall(
		defaultIntegrateDeps(),
		args[0],
		integratePaneSkill, integrateTaskSkills,
		integrateNoPaneSkill, integrateNoTaskSkills,
	)
}

// runIntegrateInstall is the testable core behind `pop integrate <agent>`
// (ADR 0064). It emits a deprecation notice for any positive component flag,
// translates the `--no-*` opt-out flags into an opt-out set, and installs the
// resolved default set. The positive flags carry no install meaning — the
// components they named install by default.
func runIntegrateInstall(d *integrateDeps, agent string, paneSkillFlag, taskSkillsFlag, noPaneSkill, noTaskSkills bool) error {
	noteDeprecatedPositiveFlags(d.stdout, paneSkillFlag, taskSkillsFlag)

	var optOut []ComponentID
	if noPaneSkill {
		optOut = append(optOut, ComponentPaneSkill)
	}
	if noTaskSkills {
		optOut = append(optOut, ComponentTaskSkills)
	}
	return runIntegrateComponents(d, agent, optOut)
}

// noteDeprecatedPositiveFlags prints a deprecation notice for each positive
// component flag that was set. The flags are no-ops — the components they named
// install by default (ADR 0064) — so this is the only effect they have.
func noteDeprecatedPositiveFlags(out io.Writer, paneSkill, taskSkills bool) {
	if out == nil {
		return
	}
	if paneSkill {
		fmt.Fprintln(out, "Note: --pane-skill is deprecated and now a no-op — the pane skill installs by default. Use --no-pane-skill to opt out.")
	}
	if taskSkills {
		fmt.Fprintln(out, "Note: --task-skills is deprecated and now a no-op — the task planning skills install by default. Use --no-task-skills to opt out.")
	}
}

// runIntegrateComponents installs the default component set for an agent
// (ADR 0064): the core status wiring plus every default opt-in component except
// those named in optOut. There is no prompting. The set is resolved here; the
// per-component install (skipping unsupported agents and conflicts) lives in
// installComponentSet.
func runIntegrateComponents(d *integrateDeps, agent string, optOut []ComponentID) error {
	skip := map[ComponentID]bool{}
	for _, id := range optOut {
		skip[id] = true
	}
	var ids []ComponentID
	for _, id := range defaultComponentIDs() {
		if skip[id] {
			if d.logf != nil {
				d.logf("runIntegrateComponents: %s/%s opted out via flag — excluding from install", strings.ToLower(agent), id)
			}
			continue
		}
		ids = append(ids, id)
	}
	if err := installComponentSet(d, agent, ids); err != nil {
		return err
	}
	// Persist this invocation's opt-out intent as the agent's full opt-out set
	// (negative consent, ADR 0064 slice 08): exactly the declined default
	// components. A bare integrate passes no opt-outs, so this clears any prior
	// opt-out and re-asserts the full default set on the next refresh.
	return persistInstallOptOut(d, agent, optOut)
}

// persistInstallOptOut records the opt-out set an install invocation expresses:
// exactly the declined default-on components, replacing any prior set for the
// agent. With an empty optOut this clears the agent's opt-outs entirely. Only
// default-on components can be opted out — the core status wiring and unknown
// ids are ignored. New path logs per slice 01.
func persistInstallOptOut(d *integrateDeps, agent string, optOut []ComponentID) error {
	if d.saveOptOut == nil {
		return nil
	}
	set := map[ComponentID]bool{}
	for _, id := range optOut {
		if comp, ok := lookupComponent(id); ok && comp.defaultOn {
			set[id] = true
		}
	}
	if d.logf != nil {
		d.logf("persistInstallOptOut: %s opt-out set -> %v", strings.ToLower(agent), sortedComponentIDs(set))
	}
	return d.saveOptOut(strings.ToLower(agent), set)
}

// sortedComponentIDs returns a name set's keys as a sorted []string for stable
// logs.
func sortedComponentIDs(set map[ComponentID]bool) []string {
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, string(id))
	}
	sort.Strings(out)
	return out
}

// installComponentSet installs the core status wiring plus exactly the given
// opt-in components for an agent, with no prompting. It is the shared install
// engine behind the default integrate path and the exact-set installs tests
// drive directly.
//
//   - The core status wiring always installs first (the integrate verb implies
//     it).
//   - A component the agent cannot host is skipped silently — pop never installs
//     a degraded version (ADR 0064).
//   - Conflicts (a same-named entry pop does not own) are skipped, never
//     overwritten — installFileComponent enforces this per top-level entry.
//
// New install paths log per slice 01.
func installComponentSet(d *integrateDeps, agent string, ids []ComponentID) error {
	agent = strings.ToLower(agent)

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

	if d.logf != nil {
		d.logf("installComponentSet: agent=%s components=%v", agent, ids)
	}

	if err := core.install(d, home, agent); err != nil {
		return err
	}
	for _, id := range ids {
		comp, ok := lookupComponent(id)
		if !ok {
			return fmt.Errorf("unknown component %q", id)
		}
		if !comp.supported(agent) {
			if d.logf != nil {
				d.logf("installComponentSet: %s/%s not supported — skipping (no degraded install)", agent, id)
			}
			continue
		}
		// A component carrying its own install func is applied directly;
		// file-based components go through the link installer.
		if comp.install != nil {
			if err := comp.install(d, home, agent); err != nil {
				return err
			}
			continue
		}
		if err := installFileComponent(d, home, id, agent); err != nil {
			return err
		}
	}
	return nil
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
// to derive a pane topic from the prompt. It installs whenever the core status
// wiring installs — no extra opt-in — and rides the same idempotent
// install/remove/refresh paths (both commands match isPopHookCommand).
var popHooks = []hookSpec{
	{"SessionStart", "pop pane set-status clear 2>/dev/null || true"},
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
	return setFrontmatterField(content, "name", name)
}

// popOwnedField is the name-independent frontmatter marker pop writes into
// every rendered skill. Ownership of a real (copy-mode) entry is decided by
// this marker rather than the `pop-` name prefix (ADR 0011, skill-prefix slice
// 02), decoupling ownership from the configurable skill-name prefix.
const popOwnedField = "pop-owned"

// injectOwnershipMarker guarantees the YAML frontmatter carries the
// name-independent `pop-owned: true` ownership marker, creating frontmatter if
// none exists. See popOwnedField.
func injectOwnershipMarker(content string) string {
	return setFrontmatterField(content, popOwnedField, "true")
}

// setFrontmatterField guarantees the YAML frontmatter contains `<key>: <value>`.
// An existing entry for the key is replaced; absent frontmatter is created;
// malformed frontmatter (no closing fence) is left untouched.
func setFrontmatterField(content, key, value string) string {
	field := key + ": " + value
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter — wrap the content in one.
		return fmt.Sprintf("---\n%s\n---\n%s", field, content)
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
	// Replace existing `<key>:` line if present.
	prefix := key + ":"
	for i := 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			lines[i] = field
			return strings.Join(lines, "\n")
		}
	}
	// Otherwise insert the field directly after the opening `---`.
	out := append([]string{lines[0], field}, lines[1:]...)
	return strings.Join(out, "\n")
}

// frontmatterHasOwnershipMarker reports whether content's YAML frontmatter
// carries `pop-owned: true` — the canonical ownership signal for a real
// copy-mode entry. Any other value, a missing key, or absent frontmatter means
// not owned.
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
type appState struct {
	// BuildRevision is the vcs.revision of the binary that last successfully
	// ran ensureIntegrations. An empty value means no check has run yet.
	BuildRevision string `json:"build_revision"`

	// OptOuts records, per agent (lowercased), the default-on components the
	// user has declined: `--no-<component>` at install time or `pop integrate
	// remove <agent> <component>` (ADR 0064 slice 08). Refresh consults this set
	// so it never re-adds or updates an opted-out component. A bare `pop
	// integrate <agent>` rewrites the agent's entry to exactly the declined
	// components, so re-running with no `--no-*` flags clears it.
	OptOuts map[string][]string `json:"opt_outs,omitempty"`
}

// loadAgentOptOut returns the persisted Component opt-out set for an agent,
// read from state.json. A missing file, missing entry, or corrupt state all
// yield an empty set (nothing opted out), so a broken state never silently
// suppresses a default component.
func loadAgentOptOut(agent string) map[ComponentID]bool {
	state := loadAppState()
	set := map[ComponentID]bool{}
	for _, id := range state.OptOuts[strings.ToLower(agent)] {
		set[ComponentID(id)] = true
	}
	return set
}

// saveAgentOptOut replaces the persisted opt-out set for an agent in
// state.json. An empty (or nil) set removes the agent's entry entirely, which
// is how a bare `pop integrate <agent>` clears prior opt-outs. The stored list
// is sorted for a stable on-disk representation.
func saveAgentOptOut(agent string, optOut map[ComponentID]bool) error {
	agent = strings.ToLower(agent)
	state := loadAppState()
	if len(optOut) == 0 {
		if _, ok := state.OptOuts[agent]; !ok {
			return nil // already clear — avoid a needless rewrite
		}
		delete(state.OptOuts, agent)
		return saveAppState(state)
	}
	ids := make([]string, 0, len(optOut))
	for id := range optOut {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	if state.OptOuts == nil {
		state.OptOuts = map[string][]string{}
	}
	state.OptOuts[agent] = ids
	return saveAppState(state)
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
// against the currently running binary's VCS revision and refreshes any that
// need it, per (agent, component) pair. Returns warnings to surface in the
// picker for any failures.
//
// Behavior:
//   - "dev" builds (no VCS revision) are skipped entirely — nothing to track.
//   - state.json is read once; if its revision matches the current binary,
//     nothing runs (the common fast path after first check on a given build).
//   - For each (agent, component) pair the refresh asks whether the component
//     is installed and stale. Installed+stale triggers a re-render; an absent
//     component is never added, and conflict and not-supported pairs are
//     skipped silently.
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
	Outcomes []componentOutcome
	Warnings []string
}

// updateStaleIntegrations is the pure core of the per-component refresh flow.
// For every (agent, component) pair in the catalog it asks: is the component
// installed for this agent and stale against the embedded sources? Only an
// installed-and-stale pair is re-rendered; absent components are never added,
// and not-supported pairs produce no outcome. Each pair that has a reportable
// state (updated, already current, opted out, conflict) produces one outcome.
//
// The function does not read or write state.json, does not gate on the
// binary revision, and does not emit output. Callers layer those behaviors
// on top (see ensureIntegrationsForRevisionWith and runIntegrateUpdateExistingWith).
func updateStaleIntegrations(newDry, newReal func() *integrateDeps) integrationUpdateResult {
	var result integrationUpdateResult

	for _, agent := range integrationAgents {
		agentUpdated := false
		for _, comp := range integrationCatalog {
			outcome, warning := refreshComponent(newDry, newReal, agent, comp.id)
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			if outcome != nil {
				result.Outcomes = append(result.Outcomes, *outcome)
				if outcome.Label == "updated" || outcome.Label == "added" {
					agentUpdated = true
				}
			}
		}
		if agentUpdated {
			debug.Log("updateStaleIntegrations: updated %s integration", agent)
		}
	}

	return result
}

// refreshComponent refreshes a single (agent, component) pair if it is
// installed and stale, returning an outcome and any warning to surface.
// A component not supported by the agent is skipped silently (nil outcome, no
// warning). The status wiring and the file-based skills are refreshed through
// their own staleness seams.
func refreshComponent(newDry, newReal func() *integrateDeps, agent string, id ComponentID) (outcome *componentOutcome, warning string) {
	comp, ok := lookupComponent(id)
	if !ok {
		return nil, ""
	}
	if !comp.supported(agent) {
		return nil, "" // not supported — skip silently, never a degraded install
	}
	switch id {
	case ComponentStatusWiring:
		return refreshStatusWiring(newDry, newReal, agent)
	default:
		return refreshFileComponent(newDry, newReal, agent, id)
	}
}

// refreshStatusWiring refreshes the status-wiring component for an agent. It
// dry-runs the install to learn installed/changed state and, only when both
// hold, performs the real install. Warnings are returned solely for an agent
// demonstrably installed but failing to check or update; an uninstalled agent
// returns a "skipped (opted out)" outcome.
func refreshStatusWiring(newDry, newReal func() *integrateDeps, agent string) (outcome *componentOutcome, warning string) {
	dryDeps := newDry()
	if err := runIntegrateWith(dryDeps, agent); err != nil {
		debug.Error("refreshStatusWiring: dry-run %s: %v", agent, err)
		if dryDeps.installed {
			return nil, fmt.Sprintf("failed to check %s integration: %v", agent, err)
		}
		return nil, ""
	}
	if !dryDeps.installed {
		return &componentOutcome{Agent: agent, Component: ComponentStatusWiring, Label: "skipped (opted out)"}, ""
	}
	if !dryDeps.changed {
		return &componentOutcome{Agent: agent, Component: ComponentStatusWiring, Label: "already current"}, ""
	}

	realDeps := newReal()
	realDeps.stdout = nil // refresh runs silently on success
	if err := runIntegrateWith(realDeps, agent); err != nil {
		debug.Error("refreshStatusWiring: update %s: %v", agent, err)
		return nil, fmt.Sprintf("failed to update %s integration (see pop.log)", agent)
	}
	return &componentOutcome{Agent: agent, Component: ComponentStatusWiring, Label: "updated"}, ""
}

// refreshFileComponent reconciles a file-based skill component for an agent. It
// inspects the link installer's render tree and the agent-location symlinks
// (through the read-only dry-run deps) to decide:
//
//   - opted out (persisted negative consent) → "skipped (opted out)", never
//     re-add or update;
//   - conflict (an unowned entry shadows pop's) → "skipped (conflict at <path>)";
//   - installed but current → "already current";
//   - installed and stale → re-render and re-link via installFileComponent,
//     which also migrates any lingering copy-mode artifact to a symlink;
//   - not installed, default-on, the agent already has pop integration, and not
//     opted out → add the missing default (ADR 0064 slice 08);
//   - not installed and the agent has no pop integration → skip (leave it
//     alone).
//
// Warnings follow the status-wiring contract: only an installed component that
// fails its staleness check or an add/update that fails warns; everything else
// is silent.
func refreshFileComponent(newDry, newReal func() *integrateDeps, agent string, id ComponentID) (outcome *componentOutcome, warning string) {
	checkDeps := newDry()
	home, err := checkDeps.userHomeDir()
	if err != nil {
		debug.Error("refreshFileComponent: home %s/%s: %v", agent, id, err)
		return nil, "" // can't resolve home — treat as not actionable
	}

	// Persisted opt-out (negative consent, ADR 0064 slice 08): a declined
	// component is never re-added and never updated, regardless of on-disk state.
	if optedOut(checkDeps, agent, id) {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s opted out — skip (never re-add or update)", agent, id)
		}
		return &componentOutcome{Agent: agent, Component: id, Label: "skipped (opted out)"}, ""
	}

	if conflictPath, conflict, err := componentConflict(checkDeps, home, id, agent); err != nil {
		debug.Error("refreshFileComponent: conflict check %s/%s: %v", agent, id, err)
		return nil, ""
	} else if conflict {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s skipped — conflict at %s (not owned by pop)", agent, id, conflictPath)
		}
		return &componentOutcome{Agent: agent, Component: id, Label: "skipped (conflict at " + conflictPath + ")"}, ""
	}

	// Opted-in check (name-agnostic): is any pop-owned artifact for this
	// component present at the agent location, under whatever name it was last
	// installed with? An empty set means the component is not installed — either
	// the user removed it (handled by the opt-out short-circuit above) or it was
	// never added.
	installedNames, err := fileComponentInstalledNames(checkDeps, home, id, agent)
	if err != nil {
		debug.Error("refreshFileComponent: installed check %s/%s: %v", agent, id, err)
		return nil, ""
	}
	if len(installedNames) == 0 {
		return addMissingDefaultComponent(newReal, checkDeps, home, agent, id)
	}

	// Reconcile decision: installed state ≠ expected resolved state is stale
	// (ADR 0063). Divergence covers the resolved install name (an owned entry
	// under the old/renamed name, or the correctly-named entry missing) as well
	// as the rendered content — not just byte-equality under the current name.
	stale, err := fileComponentStaleResolved(checkDeps, home, id, agent, installedNames)
	if err != nil {
		debug.Error("refreshFileComponent: stale check %s/%s: %v", agent, id, err)
		// Installed but the check failed — warn (installed-but-failing).
		return nil, fmt.Sprintf("failed to check %s %s integration: %v", agent, id, err)
	}
	if !stale {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s installed and current — no-op", agent, id)
		}
		return &componentOutcome{Agent: agent, Component: id, Label: "already current"}, ""
	}

	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s stale — refreshing", agent, id)
	}
	realDeps := newReal()
	realDeps.stdout = nil // refresh runs silently on success
	if err := installFileComponent(realDeps, home, id, agent); err != nil {
		debug.Error("refreshFileComponent: update %s/%s: %v", agent, id, err)
		return nil, fmt.Sprintf("failed to update %s %s integration (see pop.log)", agent, id)
	}
	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s refreshed", agent, id)
	}
	return &componentOutcome{Agent: agent, Component: id, Label: "updated"}, ""
}

// addMissingDefaultComponent installs a default-on component that is absent for
// an agent, but only when that agent already has pop integration (its status
// wiring is present). This is the refresh-adds-missing behavior (ADR 0064 slice
// 08): a release that ships a new default component, or an agent integrated
// before a component became default, picks it up on the next refresh without any
// prompt. An agent with no pop integration at all is left alone, and a
// non-default component is never auto-added. Callers must already have ruled out
// opt-out and conflict. New path logs per slice 01.
func addMissingDefaultComponent(newReal func() *integrateDeps, checkDeps *integrateDeps, home, agent string, id ComponentID) (outcome *componentOutcome, warning string) {
	comp, ok := lookupComponent(id)
	if !ok || !comp.defaultOn {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s not installed and not a default component — skip", agent, id)
		}
		return nil, "" // only default-on components are auto-added
	}
	if !agentIntegrated(checkDeps, home, agent) {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s not installed and agent has no pop integration — leave alone", agent, id)
		}
		return nil, ""
	}
	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s missing default on an integrated agent — adding", agent, id)
	}
	realDeps := newReal()
	realDeps.stdout = nil // refresh runs silently on success
	if err := installFileComponent(realDeps, home, id, agent); err != nil {
		debug.Error("refreshFileComponent: add %s/%s: %v", agent, id, err)
		return nil, fmt.Sprintf("failed to add %s %s integration (see pop.log)", agent, id)
	}
	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s added", agent, id)
	}
	return &componentOutcome{Agent: agent, Component: id, Label: "added"}, ""
}

// optedOut reports whether the agent has a persisted opt-out for a component
// (negative consent, ADR 0064 slice 08). A deps with no opt-out seam (zero
// value) reports nothing opted out.
func optedOut(d *integrateDeps, agent string, id ComponentID) bool {
	if d.loadOptOut == nil {
		return false
	}
	return d.loadOptOut(strings.ToLower(agent))[id]
}

// agentIntegrated reports whether the agent already has pop integration — the
// gate for adding missing default components on refresh. Status wiring is the
// integration marker the integrate verb always installs, so its presence is the
// "this agent is integrated" signal. A check error is treated as not integrated
// (leave the agent alone rather than touch an indeterminate state).
func agentIntegrated(d *integrateDeps, home, agent string) bool {
	installed, err := statusWiringInstalled(d, home, agent)
	if err != nil {
		debug.Error("agentIntegrated: %s: %v", agent, err)
		return false
	}
	return installed
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

	printComponentOutcomes(stdout, result.Outcomes, verbose, false /* update-existing path */)

	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "⚠ %s\n", w)
	}

	stampRevisionIfSuccess(rev, result)
	return nil
}
