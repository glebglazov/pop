package routine

import (
	"path/filepath"
)

const (
	// stateFileName is the slim machine-state sidecar (ADR-0139): the pause bit
	// with its reason, creation time, and bound directory. Authored intent —
	// schedule, agents, effort — lives in prompt.md frontmatter instead.
	stateFileName = "state.json"
	// legacyManifestFileName is the pre-ADR-0139 combined manifest. It is never
	// read for intent; its presence without a state.json only earns a warning.
	legacyManifestFileName = "manifest.json"
	promptFileName         = "prompt.md"
	memoryDirName          = "memory"
	runsDirName            = "runs"
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
