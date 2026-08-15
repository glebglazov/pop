package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// This file is `pop config repo set` and its read-back: the repo-scoped
// settings a human states through pop, keyed by repository identity rather than
// by an exact checkout so every worktree of a repository reads one value
// (ADR-0191's keying, kept). Two rules shape everything here:
//
//   - Setting one is a human stating intent, so it lands in the override layer's
//     block for the repository (ADR-0212 decision 5) and beats every declaration
//     the ladder resolves — the same destination, and the same gate, the Config
//     dashboard's repository rows write through.
//   - What may be set is derived from a schema by reflection, exactly as
//     repoScopeLegalKeys derives what may be read, so the setter cannot drift
//     from what the config accepts.

// RepoSettableConfig is the schema of what `pop config repo set` may state:
// reflected as RepoSettableKeyDocs, it is the settable key set itself. Every
// field mirrors a key of RepoOverrideConfig, so this struct is a subset of the
// hand-authored surface and never a surface of its own; the shared repo scope
// (RepoScopeConfig, legal in a committed .pop/config.toml too) is deliberately
// not included, because pop must not write into a repository's tree.
type RepoSettableConfig struct {
	// TurnCap is the repository's bound on how many Turns one implementation
	// attempt may spend (ADR-0190). Nil means pop has recorded none.
	TurnCap *int `toml:"turn_cap" desc:"Max Turns one implementation attempt in this repo may spend (claude only; other presets cannot be told)."`
}

// repoSettingKind carries the per-key behaviour reflection cannot supply: how to
// read the raw string a human typed, and how to pull that key out of each layer
// that can answer for it. The map is pinned against the reflected schema by
// test, in both directions, so a field added to RepoSettableConfig cannot become
// settable without its parser or be declared here without existing in the
// schema.
//
// block reads the key out of a [repo] block whatever layer the block came from —
// a hand-authored config.toml declaration or the override layer's entry for this
// repository — because the two share one shape (ADR-0212 decision 7).
type repoSettingKind struct {
	parse func(raw string) (any, error)
	block func(RepoOverrideConfig) (any, bool)
}

var repoSettingKinds = map[string]repoSettingKind{
	"turn_cap": {
		parse: parseTurnCapSetting,
		block: func(o RepoOverrideConfig) (any, bool) {
			if o.TurnCap == nil {
				return nil, false
			}
			return *o.TurnCap, true
		},
	},
}

// parseTurnCapSetting reads a turn cap off the command line. Zero is accepted
// and stored: resolution reads a non-positive bound as "declares no cap", so
// setting 0 is how a repository gives back a bound it once had.
func parseTurnCapSetting(raw string) (any, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("want a whole number of turns, got %q", raw)
	}
	if n < 0 {
		return nil, fmt.Errorf("want a whole number of turns, got %d (0 stores no bound)", n)
	}
	return n, nil
}

// RepoSettableKeyDocs returns the keys pop may set for a repository, reflected
// from RepoSettableConfig in declaration order with their TOML types and
// descriptions — the same reflection that backs `pop config keys`, so the
// settable set is generated rather than listed.
func RepoSettableKeyDocs() []ConfigKeyDoc {
	return structDocs("", reflect.TypeOf(RepoSettableConfig{}), false, nil)
}

// RepoSettableKeys returns just the settable key names, in declaration order.
func RepoSettableKeys() []string {
	docs := RepoSettableKeyDocs()
	keys := make([]string, 0, len(docs))
	for _, doc := range docs {
		keys = append(keys, doc.Key)
	}
	return keys
}

// UnknownRepoSettingError is the refusal for a key pop cannot set, naming the
// keys that exist so the human does not have to go looking.
func UnknownRepoSettingError(key string) error {
	return fmt.Errorf("unknown repo setting %q; settable keys: %s",
		key, strings.Join(RepoSettableKeys(), ", "))
}

// RepoIdentity returns the repository identity a checkout belongs to — the key
// this layer files values under, and what makes one value serve every worktree.
func RepoIdentity(d *Deps, path string) string {
	if d == nil {
		d = defaultDeps
	}
	return repoIdentity(d, path)
}

// SetRepoSetting states value for key against the repository owning
// checkoutPath. A human setting a repository's bound is stating intent, so it
// lands in the override layer rather than in a gap-filler pop wrote for itself
// (ADR-0212 decision 5); the user's config.toml is never touched. It returns the
// repository identity the value was filed under. An unknown key or an unreadable
// value is refused before anything is written.
func SetRepoSetting(checkoutPath, key, value string) (string, error) {
	return SetRepoSettingWith(defaultDeps, checkoutPath, key, value)
}

// SetRepoSettingWith is the injectable variant. The raw string a human typed is
// read by this file's parser and the resulting value handed to the layer's own
// entry point, so a command-line setter and the Config dashboard write the same
// key through the same gate.
func SetRepoSettingWith(d *Deps, checkoutPath, key, value string) (string, error) {
	if d == nil {
		d = defaultDeps
	}
	kind, ok := repoSettingKinds[key]
	if !ok {
		return "", UnknownRepoSettingError(key)
	}
	parsed, err := kind.parse(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	return SetRepoOverrideValueWith(d, checkoutPath, key, parsed)
}

// RepoSettingSource names the layer that supplied a setting's effective value,
// so a read can say where the number came from and not just what it is.
type RepoSettingSource string

const (
	// RepoSettingUnset means no layer declares the key.
	RepoSettingUnset RepoSettingSource = "unset"
	// RepoSettingOverride is the hand-authored [repo."<path>"] block, the most
	// specific declaration of the ladder.
	RepoSettingOverride RepoSettingSource = "hand-authored config.toml"
	// RepoSettingOverrideLayer is the override layer's entry for this repository:
	// laid over whatever the ladder resolved, so it beats even the hand-authored
	// block (ADR-0212 decision 2).
	RepoSettingOverrideLayer RepoSettingSource = "override layer"
)

// RepoSetting is one settable key's effective value for a repository, with the
// layer that supplied it and the locus a human would edit to change it.
type RepoSetting struct {
	Key    string
	Value  string // rendered TOML value; "" when unset
	Source RepoSettingSource
	Locus  string // the file or block the winning value came from; "" when unset
}

// ResolveRepoSettings reports every settable key's effective value for the
// repository owning checkoutPath, in the same precedence resolution itself uses:
// the override layer's entry for this repository beats a hand-authored
// [repo."<path>"] block matched by repository identity, which beats unset.
func (c *Config) ResolveRepoSettings(d *Deps, checkoutPath string) ([]RepoSetting, error) {
	if d == nil {
		d = defaultDeps
	}
	e := c.newRepoScope(d, checkoutPath)
	overridden, hasOverride := e.overrideLayer().repoBlock(d, e.identity)
	docs := RepoSettableKeyDocs()
	settings := make([]RepoSetting, 0, len(docs))
	for _, doc := range docs {
		kind := repoSettingKinds[doc.Key]
		setting := RepoSetting{Key: doc.Key, Source: RepoSettingUnset}
		if e.declaredFound {
			if value, ok := kind.block(e.declared); ok {
				setting.Value = fmt.Sprint(value)
				setting.Source = RepoSettingOverride
				setting.Locus = fmt.Sprintf("[repo.%q]", e.declaredKeyCanon)
			}
		}
		if hasOverride {
			if value, ok := kind.block(overridden); ok {
				setting.Value = fmt.Sprint(value)
				setting.Source = RepoSettingOverrideLayer
				setting.Locus = DefaultOverrideConfigPathWith(d)
			}
		}
		settings = append(settings, setting)
	}
	return settings, nil
}
