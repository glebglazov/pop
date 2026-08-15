package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// This file is the shape of the override layer: one pop-written document built
// like the user's own config.toml — global keys at the top, per-repository
// blocks below (ADR-0212 decision 7) — read as the two scopes an override may
// land at (decision 3).
//
// An override is not a rung of the resolution ladder (decision 2). It is laid
// over whatever the ladder resolved and it always wins, and within the layer the
// ladder's own specificity rule holds again: a repository-scoped entry beats a
// global one. That is why the file's global half is read here as well as merged
// into the effective Config by applyConfigLayerMerge. Merged, its values are
// indistinguishable from config.toml's own, so at repo scope they would ride the
// ladder's bottom rung under any committed declaration; read here, they stay
// above every rung.

// overrideRepoSection is the top-level table holding the per-repository blocks.
// It is spelled `repo`, the word the user's config.toml already uses, because
// the point of the file is that whoever can read one can read the other on
// sight. What differs is the key: a block here is keyed by Repository identity
// rather than by a checkout path, so every worktree of a repository reads one
// answer (ADR-0191's keying, kept).
const overrideRepoSection = "repo"

// overrideLayer is config.override.toml held as both of the things its readers
// need it to be.
type overrideLayer struct {
	// doc is the whole document as written: what the write side edits key by key
	// and what the Config dashboard reads a key's override out of.
	doc map[string]any
	// scoped is the same document decoded through the config schema — the global
	// half as pop's own fields, and Repo as one block per repository.
	scoped Config
}

// loadOverrideLayer reads the override file once and answers for both scopes. An
// absent file is not an error: it is the state of a machine that has overridden
// nothing.
func loadOverrideLayer(d *Deps) (overrideLayer, error) {
	file := overrideConfigFile(d)
	text, err := file.read(d)
	if err != nil {
		return overrideLayer{}, err
	}
	layer := overrideLayer{doc: map[string]any{}}
	if _, err := toml.Decode(text, &layer.doc); err != nil {
		return overrideLayer{}, fmt.Errorf("parse %s %q: %w", file.label, file.path, err)
	}
	if _, err := toml.Decode(text, &layer.scoped); err != nil {
		return overrideLayer{}, fmt.Errorf("parse %s %q: %w", file.label, file.path, err)
	}
	return layer, nil
}

// globalDoc is the document without its per-repository blocks — the half whose
// keys are dotted config paths. A block left in it would read as a global
// override of a key spelled `repo.<identity>.<key>`, which is no config key at
// all: the blocks are addressed by repository, never by path.
func (l overrideLayer) globalDoc() map[string]any {
	if _, ok := l.doc[overrideRepoSection]; !ok {
		return l.doc
	}
	out := make(map[string]any, len(l.doc))
	for name, value := range l.doc {
		if name == overrideRepoSection {
			continue
		}
		out[name] = value
	}
	return out
}

// globalScope is what the override states at global scope for the shared
// repo-scope key set. Only a key with a global home can be stated there — the
// blueprint library is the one such key — so preferred_workbench, which has no
// global spelling, is overridable at repository scope alone.
func (l overrideLayer) globalScope() RepoScopeConfig {
	return RepoScopeConfig{Workbenches: l.scoped.Workbenches}
}

// repoBlock returns the block filed for one repository identity. Keys are
// matched by identity rather than by text, so a block answers for every worktree
// of its repository however the key that files it was spelled — and so a key
// written before a repository moved still finds it. When more than one key
// resolves to the same identity the last wins, as it does for the user's own
// [repo."<path>"] blocks.
func (l overrideLayer) repoBlock(d *Deps, identity string) (RepoOverrideConfig, bool) {
	var block RepoOverrideConfig
	var found bool
	for key, b := range l.scoped.Repo {
		if repoIdentity(d, key) != identity {
			continue
		}
		block, found = b, true
	}
	return block, found
}

// repoBlockLegalKeys is what one per-repository block may hold: the shared
// repo-scope key set plus the two keys that live only in a [repo] block. It is
// reflected from the same schema that decodes a hand-authored block, so the
// override layer accepts exactly what config.toml accepts and neither surface
// can drift from the other.
func repoBlockLegalKeys() map[string]bool {
	legal := repoScopeLegalKeys()
	legal["trunk"] = true    // [repo]-only machine topology, not shared
	legal["turn_cap"] = true // [repo]-only run bound, not shared (ADR-0191)
	return legal
}
