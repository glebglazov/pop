package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/session"
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
	Tmux deps.Tmux

	SessionName func(path string) string
	LoadHistory func() (*history.History, error)
	SaveHistory func(h *history.History) error
	InTmux      func() bool
}

// DefaultSwitchDeps returns SwitchDeps wired to real production implementations.
func DefaultSwitchDeps() *SwitchDeps {
	return &SwitchDeps{
		FS:          deps.NewRealFileSystem(),
		Tmux:        defaultTmux,
		SessionName: project.SessionName,
		LoadHistory: func() (*history.History, error) {
			return history.Load(history.DefaultHistoryPath())
		},
		SaveHistory: func(h *history.History) error { return h.Save() },
		InTmux:      func() bool { return os.Getenv("TMUX") != "" },
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
	hist.Record(path)
	if err := d.SaveHistory(hist); err != nil {
		debug.Error("project switch: save history: %v", err)
	}

	return session.AttachWith(&session.Deps{
		Tmux:   d.Tmux,
		InTmux: d.InTmux,
	}, d.SessionName(path), path)
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
