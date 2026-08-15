package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/debug"
)

// This file is all that is left of config.runtime.toml: the read that retires
// it. Pop writes one config file now (ADR-0212 decisions 5 and 7), and nothing
// resolves through the runtime record any more — so a machine that still holds
// one would silently lose the trunk, the turn cap and the component opt-outs it
// carries. They fold into the override layer on the first config load instead,
// which is where each of them would land if it were stated today.
//
// The fold is a rank inversion, not a copy: a record that used to lose to every
// declaration of its scope comes back as a statement that beats them. That makes
// folding twice dangerous in a way the store's folds are not — a second fold
// would resurrect a value the human had since removed — so the source file is
// renamed aside the moment its values are in, and the fold is gated on the
// original's presence. The retired copy stays on disk as the only rollback there
// is (ADR-0174's rule that a fold never destroys what it read), under a name
// nothing reads.
//
// Within one fold, what the override layer already states wins: a key stated
// through pop is newer than a record pop wrote for itself, and re-running an
// interrupted fold must not undo it.

// retiredRuntimeFoldedSuffix names the retired copy. The fold is once-only
// because the source stops existing under the name it is looked for by, so the
// rename is both the commit and the marker.
const retiredRuntimeFoldedSuffix = ".folded"

// retiredRuntimeConfigPathWith is config.runtime.toml under the pop data dir:
// where an existing machine holds the record, and the one path in pop that still
// names the file.
func retiredRuntimeConfigPathWith(d *Deps) string {
	return filepath.Join(dataDirWith(d), "config.runtime.toml")
}

// foldRetiredRuntimeRecord folds a machine's runtime record into the override
// layer and retires the file, logging rather than propagating: a record that
// cannot be folded must not keep a human out of their config. The config load is
// where it hangs because that is the funnel every reader of a folded value
// passes, and it runs before the layers are read so the very load that folds
// already resolves through the result.
func foldRetiredRuntimeRecord(d *Deps) {
	if _, err := foldRetiredRuntimeRecordWith(d); err != nil {
		debug.Error("config: fold retired runtime record: %v", err)
	}
}

// foldRetiredRuntimeRecordWith is the injectable variant. It reports whether
// this call did the fold; the error carries what could not be folded, the file
// having been retired regardless once its foldable values were in.
func foldRetiredRuntimeRecordWith(d *Deps) (bool, error) {
	path := retiredRuntimeConfigPathWith(d)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read retired runtime record %q: %w", path, err)
	}
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		// The file stays put: a record pop cannot read is one it cannot fold, and
		// retiring it would turn an unreadable file into an invisible one.
		return false, fmt.Errorf("parse retired runtime record %q: %w", path, err)
	}
	refused := foldRuntimeDocument(d, doc)
	if err := d.FS.Rename(path, path+retiredRuntimeFoldedSuffix); err != nil {
		return false, fmt.Errorf("retire runtime record %q: %w", path, err)
	}
	if len(refused) > 0 {
		return true, fmt.Errorf("%s", strings.Join(refused, "; "))
	}
	return true, nil
}

// foldRuntimeDocument states everything the record holds that has a home in the
// override layer, at the scope that home is at, and returns what the gate
// refused. A refusal is reported rather than aborting the fold: one unreadable
// value must not strand the others, and the retired copy is where it survives.
//
// The record's [workbench.preferred] entries are deliberately not folded. They
// were written by the chord that is gone (ADR-0212 decision 6) and are keyed by
// checkout, which is no scope config has any more — folding one would have to
// invent a repository-wide answer from a per-checkout guess.
func foldRuntimeDocument(d *Deps, doc map[string]any) []string {
	var refused []string
	note := func(what string, err error) {
		refused = append(refused, fmt.Sprintf("%s: %v", what, err))
	}

	// Global scope: the list a `--no-<component>` decline recorded (ADR-0065's
	// runtime tier), which states the same thing the decline states today.
	if value, ok := documentValue(doc, integrationSkillsKey); ok {
		_, stated, err := OverrideValueWith(d, integrationSkillsKey)
		switch {
		case err != nil:
			note(integrationSkillsKey, err)
		case !stated:
			if err := SetOverrideValueWith(d, integrationSkillsKey, value); err != nil {
				note(integrationSkillsKey, err)
			}
		}
	}

	// Repository scope: the trunk a `--trunk` recorded, and the settings `pop
	// config repo set` recorded. Both are filed under Repository identity, which
	// is the key the layer's blocks use, so one entry serves every worktree.
	for _, entry := range recordedRepoEntries(d, doc) {
		stated, ok, err := RepoOverrideValueWith(d, entry.identity, entry.key)
		if err != nil {
			note(entry.key, err)
			continue
		}
		if ok {
			debug.Log("config: fold keeps the stated %s over the recorded %v for %s",
				entry.key, stated, entry.identity)
			continue
		}
		if _, err := SetRepoOverrideValueWith(d, entry.identity, entry.key, entry.value); err != nil {
			note(entry.key, err)
		}
	}
	return refused
}

// recordedRepoEntry is one repo-scope value the record holds, resolved to the
// repository it belongs to. The identity doubles as the checkout the layer's
// entry points are asked about: it is itself a path, and a path's identity is
// its own.
type recordedRepoEntry struct {
	identity string
	key      string
	value    any
}

// recordedRepoEntries collects the record's repo-scope values in a stable order,
// so a fold that is interrupted and re-run does the same thing twice.
func recordedRepoEntries(d *Deps, doc map[string]any) []recordedRepoEntry {
	var entries []recordedRepoEntry
	// A `[<checkout>] trunk = true` record marked the checkout its key names, so
	// the fold to a path value (ADR-0212 decision 3) is that key itself.
	for _, checkout := range recordedTrunkCheckouts(doc) {
		entries = append(entries, recordedRepoEntry{
			identity: repoIdentity(d, checkout),
			key:      "trunk",
			value:    canonicalPath(d, checkout),
		})
	}
	// A [repo_settings."<identity>"] block is already keyed the way the layer
	// keys its blocks, so its leaves are stated as they stand. Every leaf is
	// offered to the gate rather than filtered here: what a [repo] block may hold
	// is the gate's question, and a key it refuses names itself in the refusal.
	section, _ := doc[recordedRepoSettingsSection].(map[string]any)
	for _, identity := range sortedKeys(section) {
		block, ok := section[identity].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range sortedKeys(block) {
			entries = append(entries, recordedRepoEntry{
				identity: repoIdentity(d, identity),
				key:      key,
				value:    block[key],
			})
		}
	}
	return entries
}

// recordedRepoSettingsSection is the top-level table `pop config repo set` wrote
// its identity-keyed record into (ADR-0191), read here and nowhere else.
const recordedRepoSettingsSection = "repo_settings"

// recordedRuntimeReservedKeys are the record's own tables, which are named
// sections rather than the checkout paths the trunk records are keyed by.
var recordedRuntimeReservedKeys = map[string]bool{
	"integrations":              true,
	"workbench":                 true,
	recordedRepoSettingsSection: true,
}

// recordedTrunkCheckouts returns every checkout a `[<path>] trunk = true` record
// marks as its repository's Trunk worktree, sorted.
func recordedTrunkCheckouts(doc map[string]any) []string {
	var paths []string
	for _, key := range sortedKeys(doc) {
		if recordedRuntimeReservedKeys[key] {
			continue
		}
		block, ok := doc[key].(map[string]any)
		if !ok {
			continue
		}
		if trunk, ok := block["trunk"].(bool); !ok || !trunk {
			continue
		}
		paths = append(paths, key)
	}
	return paths
}

func sortedKeys(table map[string]any) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
