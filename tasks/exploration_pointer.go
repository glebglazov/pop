package tasks

import (
	"path/filepath"
	"strings"
)

// explorationBlockView is the set's Exploration report as the prompts that hand
// it over render it: the document to read, never a word of what it says. One
// pass writes one file (ADR-0262), so there is no timestamp to print and nothing
// to be superseded by — where it is, is the whole pointer.
type explorationBlockView struct {
	HasExploration bool
	Path           string
}

// explorationBlock resolves that view for a set, and the empty view — which
// renders nothing at all — for a set that has never been explored.
//
// An unreadable or empty document is a set with no report, the way an empty
// spec is (readSpec): the builders after the pass are handed a path only when
// there is a map at the end of it.
func explorationBlock(d *Deps, setDir string) explorationBlockView {
	if d == nil {
		d = defaultDeps
	}
	if d == nil || d.FS == nil || strings.TrimSpace(setDir) == "" {
		return explorationBlockView{}
	}
	path := filepath.Join(setDir, ExplorationFileName)
	body, err := d.FS.ReadFile(path)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		return explorationBlockView{}
	}
	return explorationBlockView{HasExploration: true, Path: path}
}
