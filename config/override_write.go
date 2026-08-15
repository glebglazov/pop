package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is the write side of config.override.toml, the pop-written layer
// that is laid over whatever the resolution ladder resolved (ADR-0212 decision
// 2). It is a library only: the editor component that drives it is the sole
// writer pop ships, and it lands separately, so no command reaches this code yet.
//
// Every entry point works on one whole key: the unit of an override is a key's
// entire value as TOML, never a patch of it (ADR-0202 decision 2). It comes in
// two halves, one per scope (ADR-0212 decision 3):
//
//	global      keys are dotted config paths — the spelling `pop config keys`
//	            prints, e.g. "work.implement.agents"
//	repository  keys are the leaves of one [repo] block — "preferred_workbench",
//	            "turn_cap" — filed under the Repository identity of a checkout,
//	            so every worktree of that repository reads the one entry

// OverrideValue returns the whole value config.override.toml stores for a dotted
// config key, decoded as a generic TOML value. The bool is false when the file
// or the key is absent — "no override here", which is a different state from an
// override deliberately set to an empty value (ADR-0202 decision 6).
func OverrideValue(key string) (any, bool, error) {
	return OverrideValueWith(defaultDeps, key)
}

// OverrideValueWith is the injectable variant.
func OverrideValueWith(d *Deps, key string) (any, bool, error) {
	doc, _, err := overrideConfigFile(d).load(d)
	if err != nil {
		return nil, false, err
	}
	value, ok := documentValue(doc, key)
	return value, ok, nil
}

// SetOverrideValue records value as the whole value of a dotted config key, so
// it beats whatever the hand-authored sources say for that key at the next
// config load. Other overrides in the file are left alone and the write is
// atomic.
func SetOverrideValue(key string, value any) error {
	return SetOverrideValueWith(defaultDeps, key, value)
}

// SetOverrideValueWith is the injectable variant.
func SetOverrideValueWith(d *Deps, key string, value any) error {
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	if err := setDocumentValue(doc, key, value); err != nil {
		return err
	}
	return file.save(d, doc)
}

// DeleteOverrideValue removes a dotted config key from config.override.toml, so
// the hand-authored value below it comes back into force. Deleting a key that
// carries no override is a no-op. Tables the removal empties are pruned, and the
// file itself is deleted once its last key goes.
func DeleteOverrideValue(key string) error {
	return DeleteOverrideValueWith(defaultDeps, key)
}

// DeleteOverrideValueWith is the injectable variant.
func DeleteOverrideValueWith(d *Deps, key string) error {
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	if !deleteDocumentValue(doc, key) {
		return nil
	}
	return file.save(d, doc)
}

// RepoOverrideValue returns the whole value the override layer states for one
// repo-scope key against the repository owning checkoutPath. The bool is false
// when no block, or no such key in it, is stored — "no override here", which is
// a different state from an override deliberately set to an empty value.
func RepoOverrideValue(checkoutPath, key string) (any, bool, error) {
	return RepoOverrideValueWith(defaultDeps, checkoutPath, key)
}

// RepoOverrideValueWith is the injectable variant.
func RepoOverrideValueWith(d *Deps, checkoutPath, key string) (any, bool, error) {
	doc, _, err := overrideConfigFile(d).load(d)
	if err != nil {
		return nil, false, err
	}
	block, _ := repoOverrideBlock(d, doc, repoIdentity(d, checkoutPath))
	if block == nil {
		return nil, false, nil
	}
	value, ok := block[key]
	return value, ok, nil
}

// SetRepoOverrideValue states value for one repo-scope key against the
// repository owning checkoutPath, so it beats whatever the ladder resolves for
// that repository — and beats a global override of the same key, the repository
// being the more specific scope (ADR-0212 decision 2). It returns the Repository
// identity the entry was filed under, which is what makes one entry serve every
// worktree. Other entries in the file are left alone and the write is atomic.
func SetRepoOverrideValue(checkoutPath, key string, value any) (string, error) {
	return SetRepoOverrideValueWith(defaultDeps, checkoutPath, key, value)
}

