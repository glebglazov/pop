package config

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is the Repo override runtime layer (ADR-0191): the repo-scoped
// settings pop writes for itself, filed in config.runtime.toml under a
// repository identity rather than an exact checkout, so every worktree of a
// repository reads one value. Two rules shape everything here:
//
//   - Pop writes only here, never into the hand-authored config.toml (ADR-0150),
//     so a [repo."<path>"] block a human writes always beats a value pop wrote
//     and never the other way around. Above both sits the override layer, whose
//     per-repository block holds the same keys and wins outright (ADR-0212
//     decision 2) — this layer records what a surface happened to pick, that one
//     records what a human stated.
//   - What may be written is derived from a schema by reflection, exactly as
//     repoScopeLegalKeys derives what may be read, so the setter cannot drift
//     from what the config accepts.

// runtimeRepoSettingsSection is the top-level table of config.runtime.toml that
// holds this layer. It is keyed by repository identity — the divergence from
// [workbench.preferred], which keys by exact checkout because it describes a
// checkout rather than a repository.
const runtimeRepoSettingsSection = "repo_settings"

// RepoRuntimeConfig is the schema of one [repo_settings."<identity>"] block: the
// repo-scoped keys pop may write on the human's behalf. Every field mirrors a
// key of RepoOverrideConfig, so this struct is a subset of the hand-authored
// surface and never a surface of its own; the shared repo scope
// (RepoScopeConfig, legal in a committed .pop/config.toml too) is deliberately
// not included, because pop must not write into a repository's tree.
type RepoRuntimeConfig struct {
	// TurnCap is the repository's bound on how many Turns one implementation
	// attempt may spend (ADR-0190). Nil means pop has recorded none.
	TurnCap *int `toml:"turn_cap" desc:"Max Turns one implementation attempt in this repo may spend (claude only; other presets cannot be told)."`
}

