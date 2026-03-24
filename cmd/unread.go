package cmd

import (
	"fmt"
	"os"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/monitor"
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
	var opts []ui.PickerOption
	if note := workingCountNote(); note != "" {
		opts = append(opts, ui.WithAttentionEmptyNote(note))
	}
	result, err := ui.RunAttention("unread", panes, attentionCallbacks(), buildUnreadPanes, opts...)
	if err != nil {
		return err
	}

	switch result.Action {
	case ui.ActionSwitchToPane:
		if result.Selected != nil {
			hist, _ := history.Load(history.DefaultHistoryPath())
			if hist == nil {
				hist = &history.History{}
			}
			hist.Record(sessionHistoryPath(result.Selected.Context, hist))
			hist.Save()
			return switchToTmuxTarget(result.Selected.Path)
		}
	case ui.ActionCancel:
		os.Exit(1)
	}

	return nil
}

func workingCountNote() string {
	state := loadMonitorState()
	if state == nil {
		return ""
	}
	count := 0
	for _, e := range state.Panes {
		if e.Status == monitor.StatusWorking {
			count++
		}
	}
	if count == 0 {
		return ""
	}
	if count == 1 {
		return "1 pane still working"
	}
	return fmt.Sprintf("%d panes still working", count)
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
