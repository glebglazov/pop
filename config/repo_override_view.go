package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// This file is the repository scope of the Config dashboard (ADR-0212 decisions
// 3 and 6). The dashboard's other rows are keys of the global surface, which one
// document defines and one write reaches; a repo-scope key is stated by whichever
// checkout of a repository the human is standing in, so its rows are resolved
// against that repository and written into the override layer's block for it.
//
// It is what makes the dashboard the place a Preferred workbench is chosen now
// that the chord that used to open a picker for it is gone: the key has no
// global spelling, so without a repository scope here there would be nowhere in
// the dashboard to say it.
//
// A row addresses its key as `repo.<leaf>` — the repository in scope, never a
// path — and that spelling is also the editor buffer's, so what a human edits is
// TOML that reads back as the value it sets.

// repoScopeKeyPrefix is the row spelling's first segment. It is the word the
// user's own config.toml already uses for a repository's block; what a row drops
// is the path that keys one, because the repository a row means is the one the
// dashboard was opened in.
const repoScopeKeyPrefix = "repo."

// RepoScopeKey spells one repo-block leaf as the dashboard addresses it.
func RepoScopeKey(leaf string) string { return repoScopeKeyPrefix + leaf }

// RepoScopeKeyLeaf reads a dashboard row key back as the repo-block leaf it
// names. The bool is false for a key of the global surface, which is how a
// caller holding one key tells the two scopes apart, and for a repo-block key
// the dashboard does not offer.
func RepoScopeKeyLeaf(key string) (string, bool) {
	leaf, ok := strings.CutPrefix(key, repoScopeKeyPrefix)
	if !ok {
		return "", false
	}
	for _, doc := range RepoScopeKeyDocs() {
		if doc.Key == leaf {
			return leaf, true
		}
	}
	return "", false
}

// RepoScopeKeyDocs returns the repo-block keys the dashboard offers, reflected
// from the same struct that decodes a [repo."<path>"] block so the row list
// cannot drift from what the surface accepts.
//
// Left out are the keys the walker unions across rungs rather than letting the
// most specific one win: a preview names the value in force and the layer that
// produced it, and for a unioned key no single layer holds either.
func RepoScopeKeyDocs() []ConfigKeyDoc {
	docs, _ := ScopeKeyDocs(ScopeRepo)
	unioned := repoScopeUnionedKeys()
	out := make([]ConfigKeyDoc, 0, len(docs))
	for _, doc := range docs {
		if unioned[doc.Key] {
			continue
		}
		out = append(out, doc)
	}
	return out
}

// repoScopeUnionedKeys is the set of repo-block keys carrying a merge policy —
// the tag that tells the walker to combine a key's value across rungs instead of
// taking the most specific one whole (ADR-0122).
func repoScopeUnionedKeys() map[string]bool {
	unioned := map[string]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type)
				continue
			}
			if f.Tag.Get("merge") == "" {
				continue
			}
			if name := strings.Split(f.Tag.Get("toml"), ",")[0]; name != "" && name != "-" {
				unioned[name] = true
			}
		}
	}
	walk(reflect.TypeOf(RepoOverrideConfig{}))
	return unioned
}

// RepoOverrideKeyViews resolves every repo-scope row for the repository owning
// checkoutPath: what is in force, which layer produced it, and what an override
// is laid over. It is the repository half of what the Config dashboard renders;
// OverrideKeyViewsWith is the global half.
func RepoOverrideKeyViews(d *Deps, configPath, checkoutPath string) ([]OverrideKeyView, error) {
	if strings.TrimSpace(checkoutPath) == "" {
		return nil, nil
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPathWith(d)
	}
	cfg, err := repoScopeBlockConfig(d, configPath)
	if err != nil {
		return nil, err
	}
	layers := cfg.newRepoScope(d, checkoutPath).repoScopeLayers()
	docs := RepoScopeKeyDocs()
	views := make([]OverrideKeyView, 0, len(docs))
	for _, doc := range docs {
		views = append(views, overrideKeyView(OverrideKey{Key: RepoScopeKey(doc.Key), Desc: doc.Desc}, doc.Type, layers))
	}
	return views, nil
}

