package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

// configCmd is the `pop config` command group. Bare `pop config` prints help.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect pop configuration",
	Long: `Inspect pop configuration.

pop config keys lists the keys each config surface accepts, so you can learn
what is available without trial and error. The list is reflected directly from
the code that decodes each surface, so it never drifts from what actually
loads.`,
}

var (
	configKeysScope string
	configKeysAll   bool
	configKeysWhy   bool
)

var configKeysCmd = &cobra.Command{
	Use:   "keys [path]",
	Short: "List the keys each config surface accepts",
	Long: `List the keys each config surface accepts.

pop has three config surfaces:
  global    the user's central config.toml (~/.config/pop/config.toml)
  pop-toml  the committed repo-root .pop/config.toml (shared, checked in)
  repo      a [repo."<path>"] override block in the global config.toml

With no arguments, top-level keys for all three surfaces are printed. Restrict
to one surface with --scope. Pass a dotted key path to drill into that table's
keys (defaults to the global surface); combine with --all to recurse into every
nested table. Without a path, --all dumps the whole surface as flat dotted keys.

A path is dotted like the --all output (e.g. repo.workbenches). The map-key
placeholder <name> is optional — write it or omit it, both resolve.

--why layers each key's declared reach over the catalog: per-actor lines saying
what shape the key takes for that actor, or why it takes none. Keys that declare
no reach are listed exactly as without the flag. Reach never replaces the schema
listing — it sits over it.

In the repo scope, keys pop can write itself (via pop config repo set) are marked
[settable], from the same reflection that backs the setter.

A key a human may override from pop itself is marked [override: <scope>], naming
the scope the override lands at. The mark comes from the key's own override tag,
so the catalog lists exactly the overridable keys.

Examples:
  pop config keys                      # top-level keys, all surfaces
  pop config keys --scope pop-toml     # top-level keys of .pop/config.toml
  pop config keys worktree             # keys inside the [worktree] table
  pop config keys repo.workbenches     # drill two levels: [repo] then workbenches
  pop config keys effort.heavy         # keys of an effort tier ([effort.<agent>.heavy])
  pop config keys workbenches --all    # every key under [[workbenches]], recursively
  pop config keys --scope global --all # the whole global surface, dotted
  pop config keys --scope repo --why   # repo keys with reach and settable marks`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConfigKeys,
}

var configShowJSON bool

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective config as TOML",
	Long: `Print pop's effective configuration as TOML.

Where pop config keys lists the schema you may set, pop config show prints the
values actually in effect: the global config.toml with its includes already
merged in (not re-listed as an includes array), and every [repo."<path>"] key
canonicalized to an absolute realpath (~ expanded, symlinks resolved). Effective
values only — no per-value provenance.

--json emits the same mirror as JSON instead, for machine consumers (e.g. the
to-tasks-here-and-now guard reading the resolved current_repo.trunk / .bare
without shell TOML-parsing).`,
	Args: cobra.NoArgs,
	RunE: runConfigShow,
}

// configRepoCmd groups the settings pop keeps per repository — the ones it
// writes itself, as opposed to the ones a human hand-authors.
var configRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Read and set the settings pop keeps per repository",
	Long: `Read and set the settings pop keeps per repository.

These are repo-scoped settings pop writes for you. A value set from any checkout
of a repository is read by every worktree of it, because it is filed under the
repository, not under the directory you happened to run in.

Pop writes them to its own runtime state (config.runtime.toml), never into your
hand-authored config.toml. A [repo."<path>"] block you write therefore always
overrides a value pop wrote, and never the other way around — to take a setting
back into your own hands, write the key in config.toml.

The settable keys are derived from the config schema itself, so what this command
can set never drifts from what the config accepts.`,
}

var configRepoSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a repo-scoped setting for the current repository",
	Long: `Set a repo-scoped setting for the repository of the current directory.

Examples:
  pop config repo set turn_cap 40   # bound one implementation attempt to 40 turns
  pop config repo set turn_cap 0    # give the bound back (0 stores no bound)`,
	Args:              cobra.ExactArgs(2),
	RunE:              runConfigRepoSet,
	ValidArgsFunction: completeRepoSettingKey,
}

var configRepoGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Show the repo-scoped settings in effect here, and where each came from",
	Long: `Show the repo-scoped settings in effect for the current repository.

Each key is reported with the value in effect and the layer that supplied it:
your hand-authored config.toml, the value pop wrote, or unset. With a key
argument, only that key is printed.`,
	Args:              cobra.MaximumNArgs(1),
	RunE:              runConfigRepoGet,
	ValidArgsFunction: completeRepoSettingKey,
}

// configDashboardCmd runs the Config dashboard as a top-level program. The same
// component opens inside pop's other TUIs, so what a human learns here carries
// over (ADR-0202 decision 10).
var configDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Browse the config keys you can override",
	Long: `Browse the config keys you can override, and what each one resolves to.

The left pane lists every override-exposed key — the keys pop config keys marks
[override: <scope>] — with its description beneath. Type to filter over the key
path and the description together. A marked row carries an override today.

The right pane previews the highlighted key in config format: the effective
value as TOML, the layer that produced it (the override layer, your config.toml,
a built-in default, or a fallthrough to another key), and, where an override is
in force, the value it is standing on.

This pass is read-only — no key edits anything yet.

It needs a terminal, so it refuses when stdout is redirected. A roomier popup
than the other dashboards suits it:

  bind-key C display-popup -E -w 80% -h 80% 'pop config dashboard'`,
	Args: cobra.NoArgs,
	RunE: runConfigDashboard,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configKeysCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configDashboardCmd)
	configCmd.AddCommand(configRepoCmd)
	configRepoCmd.AddCommand(configRepoSetCmd)
	configRepoCmd.AddCommand(configRepoGetCmd)
	configShowCmd.Flags().BoolVar(&configShowJSON, "json", false, "emit the effective config as JSON instead of TOML")
	configKeysCmd.Flags().StringVar(&configKeysScope, "scope", "",
		"limit to one surface: global | pop-toml | repo (default: all)")
	configKeysCmd.Flags().BoolVar(&configKeysAll, "all", false,
		"recurse into nested tables (flat, dotted keys)")
	configKeysCmd.Flags().BoolVar(&configKeysWhy, "why", false,
		"layer each key's declared reach over the catalog")
	_ = configKeysCmd.RegisterFlagCompletionFunc("scope",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{
				string(config.ScopeGlobal),
				string(config.ScopePopTOML),
				string(config.ScopeRepo),
			}, cobra.ShellCompDirectiveNoFileComp
		})
}

func runConfigKeys(cmd *cobra.Command, args []string) error {
	if configKeysScope != "" {
		if _, ok := config.ScopeKeyDocs(config.ConfigScope(configKeysScope)); !ok {
			return fmt.Errorf("unknown scope %q (want one of: global, pop-toml, repo)", configKeysScope)
		}
	}

	// Drill into a named table.
	if len(args) == 1 {
		scope := config.ScopeGlobal
		if configKeysScope != "" {
			scope = config.ConfigScope(configKeysScope)
		}
		return renderTableKeys(os.Stdout, scope, args[0], configKeysAll, configKeysWhy)
	}

	// No table: list top-level (or, with --all, the whole surface).
	scopes := config.ConfigScopes
	if configKeysScope != "" {
		scopes = []config.ConfigScope{config.ConfigScope(configKeysScope)}
	}
	renderScopeKeys(os.Stdout, scopes, configKeysAll, configKeysWhy)
	return nil
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	if configShowJSON {
		out, err := config.EffectiveJSON(config.DefaultConfigPathWith(cmdLayerDeps().configDeps()), currentRepoTrunk)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	}
	out, err := config.EffectiveTOML(config.DefaultConfigPathWith(cmdLayerDeps().configDeps()), currentRepoTrunk)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), out)
	return nil
}