// SetRepoOverrideValueWith is the injectable variant. A key the repo surface
// does not hold, or a value it cannot hold, is refused before anything is
// written: a file pop wrote itself must never be the source of a finding.
func SetRepoOverrideValueWith(d *Deps, checkoutPath, key string, value any) (string, error) {
	if !repoBlockLegalKeys()[key] {
		return "", unknownRepoOverrideKeyError(key)
	}
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return "", err
	}
	identity := repoIdentity(d, checkoutPath)
	block, blockKey := repoOverrideBlock(d, doc, identity)
	if block == nil {
		block, blockKey = map[string]any{}, identity
	}
	block[key] = value
	if err := validateRepoOverrideBlock(block); err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	section, ok := doc[overrideRepoSection].(map[string]any)
	if !ok || section == nil {
		section = map[string]any{}
		doc[overrideRepoSection] = section
	}
	section[blockKey] = block
	if err := file.save(d, doc); err != nil {
		return "", err
	}
	return identity, nil
}

// DeleteRepoOverrideValue removes one repo-scope key's override for the
// repository owning checkoutPath, so what the ladder resolves there comes back
// into force. Deleting a key that carries no override is a no-op. A block the
// removal empties is pruned, and the file itself goes once its last entry does.
func DeleteRepoOverrideValue(checkoutPath, key string) error {
	return DeleteRepoOverrideValueWith(defaultDeps, checkoutPath, key)
}

// DeleteRepoOverrideValueWith is the injectable variant.
func DeleteRepoOverrideValueWith(d *Deps, checkoutPath, key string) error {
	file := overrideConfigFile(d)
	doc, _, err := file.load(d)
	if err != nil {
		return err
	}
	block, blockKey := repoOverrideBlock(d, doc, repoIdentity(d, checkoutPath))
	if block == nil {
		return nil
	}
	if _, ok := block[key]; !ok {
		return nil
	}
	delete(block, key)
	section, _ := doc[overrideRepoSection].(map[string]any)
	if len(block) == 0 {
		delete(section, blockKey)
	}
	if len(section) == 0 {
		delete(doc, overrideRepoSection)
	}
	return file.save(d, doc)
}

// repoOverrideBlock finds the stored block of one repository inside the
// document, returning it together with the key it is filed under so a writer
// updates that key rather than adding a second block for the same repository.
// Matching is by identity, not by text, for the same reason resolution matches
// that way: two spellings of one repository are one repository.
func repoOverrideBlock(d *Deps, doc map[string]any, identity string) (map[string]any, string) {
	section, ok := doc[overrideRepoSection].(map[string]any)
	if !ok || section == nil {
		return nil, ""
	}
	// Keys are walked in a stable order so a document that somehow holds two
	// spellings of one repository always resolves to the same block.
	keys := make([]string, 0, len(section))
	for key := range section {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if repoIdentity(d, key) != identity {
			continue
		}
		if block, ok := section[key].(map[string]any); ok && block != nil {
			return block, key
		}
	}
	return nil, ""
}

// validateRepoOverrideBlock re-encodes the block and decodes it through the
// struct that decodes a hand-authored [repo."<path>"] block, so a value the
// config could not read back is refused at the write rather than ignored at the
// next load.
func validateRepoOverrideBlock(block map[string]any) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(block); err != nil {
		return fmt.Errorf("cannot be written as config: %w", err)
	}
	var probe RepoOverrideConfig
	if _, err := toml.Decode(buf.String(), &probe); err != nil {
		return fmt.Errorf("is not a value this key can hold: %w", err)
	}
	return nil
}

// unknownRepoOverrideKeyError refuses a key that has no repository home, naming
// the ones that do so the caller does not have to go looking.
func unknownRepoOverrideKeyError(key string) error {
	legal := repoBlockLegalKeys()
	names := make([]string, 0, len(legal))
	for name := range legal {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("config key %q has no repository scope; repo-scope keys: %s",
		key, strings.Join(names, ", "))
}