// repoScopeBlockConfig loads the hand-authored config the [repo."<path>"] blocks
// live in. A file that is not there is an empty config rather than a failure:
// the dashboard has to open on a machine that has never written one.
func repoScopeBlockConfig(d *Deps, configPath string) (*Config, error) {
	if _, err := d.FS.ReadFile(configPath); err != nil {
		return &Config{}, nil
	}
	return LoadWith(d, configPath)
}

// repoScopeLayers returns the sources that can define a repo-scope key for this
// checkout, highest rank first — the override layer's block for the repository,
// then the scope-first ladder read from the top. It is the same order
// resolveRepoConfig and preferredSources walk; reading it from the top makes
// "laid over the answer" and "asked first" the same walk, which is what lets one
// generic per-key read report provenance for both.
//
// The override rung is always present, empty when the layer states nothing, so a
// reader can tell an override by its position alone.
func (e *repoScopeEnumerator) repoScopeLayers() []overrideValueLayer {
	override := map[string]any{}
	if block, ok := e.overrideLayer().repoBlock(e.d, e.identity); ok {
		override = repoBlockDoc(block, e.identity)
	}
	layers := []overrideValueLayer{
		{layer: OverrideLayerOverride, doc: repoScopeDocument(override)},
	}

	declared := map[string]any{}
	locus := ""
	if e.declaredFound {
		declared = repoBlockDoc(e.declared, e.declaredKeyCanon)
		locus = fmt.Sprintf("[repo.%q]", e.declaredKeyCanon)
	}
	layers = append(layers, overrideValueLayer{layer: OverrideLayerConfig, locus: locus, doc: repoScopeDocument(declared)})

	// The committed anchors are enumerated inherited-first for the walker; a
	// reader asks the most specific one first, so they are read back to front.
	anchors := e.popScopeAnchors()
	for i := len(anchors) - 1; i >= 0; i-- {
		cfg, _ := e.popTOML(anchors[i])
		layers = append(layers, overrideValueLayer{
			layer: OverrideLayerRepoTOML,
			locus: anchors[i],
			doc:   repoScopeDocument(repoScopeDoc(cfg.RepoScopeConfig)),
		})
	}

	return append(layers, e.recordedLayers()...)
}

// recordedLayers are the gap-filler rungs: what pop recorded for this repository
// and for the checkouts of it. They sit under every declaration of the same
// scope, so they come last.
func (e *repoScopeEnumerator) recordedLayers() []overrideValueLayer {
	recorded := map[string]any{}
	if doc, _, err := loadRuntimeDocument(e.d); err == nil {
		if path, ok := e.recordedTrunk(doc); ok {
			recorded["trunk"] = path
		}
		if stored, _, err := runtimeRepoSettingsFromDoc(doc, e.identity); err == nil && stored.TurnCap != nil {
			recorded["turn_cap"] = int64(*stored.TurnCap)
		}
	}
	// A preferred workbench is recorded per checkout rather than per repository,
	// this one first and the Trunk worktree's after it — the inheritance a
	// checkout with no entry of its own reads through.
	layers := []overrideValueLayer{{
		layer: OverrideLayerRuntime,
		doc:   repoScopeDocument(e.recordedPreferred(recorded, e.checkoutPath)),
	}}
	if e.d != nil && e.d.Trunk != nil {
		if trunk, ok := e.d.Trunk(e.checkoutPath); ok && trunk != "" && canonicalPath(e.d, trunk) != e.canon {
			layers = append(layers, overrideValueLayer{
				layer: OverrideLayerRuntime,
				locus: trunk,
				doc:   repoScopeDocument(e.recordedPreferred(map[string]any{}, trunk)),
			})
		}
	}
	return layers
}

// recordedPreferred adds one checkout's recorded Preferred workbench to a block.
// The entry is three-valued, so presence rather than emptiness decides: an entry
// recorded as "" is an explicit none and a real value, not an absent key.
func (e *repoScopeEnumerator) recordedPreferred(block map[string]any, checkout string) map[string]any {
	name, present, err := RuntimePreferredWorkbenchWith(e.d, checkout)
	if err == nil && present {
		block["preferred_workbench"] = name
	}
	return block
}

// repoScopeDocument nests one block's leaves under the row spelling, so a key
// path a dashboard row carries reads out of it and the TOML it renders is the
// buffer a human edits.
func repoScopeDocument(block map[string]any) map[string]any {
	return map[string]any{strings.TrimSuffix(repoScopeKeyPrefix, "."): block}
}