func runConfigDashboard(cmd *cobra.Command, _ []string) error {
	d := cmdLayerDeps()
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPathWith(d.configDeps())
	}
	views, err := config.OverrideKeyViewsWith(d.configDeps(), path)
	if err != nil {
		return err
	}
	_, isTTY := tty.TerminalFd(os.Stdout)
	return runConfigDashboardWith(configDashboardRows(views), os.Stdin, os.Stdout, isTTY)
}

// runConfigDashboardWith refuses a non-terminal stdout rather than degrading to
// a listing: the TUI is the only surface this feature has in this pass, and a
// human who redirected it needs to be told that, not handed something else. The
// deferred `pop config override set` is what a script will use (ADR-0202
// decision 15).
func runConfigDashboardWith(rows []ui.ConfigDashboardRow, in io.Reader, out io.Writer, isTTY bool) error {
	if !isTTY {
		return errors.New("pop config dashboard needs a terminal: stdout is not a TTY. " +
			"Run it in a terminal (or a tmux popup); use `pop config keys` and `pop config show` for piped output")
	}
	return ui.RunConfigDashboard(rows, in, out)
}

// configDashboardRows adapts the resolved override views to the component's
// rows. The component holds no config knowledge — provenance and the words that
// tell two empty-looking states apart are decided in config, so `pop config
// dashboard` and every host that embeds the component say the same thing.
func configDashboardRows(views []config.OverrideKeyView) []ui.ConfigDashboardRow {
	rows := make([]ui.ConfigDashboardRow, 0, len(views))
	for _, view := range views {
		reach := make([]ui.ConfigDashboardReachLine, 0, len(view.Reach))
		for _, line := range view.Reach {
			reach = append(reach, ui.ConfigDashboardReachLine{Actor: line.Actor, Detail: line.Detail})
		}
		if len(reach) == 0 {
			reach = nil
		}
		rows = append(rows, ui.ConfigDashboardRow{
			Key:        view.Key,
			Desc:       view.Desc,
			Overridden: view.Overridden,
			Preview: ui.ConfigDashboardPreview{
				ValueTOML:        view.EffectiveTOML,
				Provenance:       view.Provenance(),
				Note:             view.Note,
				SourceTOML:       view.SourceTOML,
				SourceProvenance: view.SourceProvenance(),
				Reach:            reach,
			},
		})
	}
	return rows
}

// completeRepoSettingKey completes the first argument of the repo verbs with the
// settable key set, taken from the schema so completion cannot list a key the
// command would refuse.
func completeRepoSettingKey(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return config.RepoSettableKeys(), cobra.ShellCompDirectiveNoFileComp
}

func runConfigRepoSet(cmd *cobra.Command, args []string) error {
	d := cmdLayerDeps()
	checkout, err := d.DirOrGetwd()
	if err != nil {
		return err
	}
	return runConfigRepoSetWith(d.configDeps(), configRepoConfig(d), cmd.OutOrStdout(), checkout, args[0], args[1])
}

func runConfigRepoGet(cmd *cobra.Command, args []string) error {
	d := cmdLayerDeps()
	checkout, err := d.DirOrGetwd()
	if err != nil {
		return err
	}
	key := ""
	if len(args) == 1 {
		key = args[0]
	}
	return runConfigRepoGetWith(d.configDeps(), configRepoConfig(d), cmd.OutOrStdout(), checkout, key)
}

// configRepoConfig loads the global config for the repo verbs. A missing or
// broken config.toml is not fatal here: the pop-written layer is readable
// without it, and the hand-authored layer it would supply is then simply empty.
func configRepoConfig(d *Deps) *config.Config {
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPathWith(d.configDeps())
	}
	cfg, err := config.LoadWith(d.configDeps(), path)
	if err != nil {
		debug.Error("config repo: load %s: %v", path, err)
		return nil
	}
	return cfg
}

