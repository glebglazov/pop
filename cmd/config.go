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

var configKeysScope string

var configKeysCmd = &cobra.Command{
	Use:   "keys",
	Short: "List the keys each config surface accepts",
	Long: `List the keys each config surface accepts.

pop has three config surfaces:
  global    the user's central config.toml (~/.config/pop/config.toml)
  pop-toml  the committed repo-root .pop.toml (shared, checked in)
  repo      a [repo."<path>"] override block in the global config.toml

With no flag, keys for all three are printed. Restrict to one with --scope.`,
	Args: cobra.NoArgs,
	RunE: runConfigKeys,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configKeysCmd)
	configKeysCmd.Flags().StringVar(&configKeysScope, "scope", "",
		"limit to one surface: global | pop-toml | repo (default: all)")
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
	scopes := config.ConfigScopes
	if configKeysScope != "" {
		if _, ok := config.ScopeKeyDocs(config.ConfigScope(configKeysScope)); !ok {
			return fmt.Errorf("unknown scope %q (want one of: global, pop-toml, repo)", configKeysScope)
		}
		scopes = []config.ConfigScope{config.ConfigScope(configKeysScope)}
	}
	renderConfigKeys(os.Stdout, scopes)
	return nil
}

// renderConfigKeys prints each scope's keys as an aligned KEY / TYPE /
// DESCRIPTION table under a scope heading.
func renderConfigKeys(out io.Writer, scopes []config.ConfigScope) {
	for i, scope := range scopes {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s:\n", config.ScopeTitle(scope))
		docs, _ := config.ScopeKeyDocs(scope)
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
}
