package cmd

import (
	"fmt"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/spf13/cobra"
)

// pop project tmux-bindings prints the Base tmux config fragment for a user
// whose own tmux.conf means pop contributed nothing to their server
// (ADR-0199 decision 9). Read-only: it writes nothing and touches no server.
var projectTmuxBindingsCmd = &cobra.Command{
	Use:   "tmux-bindings",
	Short: "Print pop's tmux binding fragment for pasting or sourcing",
	Long: `Print the Base tmux config fragment pop would apply when starting a
server for a user with no tmux config of their own (ADR-0199).

Paste into your tmux.conf or source into a live server. Pop never installs
these bindings into a server it did not configure.`,
	Args: cobra.NoArgs,
	RunE: runProjectTmuxBindings,
}

func init() {
	projectCmd.AddCommand(projectTmuxBindingsCmd)
}

func runProjectTmuxBindings(cmd *cobra.Command, _ []string) error {
	_, err := fmt.Fprint(cmd.OutOrStdout(), tmuxmod.BindingFragment())
	return err
}