// repoBlockDoc is one [repo] block — from config.toml or from the override layer,
// the two sharing a shape — as the leaves it declares. blockKey is the block's
// own key, which is what a retired `trunk = true` named.
func repoBlockDoc(block RepoOverrideConfig, blockKey string) map[string]any {
	doc := repoScopeDoc(block.RepoScopeConfig)
	if path, ok := block.Trunk.Resolve(blockKey); ok {
		doc["trunk"] = path
	}
	if block.TurnCap != nil {
		doc["turn_cap"] = int64(*block.TurnCap)
	}
	return doc
}

// repoScopeDoc is the shared repo-scope key set as the leaves a source declares.
// A zero value is an absent key, the same presence rule repoScopeMetadata gives
// the walker, so a source that says nothing about a key does not shadow the one
// below it.
func repoScopeDoc(scope RepoScopeConfig) map[string]any {
	doc := map[string]any{}
	if scope.PreferredWorkbench != "" {
		doc["preferred_workbench"] = scope.PreferredWorkbench
	}
	return doc
}

// StoreRepoOverrideBufferWith parses the text a human handed back from $EDITOR as
// the whole value of one repo-scope key and states it for the repository owning
// checkoutPath. The two returns are the two outcomes StoreOverrideBufferWith
// gives: a problem the human must fix, nothing written; or a failure of the write
// itself.
func StoreRepoOverrideBufferWith(d *Deps, checkoutPath, key, buffer string) (string, error) {
	leaf, ok := RepoScopeKeyLeaf(key)
	if !ok {
		return fmt.Sprintf("%s is not a key pop can override.", key), nil
	}
	if strings.TrimSpace(buffer) == "" {
		return fmt.Sprintf("The buffer is empty; it has to set %s.", key), nil
	}
	doc := map[string]any{}
	if _, err := toml.Decode(buffer, &doc); err != nil {
		return fmt.Sprintf("This is not valid TOML: %v", err), nil
	}
	value, ok := documentValue(doc, key)
	if !ok {
		return fmt.Sprintf("The buffer has to set %s, and it sets no such key.", key), nil
	}
	if extra := documentKeysBesides(doc, key); len(extra) > 0 {
		return fmt.Sprintf(
			"The buffer also sets %s. One buffer overrides one key: %s.",
			strings.Join(extra, ", "), key), nil
	}
	// The same gate the write side applies, run here so a value the block could
	// not read back sends the human back to the buffer rather than failing the
	// component: a file pop wrote itself must never be the source of a finding.
	if err := validateRepoOverrideBlock(map[string]any{leaf: value}); err != nil {
		return fmt.Sprintf("%s %v", key, err), nil
	}
	_, err := SetRepoOverrideValueWith(d, checkoutPath, leaf, value)
	return "", err
}

// CopyRepoOverrideFromSourceWith states what the layers below the override still
// say as the override itself, so a human starts from the value in force. Where no
// layer defines the key it is that key's empty value — what the preview already
// renders for it.
func CopyRepoOverrideFromSourceWith(d *Deps, configPath, checkoutPath, key string) error {
	leaf, ok := RepoScopeKeyLeaf(key)
	if !ok {
		return fmt.Errorf("config key %q is not one pop can override", key)
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPathWith(d)
	}
	cfg, err := repoScopeBlockConfig(d, configPath)
	if err != nil {
		return err
	}
	layers := cfg.newRepoScope(d, checkoutPath).repoScopeLayers()
	// Layer 0 is the override itself; the source is what would be in force with
	// that layer gone.
	value, idx := topmostValue(key, layers[1:])
	if idx < 0 {
		value = zeroTOMLValue(repoScopeKeyTypes()[leaf])
	}
	_, err = SetRepoOverrideValueWith(d, checkoutPath, leaf, value)
	return err
}

// repoScopeKeyTypes maps every offered repo-scope key to its TOML type, so a key
// no layer defines still renders and copies down as config.
func repoScopeKeyTypes() map[string]string {
	docs := RepoScopeKeyDocs()
	types := make(map[string]string, len(docs))
	for _, doc := range docs {
		types[doc.Key] = doc.Type
	}
	return types
}
