package cmd

import (
	"fmt"
	"os"
	runtimedebug "runtime/debug"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "pop",
	Short: "Project and worktree switcher for tmux",
	Long: `pop is a CLI tool for quickly switching between projects and git worktrees.

It integrates with tmux to provide popup-based fuzzy selection of:
  - Projects from configured directories
  - Git worktrees in the current repository

Configure your projects in ~/.config/pop/config.toml`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command
func Execute() {
	debug.Init()
	defer debug.Close()

	// Recover from panics so the stack trace can be shown in the error screen
	// (and logged) instead of vanishing with the popup.
	defer func() {
		if r := recover(); r != nil {
			trace := string(runtimedebug.Stack())
			debug.Error("panic: %v\n%s", r, trace)
			ui.ShowError(fmt.Errorf("panic: %v", r), trace)
			os.Exit(1)
		}
	}()

	if err := rootCmd.Execute(); err != nil {
		debug.Error("%v", err)
		ui.ShowError(err, "")
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.config/pop/config.toml)")
}
