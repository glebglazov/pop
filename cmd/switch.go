package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/spf13/cobra"
)

var projectSwitchCmd = &cobra.Command{
	Use:   "switch <dir>",
	Short: "Switch to the tmux session for a directory",
	Long: `Attaches to — or creates, then attaches to — the tmux session for the
given directory, and records it in project history.

This is the non-picker entry point: external tooling (e.g. worktree
creation scripts) can hand a fresh path to pop so it still lands in
history and sorts by recency in the project picker.

Example:
  pop project switch ~/Dev/work/app`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectSwitch,
}

func init() {
	projectCmd.AddCommand(projectSwitchCmd)
}

// SwitchDeps holds dependencies for the project switch command.
type SwitchDeps struct {
	FS   deps.FileSystem
	Tmux tmuxmod.Tmux

	SessionName func(path string) string
	LoadHistory func() (*history.History, error)
}

// DefaultSwitchDeps returns SwitchDeps wired to real production implementations.
func DefaultSwitchDeps() *SwitchDeps {
	return &SwitchDeps{
		FS:          deps.NewRealFileSystem(),
		Tmux:        defaultTmuxMod,
		SessionName: checkoutSessionName,
		LoadHistory: func() (*history.History, error) {
			return history.LoadWith(cmdHistoryDeps())
		},
	}
}

func runProjectSwitch(cmd *cobra.Command, args []string) error {
	return RunProjectSwitch(DefaultSwitchDeps(), args[0])
}

// RunProjectSwitch records dir in project history and attaches to (creating
// if needed) its tmux session. Mirrors the picker's confirm path for callers
// outside the picker.
func RunProjectSwitch(d *SwitchDeps, dir string) error {
	path, err := canonicalDir(d.FS, dir)
	if err != nil {
		return err
	}

	hist, err := d.LoadHistory()
	if err != nil {
		debug.Error("project switch: load history: %v", err)
	}
	if hist == nil {
		hist = &history.History{}
	}
	if err := hist.Record(path); err != nil {
		debug.Error("project switch: record history: %v", err)
	}

	return tmuxmod.Attach(d.Tmux, d.SessionName(path), path)
}

// canonicalDir resolves dir to an absolute, symlink-free path and verifies it
// is an existing directory. History dedupes on symlink-resolved paths, so the
// canonical form is what must be recorded.
func canonicalDir(fs deps.FileSystem, dir string) (string, error) {
	path := dir
	if !filepath.IsAbs(path) {
		wd, err := fs.Getwd()
		if err != nil {
			return "", err
		}
		path = filepath.Join(wd, path)
	}
	if resolved, err := fs.EvalSymlinks(path); err == nil {
		path = resolved
	}
	info, err := fs.Stat(path)
	if err != nil {
		return "", fmt.Errorf("directory not found: %s", dir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}
	return path, nil
}