// repoSettingKind carries the per-key behaviour reflection cannot supply: how to
// read the raw string a human typed, and how to pull that key out of each layer
// that can answer for it. The map is pinned against the reflected schema by
// test, in both directions, so a field added to RepoRuntimeConfig cannot become
// settable without its parser or be declared here without existing in the
// schema.
//
// block reads the key out of a [repo] block whatever layer the block came from —
// a hand-authored config.toml declaration or the override layer's entry for this
// repository — because the two share one shape (ADR-0212 decision 7).
type repoSettingKind struct {
	parse   func(raw string) (any, error)
	block   func(RepoOverrideConfig) (any, bool)
	runtime func(RepoRuntimeConfig) (any, bool)
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
		runtime: func(r RepoRuntimeConfig) (any, bool) {
			if r.TurnCap == nil {
				return nil, false
			}
			return *r.TurnCap, true
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
// from RepoRuntimeConfig in declaration order with their TOML types and
// descriptions — the same reflection that backs `pop config keys`, so the
// settable set is generated rather than listed.
func RepoSettableKeyDocs() []ConfigKeyDoc {
	return structDocs("", reflect.TypeOf(RepoRuntimeConfig{}), false, nil)
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

// SetRepoSetting records value for key against the repository owning
// checkoutPath, in pop's runtime state. The user's config.toml is never touched.
// It returns the repository identity the value was filed under. An unknown key
// or an unreadable value is refused before anything is written.
func SetRepoSetting(checkoutPath, key, value string) (string, error) {
	return SetRepoSettingWith(defaultDeps, checkoutPath, key, value)
}

// SetRepoSettingWith is the injectable variant.
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
	identity := repoIdentity(d, checkoutPath)
	doc, _, err := loadRuntimeDocument(d)
	if err != nil {
		return "", err
	}
	setRuntimeRepoSetting(doc, identity, key, parsed)
	if err := saveRuntimeDocument(d, doc); err != nil {
		return "", err
	}
	return identity, nil
}

// RepoSettingSource names the layer that supplied a setting's effective value,
// so a read can say where the number came from and not just what it is.
type RepoSettingSource string

const (
	// RepoSettingUnset means no layer declares the key.
	RepoSettingUnset RepoSettingSource = "unset"
	// RepoSettingRuntime is the pop-written layer: what `pop config repo set` wrote.
	RepoSettingRuntime RepoSettingSource = "pop (config repo set)"
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
// [repo."<path>"] block matched by repository identity, which beats the
// pop-written runtime layer, which beats unset.
func (c *Config) ResolveRepoSettings(d *Deps, checkoutPath string) ([]RepoSetting, error) {
	if d == nil {
		d = defaultDeps
	}
	e := c.newRepoScope(d, checkoutPath)
	stored, _, err := runtimeRepoSettingsWith(d, e.identity)
	if err != nil {
		return nil, err
	}
	overridden, hasOverride := e.overrideLayer().repoBlock(d, e.identity)
	docs := RepoSettableKeyDocs()
	settings := make([]RepoSetting, 0, len(docs))
	for _, doc := range docs {
		kind := repoSettingKinds[doc.Key]
		setting := RepoSetting{Key: doc.Key, Source: RepoSettingUnset}
		if value, ok := kind.runtime(stored); ok {
			setting.Value = fmt.Sprint(value)
			setting.Source = RepoSettingRuntime
			setting.Locus = DefaultRuntimeConfigPathWith(d)
		}
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

// runtimeRepoSettingsWith reads the pop-written block for one repository
// identity. The bool distinguishes "no block" from "a block declaring nothing".
func runtimeRepoSettingsWith(d *Deps, identity string) (RepoRuntimeConfig, bool, error) {
	doc, _, err := loadRuntimeDocument(d)
	if err != nil {
		return RepoRuntimeConfig{}, false, err
	}
	return runtimeRepoSettingsFromDoc(doc, identity)
}

// runtimeRepoSettingsFromDoc is the same read against an already-loaded document,
// for a caller that needs more than one section of it and must not pay a second
// file read to get them.
func runtimeRepoSettingsFromDoc(doc map[string]any, identity string) (RepoRuntimeConfig, bool, error) {
	section, ok := doc[runtimeRepoSettingsSection].(map[string]any)
	if !ok || section == nil {
		return RepoRuntimeConfig{}, false, nil
	}
	block, ok := section[identity].(map[string]any)
	if !ok || block == nil {
		return RepoRuntimeConfig{}, false, nil
	}
	cfg, err := decodeRepoRuntimeBlock(block)
	if err != nil {
		return RepoRuntimeConfig{}, false, err
	}
	return cfg, true, nil
}

// decodeRepoRuntimeBlock re-encodes one stored block and decodes it through
// RepoRuntimeConfig, so a stored value is read by the same struct tags that
// decide what may be written — adding a key needs no second decoder, and a key
// that left the schema stops being read.
func decodeRepoRuntimeBlock(block map[string]any) (RepoRuntimeConfig, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(block); err != nil {
		return RepoRuntimeConfig{}, fmt.Errorf("encode repo settings: %w", err)
	}
	var cfg RepoRuntimeConfig
	if _, err := toml.Decode(buf.String(), &cfg); err != nil {
		return RepoRuntimeConfig{}, fmt.Errorf("decode repo settings: %w", err)
	}
	return cfg, nil
}

// setRuntimeRepoSetting sets doc[repo_settings][identity][key], creating the
// intermediate tables on demand.
func setRuntimeRepoSetting(doc map[string]any, identity, key string, value any) {
	section, ok := doc[runtimeRepoSettingsSection].(map[string]any)
	if !ok || section == nil {
		section = map[string]any{}
		doc[runtimeRepoSettingsSection] = section
	}
	block, ok := section[identity].(map[string]any)
	if !ok || block == nil {
		block = map[string]any{}
		section[identity] = block
	}
	block[key] = value
}
