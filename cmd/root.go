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
	Version:       buildVersion(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

// buildRevision returns the raw VCS revision embedded by `go build`, or "dev"
// if no revision is available (e.g. `go run`, or a binary built without VCS
// stamping). Used by the auto-update integrations path as a staleness marker.
func buildRevision() string {
	info, ok := runtimedebug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return "dev"
}

// buildVersion reads VCS stamps embedded by `go build` and returns a short
// commit SHA, optionally suffixed with "-dirty" and the commit timestamp.
func buildVersion() string {
	info, ok := runtimedebug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev := buildRevision()
	if rev == "dev" {
		return "dev"
	}
	var when string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	if when != "" {
		return rev + " (" + when + ")"
	}
	return rev
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