// runConfigRepoSetWith writes one repo-scoped setting and reports what it did:
// which repository the value was filed under, and which file now holds it. When
// a hand-authored block declares the same key, the write still happens (it is
// the lower layer, and the human may remove the block later) but the reply says
// plainly that it is not the value in effect.
func runConfigRepoSetWith(cd *config.Deps, cfg *config.Config, out io.Writer, checkout, key, value string) error {
	identity, err := config.SetRepoSettingWith(cd, checkout, key, value)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s = %s for repository %s\n", key, value, identity)
	fmt.Fprintf(out, "written to %s\n", config.DefaultRuntimeConfigPathWith(cd))

	settings, err := cfg.ResolveRepoSettings(cd, checkout)
	if err != nil {
		return err
	}
	for _, setting := range settings {
		if setting.Key != key || setting.Source != config.RepoSettingOverride {
			continue
		}
		fmt.Fprintf(out, "note: %s in your config.toml declares %s = %s and still wins\n",
			setting.Locus, key, setting.Value)
	}
	return nil
}

// runConfigRepoGetWith prints the settable keys with the value in effect for
// this repository and the layer that supplied it. Keys that declare a reach
// (ADR-0198) list each actor's shape or reason under the value row; a key with
// no declared reach prints exactly the KEY / VALUE / SOURCE line it always did.
func runConfigRepoGetWith(cd *config.Deps, cfg *config.Config, out io.Writer, checkout, key string) error {
	if key != "" {
		if !slices.Contains(config.RepoSettableKeys(), key) {
			return config.UnknownRepoSettingError(key)
		}
	}
	settings, err := cfg.ResolveRepoSettings(cd, checkout)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "repository %s\n", config.RepoIdentity(cd, checkout))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  KEY\tVALUE\tSOURCE\n")
	for _, setting := range settings {
		if key != "" && setting.Key != key {
			continue
		}
		value := setting.Value
		if value == "" {
			value = "-"
		}
		source := string(setting.Source)
		if setting.Locus != "" {
			source = fmt.Sprintf("%s (%s)", source, setting.Locus)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", setting.Key, value, source)
		if reach, ok := config.ConfigKeyReachFor(setting.Key); ok {
			for _, line := range reach.Lines {
				fmt.Fprintf(tw, "    %s\t%s\t\n", line.Actor, line.Detail)
			}
		}
	}
	return tw.Flush()
}

// currentRepoTrunk resolves the current repo's effective Trunk worktree for
// pop config show, from the current working directory. It is the config.
// CurrentTrunkFunc wired into the effective-config mirror. Outside any git repo
// it returns (nil, nil) so the current-repo section is omitted.
func currentRepoTrunk(cfg *config.Config) (*config.ResolvedTrunk, error) {
	cwd, err := cmdLayerDeps().DirOrGetwd()
	if err != nil {
		return nil, nil
	}
	return resolveCurrentRepoTrunk(cmdLayerDeps().tasksDeps(), cfg, cwd)
}

// resolveCurrentRepoTrunk resolves the current repo's effective Trunk worktree
// from checkoutPath, reusing pop's own trunk resolver
// (binding.ResolveTrunkPath) rather than re-deriving it: a bare repo's
// config-declared trunk = true worktree, or a non-bare repo's git-derived main
// worktree. It reads config + git only and never touches the task-binding
// store. The Bare flag is taken from git (whether the underlying repository is
// bare), independent of where the trunk came from. Outside any git repo it
// returns (nil, nil) so pop config show omits the current-repo section.
func resolveCurrentRepoTrunk(td *tasks.Deps, cfg *config.Config, checkoutPath string) (*config.ResolvedTrunk, error) {
	// GitMainWorktree doubles as the in-repo probe: it errors outside a git
	// repo and reports bareness from `git worktree list --porcelain`. A bare
	// repo lists its bare entry first, so bare is true even for a linked
	// worktree whose config names the trunk.
	_, bare, err := binding.GitMainWorktree(td, checkoutPath)
	if err != nil {
		return nil, nil
	}
	// Once we know we are inside a repo, resolve the trunk path. A bare repo
	// with no trunk = true override has none — surface bare with no trunk
	// rather than dropping the section entirely.
	trunkPath, _, terr := binding.ResolveTrunkPath(td, cfg, checkoutPath)
	if terr != nil {
		trunkPath = ""
	}
	return &config.ResolvedTrunk{Path: trunkPath, Bare: bare}, nil
}

