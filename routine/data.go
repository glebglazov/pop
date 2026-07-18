package routine

import (
	"path/filepath"
)

const (
	manifestFileName = "manifest.json"
	promptFileName   = "prompt.md"
	memoryDirName    = "memory"
	runsDirName      = "runs"
)

// popDataDir returns pop's base data directory, respecting XDG_DATA_HOME with the
// ~/.local/share/pop fallback, consistent with task storage paths.
func popDataDir(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop")
	}
	return filepath.Join(home, ".local", "share", "pop")
}

func routinesRoot(d *Deps) string {
	return filepath.Join(popDataDir(d), "routines")
}

func routineDir(d *Deps, id string) string {
	return filepath.Join(routinesRoot(d), id)
}
