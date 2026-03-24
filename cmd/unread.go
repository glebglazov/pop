package cmd

import (
	"github.com/spf13/cobra"
)

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