// renderScopeKeys prints each scope's keys under a scope heading. When recurse
// is set, nested tables are flattened into dotted keys. When why is set, each
// key's declared reach is layered under its row (ADR-0198).
func renderScopeKeys(out io.Writer, scopes []config.ConfigScope, recurse, why bool) {
	for i, scope := range scopes {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s:\n", config.ScopeTitle(scope))
		var docs []config.ConfigKeyDoc
		if recurse {
			docs, _ = config.ScopeKeyDocsRecursive(scope)
		} else {
			docs, _ = config.ScopeKeyDocs(scope)
		}
		writeKeyTable(out, scope, docs, why)
	}
}

// renderTableKeys prints the keys inside a table of a scope, addressed by a
// dotted path (e.g. "worktree" or "repo.workbenches").
func renderTableKeys(out io.Writer, scope config.ConfigScope, path string, recurse, why bool) error {
	docs, found, isTable, leafType := config.TableKeyDocs(scope, path, recurse)
	if !found {
		return fmt.Errorf("unknown key path %q in %s scope (see `pop config keys --scope %s --all`)",
			path, scope, scope)
	}
	if !isTable {
		return fmt.Errorf("%q is a %s in %s scope, not a table — it has no sub-keys", path, leafType, scope)
	}
	fmt.Fprintf(out, "%s · [%s]:\n", config.ScopeTitle(scope), path)
	writeKeyTable(out, scope, docs, why)
	return nil
}

// repoSettableMarker is appended to a repo-scope key pop can write itself
// (ADR-0198 decision 6). Sourced from RepoSettableKeys — the same reflection
// that backs pop config repo set — so the two surfaces cannot disagree.
const repoSettableMarker = " [settable]"

// overrideMarker labels a key a human may override from pop, naming the scope
// the override lands at (ADR-0202 decision 3). Read off the row's own override
// tag, so the catalog and the override registry cannot disagree.
func overrideMarker(d config.ConfigKeyDoc) string {
	scope, ok := d.OverrideExposure()
	if !ok {
		return ""
	}
	return " [override: " + string(scope) + "]"
}

// writeKeyTable renders docs as an aligned KEY / TYPE / DESCRIPTION table.
// In the repo scope, keys in RepoSettableKeys carry repoSettableMarker, and an
// override-exposed key carries overrideMarker in every scope. When
// why is set, keys that declare a reach list each actor line under the row.
// Schema column widths come only from the schema rows, so a key that declares
// none is listed identically with and without why.
func writeKeyTable(out io.Writer, scope config.ConfigScope, docs []config.ConfigKeyDoc, why bool) {
	settable := map[string]bool{}
	if scope == config.ScopeRepo {
		for _, key := range config.RepoSettableKeys() {
			settable[key] = true
		}
	}
	var schemaBuf bytes.Buffer
	tw := tabwriter.NewWriter(&schemaBuf, 0, 0, 2, ' ', 0)
	reaches := make([][]config.ConfigKeyReachLine, len(docs))
	for i, d := range docs {
		desc := d.Desc
		if desc == "" {
			desc = "-"
		}
		key := d.Key
		if settable[d.Key] {
			key += repoSettableMarker
		}
		key += overrideMarker(d)
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", key, d.Type, desc)
		if why {
			if reach, ok := config.ConfigKeyReachFor(d.Key); ok {
				reaches[i] = reach.Lines
			}
		}
	}
	tw.Flush()
	if len(docs) == 0 {
		return
	}
	schemaLines := strings.Split(strings.TrimSuffix(schemaBuf.String(), "\n"), "\n")
	for i, line := range schemaLines {
		fmt.Fprintln(out, line)
		if i >= len(reaches) || len(reaches[i]) == 0 {
			continue
		}
		rtw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, rl := range reaches[i] {
			fmt.Fprintf(rtw, "    %s\t%s\t\n", rl.Actor, rl.Detail)
		}
		rtw.Flush()
	}
}
