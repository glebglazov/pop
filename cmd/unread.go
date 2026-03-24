package cmd

import (
	"github.com/spf13/cobra"
)

// Deprecated: use `pop dashboard` instead. Kept as a hidden alias so
// existing keybindings and scripts that call `pop unread` keep working.
var unreadCmd = &cobra.Command{
	Use:    "unread",
	Short:  "Show active agent panes (alias for dashboard)",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runDashboard,
}

func init() {
	rootCmd.AddCommand(unreadCmd)
}
