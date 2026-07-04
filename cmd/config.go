package cmd

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/glebglazov/pop/config"
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
)

var configKeysCmd = &cobra.Command{
	Use:   "keys [path]",
	Short: "List the keys each config surface accepts",
	Long: `List the keys each config surface accepts.

pop has three config surfaces:
  global    the user's central config.toml (~/.config/pop/config.toml)
  pop-toml  the committed repo-root .pop.toml (shared, checked in)
  repo      a [repo."<path>"] override block in the global config.toml

With no arguments, top-level keys for all three surfaces are printed. Restrict
to one surface with --scope. Pass a dotted key path to drill into that table's
keys (defaults to the global surface); combine with --all to recurse into every
nested table. Without a path, --all dumps the whole surface as flat dotted keys.

A path is dotted like the --all output (e.g. repo.workbenches). The map-key
placeholder <name> is optional — write it or omit it, both resolve.

Examples:
  pop config keys                      # top-level keys, all surfaces
  pop config keys --scope pop-toml     # top-level keys of .pop.toml
  pop config keys worktree             # keys inside the [worktree] table
  pop config keys repo.workbenches     # drill two levels: [repo] then workbenches
  pop config keys effort.heavy         # keys of an effort tier ([effort.<agent>.heavy])
  pop config keys workbenches --all    # every key under [[workbenches]], recursively
  pop config keys --scope global --all # the whole global surface, dotted`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConfigKeys,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configKeysCmd)
	configKeysCmd.Flags().StringVar(&configKeysScope, "scope", "",
		"limit to one surface: global | pop-toml | repo (default: all)")
	configKeysCmd.Flags().BoolVar(&configKeysAll, "all", false,
		"recurse into nested tables (flat, dotted keys)")
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
		return renderTableKeys(os.Stdout, scope, args[0], configKeysAll)
	}

	// No table: list top-level (or, with --all, the whole surface).
	scopes := config.ConfigScopes
	if configKeysScope != "" {
		scopes = []config.ConfigScope{config.ConfigScope(configKeysScope)}
	}
	renderScopeKeys(os.Stdout, scopes, configKeysAll)
	return nil
}

// renderScopeKeys prints each scope's keys under a scope heading. When recurse
// is set, nested tables are flattened into dotted keys.
func renderScopeKeys(out io.Writer, scopes []config.ConfigScope, recurse bool) {
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
		writeKeyTable(out, docs)
	}
}

// renderTableKeys prints the keys inside a table of a scope, addressed by a
// dotted path (e.g. "worktree" or "repo.workbenches").
func renderTableKeys(out io.Writer, scope config.ConfigScope, path string, recurse bool) error {
	docs, found, isTable, leafType := config.TableKeyDocs(scope, path, recurse)
	if !found {
		return fmt.Errorf("unknown key path %q in %s scope (see `pop config keys --scope %s --all`)",
			path, scope, scope)
	}
	if !isTable {
		return fmt.Errorf("%q is a %s in %s scope, not a table — it has no sub-keys", path, leafType, scope)
	}
	fmt.Fprintf(out, "%s · [%s]:\n", config.ScopeTitle(scope), path)
	writeKeyTable(out, docs)
	return nil
}

// writeKeyTable renders docs as an aligned KEY / TYPE / DESCRIPTION table.
func writeKeyTable(out io.Writer, docs []config.ConfigKeyDoc) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, d := range docs {
		desc := d.Desc
		if desc == "" {
			desc = "-"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.Key, d.Type, desc)
	}
	tw.Flush()
}
