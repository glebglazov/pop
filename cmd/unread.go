package cmd

import (
	"fmt"
	"os"

	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var unreadCmd = &cobra.Command{
	Use:   "unread",
	Short: "Show panes that need your attention",
	Args:  cobra.NoArgs,
	RunE:  runUnread,
}

func init() {
	rootCmd.AddCommand(unreadCmd)
}

func runUnread(cmd *cobra.Command, args []string) error {
	panes := buildUnreadPanes()
	if len(panes) == 0 {
		fmt.Println("No panes need attention")
		return nil
	}

	result, err := ui.RunAttention("unread", panes, capturePanePreview)
	if err != nil {
		return err
	}

	switch result.Action {
	case ui.ActionSwitchToPane:
		if result.Selected != nil {
			return switchToTmuxTarget(result.Selected.Path)
		}
	case ui.ActionCancel:
		os.Exit(1)
	}

	return nil
}

func buildUnreadPanes() []ui.AttentionPane {
	state := loadMonitorState()
	if state == nil {
		return nil
	}

	entries := state.PanesNeedingAttention()
	paneCommands := tmuxPaneCommands()
	panes := make([]ui.AttentionPane, 0, len(entries))
	for _, entry := range entries {
		name := entry.Session + " " + entry.PaneID
		if cmd, ok := paneCommands[entry.PaneID]; ok {
			name = entry.Session + " " + entry.PaneID + " (" + cmd + ")"
		}
		panes = append(panes, ui.AttentionPane{
			PaneID:  entry.PaneID,
			Session: entry.Session,
			Name:    name,
		})
	}
	return panes
}
