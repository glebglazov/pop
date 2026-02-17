package cmd

import (
	"os"

	"github.com/glebglazov/pop/debug"
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
}

// Execute runs the root command
func Execute() {
	debug.Init()
	defer debug.Close()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.config/pop/config.toml)")
}
